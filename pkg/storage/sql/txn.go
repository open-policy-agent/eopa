package sql

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

const (
	readValueBytesCounter = "disk_read_bytes"
	readKeysCounter       = "disk_read_keys"
	writtenKeysCounter    = "disk_written_keys"
	deletedKeysCounter    = "disk_deleted_keys"

	commitTimer = "disk_commit"
	readTimer   = "disk_read"
	writeTimer  = "disk_write"
)

type (
	transaction struct {
		underlying *sql.Tx              // handle for the underlying SQL transaction
		db         *Store               // handle for the database this transaction was created on
		xid        uint64               // unique id for this transaction
		stale      bool                 // bit to indicate if the transaction was already aborted/committed
		write      bool                 // bit to indicate if the transaction may perform writes
		event      storage.TriggerEvent // constructed as we go, supplied by the caller to be included in triggers
		metrics    metrics.Metrics      // per-transaction metrics
	}

	update struct {
		table  *TableOpt
		path   storage.Path
		value  interface{}
		delete bool
	}
)

func newTransaction(xid uint64, write bool, underlying *sql.Tx, context *storage.Context, db *Store) *transaction {
	// Even if the caller is not interested, these will contribute
	// to the prometheus metrics on commit.
	var m metrics.Metrics
	if context != nil {
		m = context.Metrics()
	}
	if m == nil {
		m = metrics.New()
	}

	return &transaction{
		underlying: underlying,
		db:         db,
		xid:        xid,
		stale:      false,
		write:      write,
		event: storage.TriggerEvent{
			Context: context,
		},
		metrics: m,
	}
}

func (txn *transaction) ID() uint64 {
	return txn.xid
}

// Commit will commit the underlying transaction, and forward the per-transaction
// metrics into prometheus metrics.
// NOTE(sr): aborted transactions are not measured
func (txn *transaction) Commit(context.Context) (storage.TriggerEvent, error) {
	txn.stale = true
	txn.metrics.Timer(commitTimer).Start()
	err := wrapError(txn.underlying.Commit())
	txn.metrics.Timer(commitTimer).Stop()

	if err != nil {
		return txn.event, err
	}

	m := txn.metrics.All()
	if txn.write {
		forwardMetric(m, readKeysCounter, keysReadPerStoreWrite)
		forwardMetric(m, readKeysCounter, keysReadPerStoreWrite)
		forwardMetric(m, writtenKeysCounter, keysWrittenPerStoreWrite)
		forwardMetric(m, deletedKeysCounter, keysDeletedPerStoreWrite)
		forwardMetric(m, readValueBytesCounter, bytesReadPerStoreWrite)
	} else {
		forwardMetric(m, readKeysCounter, keysReadPerStoreRead)
		forwardMetric(m, readValueBytesCounter, bytesReadPerStoreRead)
	}
	return txn.event, nil
}

func (txn *transaction) Abort(context.Context) {
	txn.stale = true
	txn.underlying.Rollback() // Nothing to do with the error.
}

// Read queries the tables required to return the document for the path.
func (txn *transaction) Read(ctx context.Context, path storage.Path) (fjson.Json, error) {
	txn.metrics.Timer(readTimer).Start()
	defer txn.metrics.Timer(readTimer).Stop()

	var result interface{}

	nothingFound := true
	for _, table := range txn.db.tables.Find(path) {
		rpath := table.RelativePath(path)

		r, err := txn.read(ctx, table, rpath)
		if storage.IsNotFound(err) {
			continue
		} else if err != nil {
			return nil, err
		}

		x := path
		if len(x) < len(table.base()) {
			x = table.base()[len(x):]
		} else {
			x = nil
		}

		result, err = patch(result, storage.AddOp, x, 0, r)
		if err != nil {
			return nil, err
		}

		nothingFound = false
	}

	if nothingFound && len(path) == 0 {
		// Root document always exists.
		return fjson.NewObject(nil), nil
	} else if nothingFound {
		return nil, errNotFound
	}

	jresult, err := fjson.New(result)
	if err != nil {
		return nil, err
	}

	return jresult, nil
}

// read queries a single table.
func (txn *transaction) read(ctx context.Context, table TableOpt, path storage.Path) (interface{}, error) {
	stmt, err := txn.db.prepare(ctx, txn.underlying, table.SelectQuery(path))
	if err != nil {
		return nil, err
	}

	args := table.SelectArgs(path)
	rows, err := stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, err
	}

	var result interface{}
	n := 0
	prefix := len(args)

	docPath := table.DocPath(path)

	for rows.Next() {
		// Extract the portion of the document pointed.

		path, doc, err := table.Scan(rows)
		if err != nil {
			if err := rows.Close(); err != nil {
				return nil, err
			}

			return nil, err
		}

		doc, err = Ptr(doc, docPath)
		if storage.IsNotFound(err) {
			continue
		}

		// Insert the row document into the result document.

		result, err = patch(result, storage.AddOp, path[prefix:], 0, doc)
		if err != nil {
			if err := rows.Close(); err != nil {
				return nil, err
			}

			return nil, err
		}

		n++
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if n == 0 {
		return nil, errNotFound
	}

	return result, nil
}

