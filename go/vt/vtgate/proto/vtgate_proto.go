// Copyright 2012, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proto

import (
	"bytes"
	"fmt"

	"github.com/youtube/vitess/go/bson"
	"github.com/youtube/vitess/go/bytes2"
	mproto "github.com/youtube/vitess/go/mysql/proto"
	"github.com/youtube/vitess/go/sqltypes"
	tproto "github.com/youtube/vitess/go/vt/tabletserver/proto"
	"github.com/youtube/vitess/go/vt/topo"
)

// Session represents the session state. It keeps track of
// the shards on which transactions are in progress, along
// with the corresponding tranaction ids.
type Session struct {
	InTransaction bool
	ShardSessions []*ShardSession
}

// ShardSession represents the session state for a shard.
type ShardSession struct {
	Keyspace      string
	Shard         string
	TabletType    topo.TabletType
	TransactionId int64
}

// MarshalBson marshals Session into buf.
func (session *Session) MarshalBson(buf *bytes2.ChunkedWriter, key string) {
	bson.EncodeOptionalPrefix(buf, bson.Object, key)
	lenWriter := bson.NewLenWriter(buf)

	bson.EncodeBool(buf, "InTransaction", session.InTransaction)
	encodeShardSessionsBson(session.ShardSessions, "ShardSessions", buf)

	buf.WriteByte(0)
	lenWriter.RecordLen()
}

func (session *Session) String() string {
	return fmt.Sprintf("InTransaction: %v, ShardSession: %+v", session.InTransaction, session.ShardSessions)
}

func encodeShardSessionsBson(shardSessions []*ShardSession, key string, buf *bytes2.ChunkedWriter) {
	bson.EncodePrefix(buf, bson.Array, key)
	lenWriter := bson.NewLenWriter(buf)
	for i, v := range shardSessions {
		v.MarshalBson(buf, bson.Itoa(i))
	}
	buf.WriteByte(0)
	lenWriter.RecordLen()
}

// MarshalBson marshals ShardSession into buf.
func (shardSession *ShardSession) MarshalBson(buf *bytes2.ChunkedWriter, key string) {
	bson.EncodeOptionalPrefix(buf, bson.Object, key)
	lenWriter := bson.NewLenWriter(buf)

	bson.EncodeString(buf, "Keyspace", shardSession.Keyspace)
	bson.EncodeString(buf, "Shard", shardSession.Shard)
	bson.EncodeString(buf, "TabletType", string(shardSession.TabletType))
	bson.EncodeInt64(buf, "TransactionId", shardSession.TransactionId)

	buf.WriteByte(0)
	lenWriter.RecordLen()
}

// UnmarshalBson unmarshals Session from buf.
func (session *Session) UnmarshalBson(buf *bytes.Buffer, kind byte) {
	bson.VerifyObject(kind)
	bson.Next(buf, 4)

	kind = bson.NextByte(buf)
	for kind != bson.EOO {
		keyName := bson.ReadCString(buf)
		switch keyName {
		case "InTransaction":
			session.InTransaction = bson.DecodeBool(buf, kind)
		case "ShardSessions":
			session.ShardSessions = decodeShardSessionsBson(buf, kind)
		default:
			bson.Skip(buf, kind)
		}
		kind = bson.NextByte(buf)
	}
}

func decodeShardSessionsBson(buf *bytes.Buffer, kind byte) []*ShardSession {
	switch kind {
	case bson.Array:
		// valid
	case bson.Null:
		return nil
	default:
		panic(bson.NewBsonError("Unexpected data type %v for ShardSessions", kind))
	}

	bson.Next(buf, 4)
	shardSessions := make([]*ShardSession, 0, 8)
	kind = bson.NextByte(buf)
	for kind != bson.EOO {
		if kind != bson.Object {
			panic(bson.NewBsonError("Unexpected data type %v for ShardSession", kind))
		}
		bson.SkipIndex(buf)
		shardSession := new(ShardSession)
		shardSession.UnmarshalBson(buf, kind)
		shardSessions = append(shardSessions, shardSession)
		kind = bson.NextByte(buf)
	}
	return shardSessions
}

// UnmarshalBson unmarshals ShardSession from buf.
func (shardSession *ShardSession) UnmarshalBson(buf *bytes.Buffer, kind byte) {
	bson.VerifyObject(kind)
	bson.Next(buf, 4)

	kind = bson.NextByte(buf)
	for kind != bson.EOO {
		keyName := bson.ReadCString(buf)
		switch keyName {
		case "Keyspace":
			shardSession.Keyspace = bson.DecodeString(buf, kind)
		case "Shard":
			shardSession.Shard = bson.DecodeString(buf, kind)
		case "TabletType":
			shardSession.TabletType = topo.TabletType(bson.DecodeString(buf, kind))
		case "TransactionId":
			shardSession.TransactionId = bson.DecodeInt64(buf, kind)
		default:
			bson.Skip(buf, kind)
		}
		kind = bson.NextByte(buf)
	}
}

// QueryShard represents a query request for the
// specified list of shards.
type QueryShard struct {
	Sql           string
	BindVariables map[string]interface{}
	Keyspace      string
	Shards        []string
	TabletType    topo.TabletType
	Session       *Session
}

