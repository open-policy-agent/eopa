// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package inmem implements an in-memory version of the policy engine's storage
// layer.
//
// The in-memory store is used as the default storage layer implementation. The
// in-memory store supports multi-reader/single-writer concurrency with
// rollback.
//
// Callers should assume the in-memory store does not make copies of written
// data. Once data is written to the in-memory store, it should not be modified
// (outside of calling Store.Write). Furthermore, data read from the in-memory
// store should be treated as read-only.
package inmem

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	bjson "github.com/styrainc/load-private/pkg/json"
	"github.com/styrainc/load-private/pkg/plugins/bundle"
	"github.com/styrainc/load-private/pkg/store/internal/merge"

	bundleApi "github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/server/types"
	"github.com/open-policy-agent/opa/storage"
)

type BJSONReader interface {
	ReadBJSON(context.Context, storage.Transaction, storage.Path) (bjson.Json, error)
}

type WriterUnchecked interface {
	WriteUnchecked(context.Context, storage.Transaction, storage.PatchOp, storage.Path, interface{}) error
}

type DataPlugins interface {
	RegisterDataPlugin(name string, path storage.Path)
}

// New returns an empty in-memory store.
func New() storage.Store {
	return &store{
		data:        bjson.NewObject(nil),
		triggers:    map[*handle]storage.TriggerConfig{},
		policies:    map[string][]byte{},
		dataPlugins: map[string]storage.Path{},
	}
}

// NewFromObject returns a new in-memory store from the supplied data object.
func NewFromObject(data interface{}) storage.Store {
	v, ok := data.(bjson.Json)
	if !ok {
		v = bjson.MustNew(data)
	}

	if _, ok := v.(bjson.Object); !ok {
		panic("XXX")
	}

	db := New()
	ctx := context.Background()
	txn, err := db.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		panic(err)
	}
	if err := db.Write(ctx, txn, storage.AddOp, storage.Path{}, v); err != nil {
		panic(err)
	}
	if err := db.Commit(ctx, txn); err != nil {
		panic(err)
	}
	return db
}

// NewFromReader returns a new in-memory store from a reader that produces a
// JSON serialized object. This function is for test purposes.
func NewFromReader(r io.Reader) storage.Store {
	data, err := bjson.NewDecoder(r).Decode()
	if err != nil {
		panic(err)
	}
	return NewFromObject(data.(bjson.Object))
}

type store struct {
	rmu      sync.RWMutex                      // reader-writer lock
	wmu      sync.Mutex                        // writer lock
	xid      uint64                            // last generated transaction id
	data     bjson.Json                        // raw data
	policies map[string][]byte                 // raw policies
	triggers map[*handle]storage.TriggerConfig // registered triggers

	dataPluginsMutex sync.Mutex
	dataPlugins      map[string]storage.Path // data plugins
}

type handle struct {
	db *store
}

func (db *store) RegisterDataPlugin(name string, path storage.Path) {
	db.dataPluginsMutex.Lock()
	defer db.dataPluginsMutex.Unlock()

	if path == nil {
		delete(db.dataPlugins, name)
	} else {
		db.dataPlugins[name] = path
	}
}

func (db *store) NewTransaction(_ context.Context, params ...storage.TransactionParams) (storage.Transaction, error) {
	var write bool
	var context *storage.Context
	if len(params) > 0 {
		write = params[0].Write
		context = params[0].Context
	}
	xid := atomic.AddUint64(&db.xid, uint64(1))
	if write {
		db.wmu.Lock()
	} else {
		db.rmu.RLock()
	}
	return newTransaction(xid, write, context, db), nil
}