func (txn *transaction) Write(ctx context.Context, op storage.PatchOp, path storage.Path, value interface{}) error {
	txn.metrics.Timer(writeTimer).Start()
	defer txn.metrics.Timer(writeTimer).Stop()

	// Execute the write in two stages: first, identify all the
	// rows matching with the path and gather their updates;
	// second, apply the updates. This makes it unnecessary to
	// have two active, concurrent SQL queries, which is not
	// supported with SQLite.

	var updates []update
	for _, table := range txn.db.tables.Find(path) {
		rpath := table.RelativePath(path)

		ipath := append(table.base(), rpath...)[len(path):] // no-op if path is not shorter than table path.

		op := op
		value, err := Ptr(value, ipath)
		if storage.IsNotFound(err) {
			op = storage.RemoveOp
		} else if err != nil {
			return err
		}

		update, err := txn.update(ctx, table, op, rpath, value)
		if err != nil {
			return err
		}

		updates = append(updates, update...)
	}

	for _, u := range updates {
		stmt, err := txn.db.prepare(ctx, txn.underlying, u.table.DeleteQuery())
		if err != nil {
			return err
		}

		if _, err := stmt.ExecContext(ctx, u.table.DeleteArgs(u.path)...); err != nil {
			return err
		}

		if u.delete {
			txn.metrics.Counter(deletedKeysCounter).Add(1)
		} else {
			stmt, err := txn.db.prepare(ctx, txn.underlying, u.table.InsertQuery())
			if err != nil {
				return err
			}

			args, err := u.table.InsertArgs(u.path, u.value)
			if err != nil {
				return err
			}

			if _, err := stmt.ExecContext(ctx, args...); err != nil {
				return err
			}

			txn.metrics.Counter(writtenKeysCounter).Add(1)
		}

		txn.event.Data = append(txn.event.Data, storage.DataEvent{
			Path:    path,    // ?
			Data:    u.value, // nil if delete == true
			Removed: u.delete,
		})
	}

	return nil
}

// update identifies the required database row updates for the op.
func (txn *transaction) update(ctx context.Context, table TableOpt, op storage.PatchOp, path storage.Path, value interface{}) ([]update, error) {
	if n := len(table.KeyColumns); len(path) == n {
		if op == storage.RemoveOp {
			return []update{{table: &table, path: path, delete: true}}, nil
		}

		return []update{{table: &table, path: path, value: value}}, nil
	} else if len(path) > n {
		curr, err := txn.read(ctx, table, path[0:n])
		if err != nil {
			return nil, err
		}

		modified, err := patch(curr, op, path, n, value)
		if err != nil {
			return nil, err
		}

		return []update{{table: &table, path: path, value: modified}}, nil
	}

	// len(path) < n: it's a prefix, query to identify the exact rows to remove.

	stmt, err := txn.db.prepare(ctx, txn.underlying, table.SelectQuery(path))
	if err != nil {
		return nil, err
	}

	rows, err := stmt.QueryContext(ctx, table.SelectArgs(path)...)
	if err != nil {
		return nil, err
	}

	var updates []update
	for rows.Next() {
		path, _, err := table.Scan(rows)
		if err != nil {
			if err := rows.Close(); err != nil {
				return nil, err
			}
		}

		updates = append(updates, update{table: &table, path: path, delete: true})
		txn.metrics.Counter(readKeysCounter).Add(1)
	}

	// False returning Next closes the rows.

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if op == storage.RemoveOp {
		return updates, nil
	}

	return txn.partitionWriteMultiple(&table, path, value, updates)
}

func (txn *transaction) partitionWriteMultiple(table *TableOpt, path storage.Path, value interface{}, result []update) ([]update, error) {
	// NOTE(tsandall): value must be an object so that it can be partitioned; in
	// the future, arrays could be supported but that requires investigation.

	switch v := value.(type) {
	case map[string]interface{}:
		bs, err := serialize(v)
		if err != nil {
			return nil, err
		}
		return txn.doPartitionWriteMultiple(table, path, bs, result)
	case map[string]json.RawMessage:
		bs, err := serialize(v)
		if err != nil {
			return nil, err
		}
		return txn.doPartitionWriteMultiple(table, path, bs, result)
	case json.RawMessage:
		return txn.doPartitionWriteMultiple(table, path, v, result)
	case []uint8:
		return txn.doPartitionWriteMultiple(table, path, v, result)
	}

	return nil, &storage.Error{Code: storage.InvalidPatchErr, Message: "value cannot be partitioned"}
}