// MarshalBson marshals QueryShard into buf.
func (qrs *QueryShard) MarshalBson(buf *bytes2.ChunkedWriter, key string) {
	bson.EncodeOptionalPrefix(buf, bson.Object, key)
	lenWriter := bson.NewLenWriter(buf)

	bson.EncodeString(buf, "Sql", qrs.Sql)
	tproto.EncodeBindVariablesBson(buf, "BindVariables", qrs.BindVariables)
	bson.EncodeString(buf, "Keyspace", qrs.Keyspace)
	bson.EncodeStringArray(buf, "Shards", qrs.Shards)
	bson.EncodeString(buf, "TabletType", string(qrs.TabletType))

	if qrs.Session != nil {
		qrs.Session.MarshalBson(buf, "Session")
	}

	buf.WriteByte(0)
	lenWriter.RecordLen()
}

// UnmarshalBson unmarshals QueryShard from buf.
func (qrs *QueryShard) UnmarshalBson(buf *bytes.Buffer, kind byte) {
	bson.VerifyObject(kind)
	bson.Next(buf, 4)

	kind = bson.NextByte(buf)
	for kind != bson.EOO {
		keyName := bson.ReadCString(buf)
		switch keyName {
		case "Sql":
			qrs.Sql = bson.DecodeString(buf, kind)
		case "BindVariables":
			qrs.BindVariables = tproto.DecodeBindVariablesBson(buf, kind)
		case "Keyspace":
			qrs.Keyspace = bson.DecodeString(buf, kind)
		case "TabletType":
			qrs.TabletType = topo.TabletType(bson.DecodeString(buf, kind))
		case "Shards":
			qrs.Shards = bson.DecodeStringArray(buf, kind)
		case "Session":
			if kind != bson.Null {
				qrs.Session = new(Session)
				qrs.Session.UnmarshalBson(buf, kind)
			}
		default:
			bson.Skip(buf, kind)
		}
		kind = bson.NextByte(buf)
	}
}

// QueryResult is mproto.QueryResult+Session (for now).
type QueryResult struct {
	Fields       []mproto.Field
	RowsAffected uint64
	InsertId     uint64
	Rows         [][]sqltypes.Value
	Session      *Session
	Error        string
}

func PopulateQueryResult(in *mproto.QueryResult, out *QueryResult) {
	out.Fields = in.Fields
	out.RowsAffected = in.RowsAffected
	out.InsertId = in.InsertId
	out.Rows = in.Rows
}

// MarshalBson marshals QueryResult into buf.
func (qr *QueryResult) MarshalBson(buf *bytes2.ChunkedWriter, key string) {
	bson.EncodeOptionalPrefix(buf, bson.Object, key)
	lenWriter := bson.NewLenWriter(buf)

	mproto.EncodeFieldsBson(qr.Fields, "Fields", buf)
	bson.EncodeUint64(buf, "RowsAffected", qr.RowsAffected)
	bson.EncodeUint64(buf, "InsertId", qr.InsertId)
	mproto.EncodeRowsBson(qr.Rows, "Rows", buf)

	if qr.Session != nil {
		qr.Session.MarshalBson(buf, "Session")
	}

	if qr.Error != "" {
		bson.EncodeString(buf, "Error", qr.Error)
	}

	buf.WriteByte(0)
	lenWriter.RecordLen()
}

// UnmarshalBson unmarshals QueryResult from buf.
func (qr *QueryResult) UnmarshalBson(buf *bytes.Buffer, kind byte) {
	bson.VerifyObject(kind)
	bson.Next(buf, 4)

	kind = bson.NextByte(buf)
	for kind != bson.EOO {
		keyName := bson.ReadCString(buf)
		switch keyName {
		case "Fields":
			qr.Fields = mproto.DecodeFieldsBson(buf, kind)
		case "RowsAffected":
			qr.RowsAffected = bson.DecodeUint64(buf, kind)
		case "InsertId":
			qr.InsertId = bson.DecodeUint64(buf, kind)
		case "Rows":
			qr.Rows = mproto.DecodeRowsBson(buf, kind)
		case "Session":
			if kind != bson.Null {
				qr.Session = new(Session)
				qr.Session.UnmarshalBson(buf, kind)
			}
		case "Error":
			qr.Error = bson.DecodeString(buf, kind)
		default:
			bson.Skip(buf, kind)
		}
		kind = bson.NextByte(buf)
	}
}

// BatchQueryShard represents a batch query request
// for the specified shards.
type BatchQueryShard struct {
	Queries    []tproto.BoundQuery
	Keyspace   string
	Shards     []string
	TabletType topo.TabletType
	Session    *Session
}

