package sql

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"

	_ "github.com/lib/pq" // Include the PostgreSQL driver to the binary.
	"github.com/prometheus/client_golang/prometheus"
	_ "modernc.org/sqlite" // Include the SQLite3 driver to the binary.

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	bjson "github.com/styrainc/load-private/pkg/json"
)

type (

	// Store provides an SQL-based implementation of the storage.Store interface.
	Store struct {
		storage.PolicyNotSupported
		storage.TriggersNotSupported
		db         *sql.DB              // underlying SQL connection pool
		xid        uint64               // next transaction id
		rmu        sync.RWMutex         // reader-writer lock
		wmu        sync.Mutex           // writer lock
		smu        sync.Mutex           // statement cache lock
		statements map[string]*sql.Stmt // prepared statements
		tables     *tableTrie           // data structure to support path mapping
	}
)

// New returns a new SQL-based store based on the provided options. It
// does not validate the table configuration with the existing tables
// in the database.
func New(ctx context.Context, _ logging.Logger, prom prometheus.Registerer, options interface{}) (storage.Store, error) {
	opts := options.(Options)

	db, err := sql.Open(opts.Driver, opts.DataSourceName)
	if err != nil {
		return nil, wrapError(err)
	}

	if prom != nil {
		if err := initPrometheus(prom); err != nil {
			return nil, err
		}
	}

	tables := buildTablesTrie(opts.Tables)
	if tables == nil {
		var paths []storage.Path
		for _, table := range opts.Tables {
			paths = append(paths, table.Path)
		}

		return nil, &storage.Error{
			Code:    storage.InternalErr,
			Message: fmt.Sprintf("tables overlap: %v", paths),
		}
	}

	for _, table := range opts.Tables {
		if len(table.KeyColumns) == 0 || len(table.ValueColumns) == 0 {
			return nil, &storage.Error{
				Code:    storage.InternalErr,
				Message: fmt.Sprintf("table has invalid column(s): %v", table.Path),
			}
		}
	}

	for _, table := range opts.Tables {
		if table.discovered {
			continue
		}

		if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+table.Table+"("+table.Schema()+")"); err != nil {
			return nil, err
		}
	}

	return &Store{
		db:         db,
		statements: make(map[string]*sql.Stmt),
		tables:     tables,
	}, nil
}

// Close finishes the DB connection(s).
func (db *Store) Close(context.Context) error {
	return wrapError(db.db.Close())
}

// NewTransaction implements the storage.Store interface.
func (db *Store) NewTransaction(ctx context.Context, params ...storage.TransactionParams) (storage.Transaction, error) {
	var write bool
	var context *storage.Context

	if len(params) > 0 {
		write = params[0].Write
		context = params[0].Context
	}

	xid := atomic.AddUint64(&db.xid, uint64(1))
	if write {
		db.wmu.Lock() // TODO: why only one concurrent write txn?
	} else {
		db.rmu.RLock()
	}

	underlying, err := db.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: !write})
	if err != nil {
		return nil, err
	}

	return newTransaction(xid, write, underlying, context, db), nil
}

// Truncate implements the storage.Store interface. This method must be called within a transaction.
func (db *Store) Truncate(_ context.Context, _ storage.Transaction, _ storage.TransactionParams, _ storage.Iterator) error {
	return &storage.Error{
		Code:    storage.InternalErr,
		Message: "truncate not supported",
	}
}

// Commit implements the storage.Store interface.
func (db *Store) Commit(ctx context.Context, txn storage.Transaction) error {
	underlying, err := db.underlying(txn)
	if err != nil {
		return err
	}

	if underlying.write {
		db.rmu.Lock() // blocks until all readers are done

		defer db.wmu.Unlock()
		defer db.rmu.Unlock()

		if _, err := underlying.Commit(ctx); err != nil {
			return err
		}
	} else { // committing read txn
		defer db.rmu.RUnlock()
		underlying.Abort(ctx)
	}

	return nil
}

// Abort implements the storage.Store interface.
func (db *Store) Abort(ctx context.Context, txn storage.Transaction) {
	underlying, err := db.underlying(txn)
	if err != nil {
		panic(err)
	}

	underlying.Abort(ctx)

	if underlying.write {
		db.wmu.Unlock()
	} else {
		db.rmu.RUnlock()
	}
}

// Read implements the storage.Store interface.
func (db *Store) Read(ctx context.Context, txn storage.Transaction, path storage.Path) (interface{}, error) {
	underlying, err := db.underlying(txn)
	if err != nil {
		return nil, err
	}

	doc, err := underlying.Read(ctx, path)
	if err != nil {
		return nil, err
	}

	return doc.JSON(), nil
}

// ReadBJSON implements the storage.Store interface.
func (db *Store) ReadBJSON(ctx context.Context, txn storage.Transaction, path storage.Path) (bjson.Json, error) {
	underlying, err := db.underlying(txn)
	if err != nil {
		return nil, err
	}
	return underlying.Read(ctx, path)
}

// Write implements the storage.Store interface.
func (db *Store) Write(ctx context.Context, txn storage.Transaction, op storage.PatchOp, path storage.Path, value interface{}) error {
	underlying, err := db.underlying(txn)
	if err != nil {
		return err
	}

	val := util.Reference(value)
	if err := util.RoundTrip(val); err != nil {
		return wrapError(err)
	}

	return underlying.Write(ctx, op, path, *val)
}

// MakeDir makes Store a storage.MakeDirer, to avoid the superfluous MakeDir
// steps -- MakeDir is implicit in the disk storage's data layout.
//
// Here, we only check if it's a write transaction, for consistency with
// other implementations, and do nothing.
func (db *Store) MakeDir(_ context.Context, txn storage.Transaction, _ storage.Path) error {
	underlying, err := db.underlying(txn)
	if err != nil {
		return err
	}

	if !underlying.write {
		return &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "MakeDir must be called with a write transaction",
		}
	}
	return nil
}

func (db *Store) underlying(txn storage.Transaction) (*transaction, error) {
	underlying, ok := txn.(*transaction)
	if !ok {
		return nil, &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: fmt.Sprintf("unexpected transaction type %T", txn),
		}
	}
	if underlying.db != db {
		return nil, &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "unknown transaction",
		}
	}
	if underlying.stale {
		return nil, &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "stale transaction",
		}
	}
	return underlying, nil
}

// prepare looks for a cached prepared statement, preparing a new one as necessary.
func (db *Store) prepare(ctx context.Context, tx *sql.Tx, sql string) (*sql.Stmt, error) {
	db.smu.Lock()
	stmt, ok := db.statements[sql]
	if ok {
		db.smu.Unlock()
		return tx.StmtContext(ctx, stmt), nil
	}

	stmt, err := db.db.PrepareContext(ctx, sql)
	if err != nil {
		db.smu.Unlock()
		return nil, err
	}

	db.statements[sql] = stmt

	db.smu.Unlock()

	return tx.StmtContext(ctx, stmt), nil
}