// Truncate implements the storage.Store interface. This method must be called within a transaction.
func (db *store) Truncate(ctx context.Context, txn storage.Transaction, params storage.TransactionParams, it storage.Iterator) error {
	var update *storage.Update
	var err error
	mergedData := bjson.NewObject(nil)

	underlying, err := db.underlying(txn)
	if err != nil {
		return err
	}

	for {
		update, err = it.Next()
		if err != nil {
			break
		}

		if update.IsPolicy {
			err = underlying.UpsertPolicy(strings.TrimLeft(update.Path.String(), "/"), update.Value)
			if err != nil {
				return err
			}
		} else {
			value, err := bundle.BjsonFromBinary(update.Value)
			if err != nil {
				return err
			}

			var key []string
			dirpath := strings.TrimLeft(update.Path.String(), "/")
			if len(dirpath) > 0 {
				key = strings.Split(dirpath, "/")
			}

			if value != nil {
				obj, err := mktree(key, value)
				if err != nil {
					return err
				}

				merged, ok := merge.InterfaceMaps(mergedData, obj)
				if !ok {
					return fmt.Errorf("failed to insert data file from path %s", filepath.Join(key...))
				}
				mergedData = merged
			}
		}
	}

	if err != nil && err != io.EOF {
		return err
	}

	// For backwards compatibility, check if `RootOverwrite` was configured.
	if params.RootOverwrite {
		newPath, ok := storage.ParsePathEscaped("/")
		if !ok {
			return fmt.Errorf("storage path invalid: %v", newPath)
		}
		return underlying.Write(storage.AddOp, newPath, mergedData)
	}

	for _, root := range params.BasePaths {
		newPath, ok := storage.ParsePathEscaped("/" + root)
		if !ok {
			return fmt.Errorf("storage path invalid: %v", newPath)
		}

		if value, ok := lookup(newPath, mergedData); ok {
			if len(newPath) > 0 {
				if err := storage.MakeDir(ctx, db, txn, newPath[:len(newPath)-1]); err != nil {
					return err
				}
			}
			if err := underlying.Write(storage.AddOp, newPath, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (db *store) Commit(ctx context.Context, txn storage.Transaction) error {
	underlying, err := db.underlying(txn)
	if err != nil {
		return err
	}
	if underlying.write {
		db.rmu.Lock()
		event := underlying.Commit()
		db.runOnCommitTriggers(ctx, txn, event)
		// Mark the transaction stale after executing triggers so they can
		// perform store operations if needed.
		underlying.stale = true
		db.rmu.Unlock()
		db.wmu.Unlock()
	} else {
		db.rmu.RUnlock()
	}
	return nil
}

func (db *store) Abort(_ context.Context, txn storage.Transaction) {
	underlying, err := db.underlying(txn)
	if err != nil {
		panic(err)
	}
	underlying.stale = true
	if underlying.write {
		db.wmu.Unlock()
	} else {
		db.rmu.RUnlock()
	}
}

func (db *store) ListPolicies(_ context.Context, txn storage.Transaction) ([]string, error) {
	underlying, err := db.underlying(txn)
	if err != nil {
		return nil, err
	}
	return underlying.ListPolicies(), nil
}

func (db *store) GetPolicy(_ context.Context, txn storage.Transaction, id string) ([]byte, error) {
	underlying, err := db.underlying(txn)
	if err != nil {
		return nil, err
	}
	return underlying.GetPolicy(id)
}

func (db *store) UpsertPolicy(_ context.Context, txn storage.Transaction, id string, bs []byte) error {
	underlying, err := db.underlying(txn)
	if err != nil {
		return err
	}
	return underlying.UpsertPolicy(id, bs)
}

func (db *store) DeletePolicy(_ context.Context, txn storage.Transaction, id string) error {
	underlying, err := db.underlying(txn)
	if err != nil {
		return err
	}
	if _, err := underlying.GetPolicy(id); err != nil {
		return err
	}
	return underlying.DeletePolicy(id)
}

func (db *store) Register(_ context.Context, txn storage.Transaction, config storage.TriggerConfig) (storage.TriggerHandle, error) {
	underlying, err := db.underlying(txn)
	if err != nil {
		return nil, err
	}
	if !underlying.write {
		return nil, &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "triggers must be registered with a write transaction",
		}
	}
	h := &handle{db}
	db.triggers[h] = config
	return h, nil
}

func (db *store) Read(_ context.Context, txn storage.Transaction, path storage.Path) (interface{}, error) {
	underlying, err := db.underlying(txn)
	if err != nil {
		return nil, err
	}
	u, err := underlying.Read(path)
	if err != nil {
		return nil, err
	}
	return u.(bjson.Json).JSON(), nil
}

func (db *store) ReadBJSON(_ context.Context, txn storage.Transaction, path storage.Path) (bjson.Json, error) {
	underlying, err := db.underlying(txn)
	if err != nil {
		return nil, err
	}
	u, err := underlying.Read(path)
	if err != nil {
		return nil, err
	}
	return u.(bjson.Json), nil
}

func (db *store) WriteUnchecked(_ context.Context, txn storage.Transaction, op storage.PatchOp, path storage.Path, value interface{}) error {
	underlying, err := db.underlying(txn)
	if err != nil {
		return err
	}
	v, ok := value.(bjson.Json)
	if !ok {
		v = bjson.MustNew(value)
	}

	return underlying.Write(op, path, v.Clone(true).(bjson.Json))
}

func (db *store) checkDataPlugins(path storage.Path) error {
	db.dataPluginsMutex.Lock()
	defer db.dataPluginsMutex.Unlock()

	for name, plugin := range db.dataPlugins {
		if path.HasPrefix(plugin) {
			return types.BadRequestErr(fmt.Sprintf("path %q is owned by plugin %q", path.String(), name))
		}
	}
	return nil
}

func (db *store) Write(ctx context.Context, txn storage.Transaction, op storage.PatchOp, path storage.Path, value interface{}) error {
	// check dataplugins path
	if len(path) != 0 && db.dataPlugins != nil {
		if err := db.checkDataPlugins(path); err != nil {
			return err
		}
	}

	return db.WriteUnchecked(ctx, txn, op, path, value)
}

// WriteUnchecked is a convenience function to invoke the write unchecked.
// It will create a new Transaction to perform the write with, and clean up after itself
func WriteUnchecked(ctx context.Context, store storage.Store, op storage.PatchOp, path storage.Path, value interface{}) error {
	return storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.(WriterUnchecked).WriteUnchecked(ctx, txn, op, path, value)
	})
}

func (h *handle) Unregister(_ context.Context, txn storage.Transaction) {
	underlying, err := h.db.underlying(txn)
	if err != nil {
		panic(err)
	}
	if !underlying.write {
		panic(&storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "triggers must be unregistered with a write transaction",
		})
	}
	delete(h.db.triggers, h)
}