// MarshalBson marshals BatchQueryShard into buf.
func (bqs *BatchQueryShard) MarshalBson(buf *bytes2.ChunkedWriter, key string) {
	bson.EncodeOptionalPrefix(buf, bson.Object, key)
	lenWriter := bson.NewLenWriter(buf)

	tproto.EncodeQueriesBson(bqs.Queries, "Queries", buf)
	bson.EncodeString(buf, "Keyspace", bqs.Keyspace)
	bson.EncodeStringArray(buf, "Shards", bqs.Shards)
	bson.EncodeString(buf, "TabletType", string(bqs.TabletType))

	if bqs.Session != nil {
		bqs.Session.MarshalBson(buf, "Session")
	}

	buf.WriteByte(0)
	lenWriter.RecordLen()
}

// UnmarshalBson unmarshals BatchQueryShard from buf.
func (bqs *BatchQueryShard) UnmarshalBson(buf *bytes.Buffer, kind byte) {
	bson.VerifyObject(kind)
	bson.Next(buf, 4)

	kind = bson.NextByte(buf)
	for kind != bson.EOO {
		keyName := bson.ReadCString(buf)
		switch keyName {
		case "Queries":
			bqs.Queries = tproto.DecodeQueriesBson(buf, kind)
		case "Keyspace":
			bqs.Keyspace = bson.DecodeString(buf, kind)
		case "Shards":
			bqs.Shards = bson.DecodeStringArray(buf, kind)
		case "TabletType":
			bqs.TabletType = topo.TabletType(bson.DecodeString(buf, kind))
		case "Session":
			if kind != bson.Null {
				bqs.Session = new(Session)
				bqs.Session.UnmarshalBson(buf, kind)
			}
		default:
			bson.Skip(buf, kind)
		}
		kind = bson.NextByte(buf)
	}
}

// QueryResultList is mproto.QueryResultList+Session
type QueryResultList struct {
	List    []mproto.QueryResult
	Session *Session
	Error   string
}

// MarshalBson marshals QueryResultList into buf.
func (qrl *QueryResultList) MarshalBson(buf *bytes2.ChunkedWriter, key string) {
	bson.EncodeOptionalPrefix(buf, bson.Object, key)
	lenWriter := bson.NewLenWriter(buf)

	tproto.EncodeResultsBson(qrl.List, "List", buf)

	if qrl.Session != nil {
		qrl.Session.MarshalBson(buf, "Session")
	}

	if qrl.Error != "" {
		bson.EncodeString(buf, "Error", qrl.Error)
	}

	buf.WriteByte(0)
	lenWriter.RecordLen()
}

// UnmarshalBson unmarshals QueryResultList from buf.
func (qrl *QueryResultList) UnmarshalBson(buf *bytes.Buffer, kind byte) {
	bson.VerifyObject(kind)
	bson.Next(buf, 4)

	kind = bson.NextByte(buf)
	for kind != bson.EOO {
		keyName := bson.ReadCString(buf)
		switch keyName {
		case "List":
			qrl.List = tproto.DecodeResultsBson(buf, kind)
		case "Session":
			if kind != bson.Null {
				qrl.Session = new(Session)
				qrl.Session.UnmarshalBson(buf, kind)
			}
		case "Error":
			qrl.Error = bson.DecodeString(buf, kind)
		default:
			bson.Skip(buf, kind)
		}
		kind = bson.NextByte(buf)
	}
}

type StreamQueryKeyRange struct {
	Sql           string
	BindVariables map[string]interface{}
	Keyspace      string
	KeyRange      string
	TabletType    topo.TabletType
	Session       *Session
}

func (sqs *StreamQueryKeyRange) MarshalBson(buf *bytes2.ChunkedWriter, key string) {
	bson.EncodeOptionalPrefix(buf, bson.Object, key)
	lenWriter := bson.NewLenWriter(buf)

	bson.EncodeString(buf, "Sql", sqs.Sql)
	tproto.EncodeBindVariablesBson(buf, "BindVariables", sqs.BindVariables)
	bson.EncodeString(buf, "Keyspace", sqs.Keyspace)
	bson.EncodeString(buf, "KeyRange", sqs.KeyRange)
	bson.EncodeString(buf, "TabletType", string(sqs.TabletType))

	if sqs.Session != nil {
		sqs.Session.MarshalBson(buf, "Session")
	}

	buf.WriteByte(0)
	lenWriter.RecordLen()
}

func (sqs *StreamQueryKeyRange) UnmarshalBson(buf *bytes.Buffer, kind byte) {
	bson.VerifyObject(kind)
	bson.Next(buf, 4)

	kind = bson.NextByte(buf)
	for kind != bson.EOO {
		keyName := bson.ReadCString(buf)
		switch keyName {
		case "Sql":
			sqs.Sql = bson.DecodeString(buf, kind)
		case "BindVariables":
			sqs.BindVariables = tproto.DecodeBindVariablesBson(buf, kind)
		case "Keyspace":
			sqs.Keyspace = bson.DecodeString(buf, kind)
		case "KeyRange":
			sqs.KeyRange = bson.DecodeString(buf, kind)
		case "TabletType":
			sqs.TabletType = topo.TabletType(bson.DecodeString(buf, kind))
		case "Session":
			if kind != bson.Null {
				sqs.Session = new(Session)
				sqs.Session.UnmarshalBson(buf, kind)
			}
		default:
			bson.Skip(buf, kind)
		}
		kind = bson.NextByte(buf)
	}
}
