// Copyright 2012, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wrangler

import (
	"fmt"

	log "github.com/golang/glog"
	"github.com/youtube/vitess/go/vt/tabletmanager/actionnode"
	"github.com/youtube/vitess/go/vt/topo"
)

// shard related methods for Wrangler

func (wr *Wrangler) lockShard(keyspace, shard string, actionNode *actionnode.ActionNode) (lockPath string, err error) {
	log.Infof("Locking shard %v/%v for action %v", keyspace, shard, actionNode.Action)
	return wr.ts.LockShardForAction(keyspace, shard, actionNode.ToJson(), wr.lockTimeout, interrupted)
}

func (wr *Wrangler) unlockShard(keyspace, shard string, actionNode *actionnode.ActionNode, lockPath string, actionError error) error {
	// first update the actionNode
	if actionError != nil {
		log.Infof("Unlocking shard %v/%v for action %v with error %v", keyspace, shard, actionNode.Action, actionError)
		actionNode.Error = actionError.Error()
		actionNode.State = actionnode.ACTION_STATE_FAILED
	} else {
		log.Infof("Unlocking shard %v/%v for successful action %v", keyspace, shard, actionNode.Action)
		actionNode.Error = ""
		actionNode.State = actionnode.ACTION_STATE_DONE
	}
	err := wr.ts.UnlockShardForAction(keyspace, shard, lockPath, actionNode.ToJson())
	if actionError != nil {
		if err != nil {
			// this will be masked
			log.Warningf("UnlockShardForAction failed: %v", err)
		}
		return actionError
	}
	return err
}

// SetShardServedTypes changes the ServedTypes parameter of a shard.
// It does not rebuild any serving graph or do any consistency check (yet).
func (wr *Wrangler) SetShardServedTypes(keyspace, shard string, servedTypes []topo.TabletType) error {

	actionNode := actionnode.SetShardServedTypes(servedTypes)
	lockPath, err := wr.lockShard(keyspace, shard, actionNode)
	if err != nil {
		return err
	}

	err = wr.setShardServedTypes(keyspace, shard, servedTypes)
	return wr.unlockShard(keyspace, shard, actionNode, lockPath, err)
}

func (wr *Wrangler) setShardServedTypes(keyspace, shard string, servedTypes []topo.TabletType) error {
	shardInfo, err := wr.ts.GetShard(keyspace, shard)
	if err != nil {
		return err
	}

	shardInfo.ServedTypes = servedTypes
	return wr.ts.UpdateShard(shardInfo)
}

// DeleteShard will do all the necessary changes in the topology server
// to entirely remove a shard. It can only work if there are no tablets
// in that shard.
func (wr *Wrangler) DeleteShard(keyspace, shard string) error {
	shardInfo, err := wr.ts.GetShard(keyspace, shard)
	if err != nil {
		return err
	}

	tabletMap, err := GetTabletMapForShard(wr.ts, keyspace, shard)
	if err != nil {
		return err
	}
	if len(tabletMap) > 0 {
		return fmt.Errorf("shard %v/%v still has %v tablets", keyspace, shard, len(tabletMap))
	}

	// remove the replication graph and serving graph in each cell
	for _, cell := range shardInfo.Cells {
		if err := wr.ts.DeleteShardReplication(cell, keyspace, shard); err != nil {
			log.Warningf("Cannot delete ShardReplication in cell %v for %v/%v: %v", cell, keyspace, shard, err)
		}

		for _, t := range topo.AllTabletTypes {
			if !topo.IsInServingGraph(t) {
				continue
			}

			if err := wr.ts.DeleteSrvTabletType(cell, keyspace, shard, t); err != nil && err != topo.ErrNoNode {
				log.Warningf("Cannot delete EndPoints in cell %v for %v/%v/%v: %v", cell, keyspace, shard, t, err)
			}
		}

		if err := wr.ts.DeleteSrvShard(cell, keyspace, shard); err != nil && err != topo.ErrNoNode {
			log.Warningf("Cannot delete SrvShard in cell %v for %v/%v: %v", cell, keyspace, shard, err)
		}
	}

	return wr.ts.DeleteShard(keyspace, shard)
}

// RemoveShardCell will remove a cell from the Cells list in a shard.
// It will first check the shard has no tablets there.  if 'force' is
// specified, it will remove the cell even when the tablet map cannot
// be retrieved. This is intended to be used when a cell is completely
// down and its topology server cannot even be reached.
func (wr *Wrangler) RemoveShardCell(keyspace, shard, cell string, force bool) error {
	actionNode := actionnode.UpdateShard()
	lockPath, err := wr.lockShard(keyspace, shard, actionNode)
	if err != nil {
		return err
	}

	err = wr.removeShardCell(keyspace, shard, cell, force)
	return wr.unlockShard(keyspace, shard, actionNode, lockPath, err)
}

func (wr *Wrangler) removeShardCell(keyspace, shard, cell string, force bool) error {
	shardInfo, err := wr.ts.GetShardCritical(keyspace, shard)
	if err != nil {
		return err
	}

	// check the cell is in the list already
	if !topo.InCellList(cell, shardInfo.Cells) {
		return fmt.Errorf("cell %v in not in shard info", cell)
	}

	// check the master alias is not in the cell
	if shardInfo.MasterAlias.Cell == cell {
		return fmt.Errorf("master %v is in the cell '%v' we want to remove", shardInfo.MasterAlias, cell)
	}

	// get the ShardReplication object in the cell
	sri, err := wr.ts.GetShardReplication(cell, keyspace, shard)
	switch err {
	case nil:
		if len(sri.ReplicationLinks) > 0 {
			return fmt.Errorf("cell %v has %v possible tablets in replication graph", cell, len(sri.ReplicationLinks))
		}

		// ShardReplication object is now useless, remove it
		if err := wr.ts.DeleteShardReplication(cell, keyspace, shard); err != nil {
			return fmt.Errorf("error deleting ShardReplication object in cell %v: %v", cell, err)
		}

		// we keep going
	case topo.ErrNoNode:
		// no ShardReplication object, we keep going
	default:
		// we can't get the object, assume topo server is down there,
		// so we look at force flag
		if !force {
			return err
		}
		log.Warningf("Cannot get ShardReplication from cell %v, assuming cell topo server is down, and forcing the removal", cell)
	}

	// now we can update the shard
	log.Infof("Removing cell %v from shard %v/%v", cell, keyspace, shard)
	newCells := make([]string, 0, len(shardInfo.Cells)-1)
	for _, c := range shardInfo.Cells {
		if c != cell {
			newCells = append(newCells, c)
		}
	}
	shardInfo.Cells = newCells

	return wr.ts.UpdateShard(shardInfo)
}