func (db *store) runOnCommitTriggers(ctx context.Context, txn storage.Transaction, event storage.TriggerEvent) {
	for _, t := range db.triggers {
		t.OnCommit(ctx, txn, event)
	}
}

func (db *store) underlying(txn storage.Transaction) (*transaction, error) {
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

const rootMustBeObjectMsg = "root must be object"
const rootCannotBeRemovedMsg = "root cannot be removed"

func invalidPatchError(f string, a ...interface{}) *storage.Error {
	return &storage.Error{
		Code:    storage.InvalidPatchErr,
		Message: fmt.Sprintf(f, a...),
	}
}

func mktree(path []string, value bjson.Json) (bjson.Object, error) {
	if len(path) == 0 {
		// For 0 length path the value is the full tree.
		obj, ok := value.(bjson.Object)
		if !ok {
			return nil, invalidPatchError(rootMustBeObjectMsg)
		}
		return obj, nil
	}

	dir := bjson.NewObject(nil)
	for i := len(path) - 1; i > 0; i-- {
		dir.Set(path[i], value)
		value = dir
		dir = bjson.NewObject(nil)
	}
	dir.Set(path[0], value)

	return dir, nil
}

func lookup(path storage.Path, data bjson.Object) (bjson.Json, bool) {
	if len(path) == 0 {
		return data, true
	}
	for i := 0; i < len(path)-1; i++ {
		value := data.Value(path[i])
		if value == nil {
			return nil, false
		}
		obj, ok := value.(bjson.Object)
		if !ok {
			return nil, false
		}
		data = obj
	}
	value := data.Value(path[len(path)-1])
	return value, value != nil
}

func (db *store) MakeDir(ctx context.Context, txn storage.Transaction, path storage.Path) error {
	if len(path) == 0 {
		return nil
	}

	node, err := db.ReadBJSON(ctx, txn, path)
	if err != nil {
		if !storage.IsNotFound(err) {
			return err
		}

		if err := db.MakeDir(ctx, txn, path[:len(path)-1]); err != nil {
			return err
		}

		return db.Write(ctx, txn, storage.AddOp, path, bjson.NewObject(nil))
	}

	if _, ok := node.(bjson.Object); ok {
		return nil
	}

	return writeConflictError(path)
}

func writeConflictError(path storage.Path) *storage.Error {
	return &storage.Error{
		Code:    storage.WriteConflictErr,
		Message: path.String(),
	}
}

func (db *store) NonEmpty(ctx context.Context, txn storage.Transaction) func([]string) (bool, error) {
	return func(path []string) (bool, error) {
		if _, err := db.ReadBJSON(ctx, txn, storage.Path(path)); err == nil {
			return true, nil
		} else if !storage.IsNotFound(err) {
			return false, err
		}
		for i := len(path) - 1; i > 0; i-- {
			val, err := db.ReadBJSON(ctx, txn, storage.Path(path[:i]))
			if err != nil && !storage.IsNotFound(err) {
				return false, err
			} else if err == nil {
				if _, ok := val.(bjson.Object); ok {
					return false, nil
				}
				return true, nil
			}
		}
		return false, nil
	}
}

func init() {
	bundleApi.RegisterStore(New)
}