func (txn *transaction) doPartitionWriteMultiple(table *TableOpt, path storage.Path, bs []byte, result []update) ([]update, error) {
	var obj map[string]json.RawMessage
	err := util.Unmarshal(bs, &obj)
	if err != nil {
		return nil, &storage.Error{Code: storage.InvalidPatchErr, Message: "value cannot be partitioned"}
	}

	for k, v := range obj {
		child := append(path, k)
		if len(child) < len(table.KeyColumns) {
			var err error
			result, err = txn.partitionWriteMultiple(table, child, v, result)
			if err != nil {
				return nil, err
			}

			continue
		}

		result = append(result, update{table: table, path: child, value: v})
	}

	return result, nil
}

func serialize(value interface{}) ([]byte, error) {
	val, ok := value.([]byte)
	if ok {
		return val, nil
	}

	bs, err := json.Marshal(value)
	return bs, wrapError(err)
}

func deserialize(bs []byte, result interface{}) error {
	d := util.NewJSONDecoder(bytes.NewReader(bs))
	return wrapError(d.Decode(&result))
}

func patch(data interface{}, op storage.PatchOp, path storage.Path, idx int, value interface{}) (interface{}, error) {
	if idx == len(path) {
		return value, nil
	}

	val := value
	switch v := value.(type) {
	case json.RawMessage:
		var obj map[string]json.RawMessage
		err := util.Unmarshal(v, &obj)
		if err == nil {
			val = obj
		} else {
			var obj interface{}
			err := util.Unmarshal(v, &obj)
			if err != nil {
				return nil, err
			}
			val = obj
		}
	case []uint8:
		var obj map[string]json.RawMessage
		err := util.Unmarshal(v, &obj)
		if err == nil {
			val = obj
		} else {
			var obj interface{}
			err := util.Unmarshal(v, &obj)
			if err != nil {
				return nil, err
			}
			val = obj
		}
	}

	// Base case: mutate the data value in-place.
	if len(path) == idx+1 { // last element
		switch x := data.(type) {
		case map[string]interface{}:
			key := path[len(path)-1]
			switch op {
			case storage.RemoveOp:
				if _, ok := x[key]; !ok {
					return nil, NewNotFoundError(path)
				}
				delete(x, key)
				return x, nil
			case storage.ReplaceOp:
				if _, ok := x[key]; !ok {
					return nil, NewNotFoundError(path)
				}
				x[key] = val
				return x, nil
			case storage.AddOp:
				x[key] = val
				return x, nil
			}
		case []interface{}:
			switch op {
			case storage.AddOp:
				if path[idx] == "-" || path[idx] == strconv.Itoa(len(x)) {
					return append(x, val), nil
				}
				i, err := ValidateArrayIndexForWrite(x, path[idx], idx, path)
				if err != nil {
					return nil, err
				}
				// insert at i
				return append(x[:i], append([]interface{}{val}, x[i:]...)...), nil
			case storage.ReplaceOp:
				i, err := ValidateArrayIndexForWrite(x, path[idx], idx, path)
				if err != nil {
					return nil, err
				}
				x[i] = val
				return x, nil
			case storage.RemoveOp:
				i, err := ValidateArrayIndexForWrite(x, path[idx], idx, path)
				if err != nil {
					return nil, err

				}
				return append(x[:i], x[i+1:]...), nil // i is skipped
			default:
				panic("unreachable")
			}
		case nil: // data wasn't set before
			return map[string]interface{}{path[idx]: val}, nil
		default:
			return nil, NewNotFoundError(path)
		}
	}

	// Recurse on the value located at the next part of the path.
	key := path[idx]

	switch x := data.(type) {
	case map[string]interface{}:
		modified, err := patch(x[key], op, path, idx+1, val)
		if err != nil {
			return nil, err
		}
		x[key] = modified
		return x, nil
	case []interface{}:
		i, err := ValidateArrayIndexForWrite(x, path[idx], idx+1, path)
		if err != nil {
			return nil, err
		}
		modified, err := patch(x[i], op, path, idx+1, val)
		if err != nil {
			return nil, err
		}
		x[i] = modified
		return x, nil
	case nil: // data isn't there yet
		y := make(map[string]interface{}, 1)
		modified, err := patch(nil, op, path, idx+1, val)
		if err != nil {
			return nil, err
		}
		y[key] = modified
		return y, nil
	default:
		return nil, NewNotFoundError(path)
	}
}
