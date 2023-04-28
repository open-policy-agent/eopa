package storage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/open-policy-agent/opa/bundle"
	bundleApi "github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/server/types"
	"github.com/open-policy-agent/opa/storage"
	"github.com/prometheus/client_golang/prometheus"

	bjson "github.com/styrainc/load-private/pkg/json"
	"github.com/styrainc/load-private/pkg/storage/inmem"
	"github.com/styrainc/load-private/pkg/vm"
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

type (
	// store implements a virtual store spanning a single
	// read-write-storage and multiple read-only storage backends.
	store struct {
		xid  uint64        // last generated transaction id
		root storage.Store // read-write root storage
		tree *tree         // attached read-only storages
		ops  vm.DataOperations

		dataPluginsMutex sync.Mutex
		dataPlugins      map[string]storage.Path // data plugins
	}

	Options struct {
		Stores []StoreOptions
	}

	// StoreOptions stores the configuration options for the attached store.
	StoreOptions struct {
		Paths   [][2]storage.Path // List of (base path, complete path) the attached storage handles.
		New     func(ctx context.Context, logger logging.Logger, prom prometheus.Registerer, opts interface{}) (storage.Store, error)
		Options interface{}
	}

	// transaction wraps multiple underlying transactions.
	transaction struct {
		xid          uint64
		params       []storage.TransactionParams
		store        *store
		tree         node
		mu           sync.Mutex
		transactions []nestedTransaction
		context      *storage.Context
	}

	nestedTransaction struct {
		store storage.Store
		txn   storage.Transaction
	}

	transactionContextKey struct{}
)

func init() {
	bundle.RegisterStore(New)
}

// New constructs a new store that can also act as a namespace for the evaluation VM.
func New() storage.Store {
	root := inmem.New()
	s, err := newInternal(context.Background(), nil, nil, root, Options{})
	if err != nil {
		panic(err)
	}
	return s
}

// NewFromObject returns a new in-memory store from the supplied data object.
func NewFromObject(data interface{}) storage.Store {
	v, ok := data.(bjson.Json)
	if !ok {
		v = bjson.MustNew(data)
	}

	if _, ok := v.(bjson.Object); !ok {
		panic("not reached")
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

func newInternal(ctx context.Context, logger logging.Logger, prom prometheus.Registerer, root storage.Store, opts Options) (storage.Store, error) {
	s := store{
		root:        root,
		tree:        newTree(nil, root),
		dataPlugins: map[string]storage.Path{},
	}

	for _, opt := range opts.Stores {
		store, err := opt.New(ctx, logger, prom, opt.Options)
		if err != nil {
			return nil, err
		}

		for _, path := range opt.Paths {
			path, complete := path[0], path[1]

			if len(path) == 0 {
				// For simplicity, the root is hardcoded to inmem storage. Eventually we can
				// support other storage types too.
				return nil, fmt.Errorf("too short path: %v", path)
			}

			if len(path) > len(complete) {
				return nil, fmt.Errorf("too long path: %v", path)
			}

			insert := path
			if len(path) == len(complete) {
				insert = insert[:len(insert)-1]
			}

			if err := s.tree.Insert(insert, newLazyTree(len(complete), path, store, nil)); err != nil {
				return nil, err
			}
		}
	}

	return &s, nil
}

func (s *store) Register(ctx context.Context, txn storage.Transaction, config storage.TriggerConfig) (storage.TriggerHandle, error) {
	onCommit := config.OnCommit

	// Use the virtual transaction instead of the root store's transaction.
	config.OnCommit = func(ctx context.Context, _ storage.Transaction, e storage.TriggerEvent) {
		onCommit(ctx, ctx.Value(transactionContextKey{}).(*transaction), e)
	}

	t, err := s.underlying(txn).dispatch(ctx, s.root, false)
	if err != nil {
		return nil, err
	}

	return s.root.Register(ctx, t, config)
}

func (s *store) ListPolicies(ctx context.Context, txn storage.Transaction) ([]string, error) {
	t, err := s.underlying(txn).dispatch(ctx, s.root, false)
	if err != nil {
		return nil, err
	}

	return s.root.ListPolicies(ctx, t)
}

func (s *store) GetPolicy(ctx context.Context, txn storage.Transaction, id string) ([]byte, error) {
	t, err := s.underlying(txn).dispatch(ctx, s.root, false)
	if err != nil {
		return nil, err
	}

	return s.root.GetPolicy(ctx, t, id)
}

func (s *store) UpsertPolicy(ctx context.Context, txn storage.Transaction, id string, bs []byte) error {
	t, err := s.underlying(txn).dispatch(ctx, s.root, true)
	if err != nil {
		return err
	}

	return s.root.UpsertPolicy(ctx, t, id, bs)
}

func (s *store) DeletePolicy(ctx context.Context, txn storage.Transaction, id string) error {
	t, err := s.underlying(txn).dispatch(ctx, s.root, true)
	if err != nil {
		return err
	}

	return s.root.DeletePolicy(ctx, t, id)
}

func (s *store) underlying(txn storage.Transaction) *transaction {
	return txn.(*transaction)
}

func (s *store) RegisterDataPlugin(name string, path storage.Path) {
	s.dataPluginsMutex.Lock()
	defer s.dataPluginsMutex.Unlock()

	if path == nil {
		delete(s.dataPlugins, name)
	} else {
		s.dataPlugins[name] = path
	}
}

// NewTransaction is called create a new transaction in the store.
func (s *store) NewTransaction(_ context.Context, params ...storage.TransactionParams) (storage.Transaction, error) {
	var context *storage.Context
	if len(params) > 0 {
		context = params[0].Context
	}
	xid := atomic.AddUint64(&s.xid, uint64(1))

	txn := &transaction{xid: xid, params: params, store: s, context: context}
	txn.tree = s.tree.Clone(txn)
	return txn, nil
}

// Read is called to fetch a document referred to by path.
func (s *store) Read(ctx context.Context, txn storage.Transaction, path storage.Path) (interface{}, error) {
	doc, err := s.underlying(txn).Read(ctx, path)
	if err != nil && !storage.IsNotFound(err) {
		panic(err)
	}

	return doc, err
}

func (s *store) ReadBJSON(ctx context.Context, txn storage.Transaction, path storage.Path) (bjson.Json, error) {
	doc, err := s.underlying(txn).ReadBJSON(ctx, path)
	if err != nil && !storage.IsNotFound(err) {
		panic(err)
	}

	return doc, err
}

func (s *store) WriteUnchecked(ctx context.Context, txn storage.Transaction, op storage.PatchOp, path storage.Path, doc interface{}) error {
	return s.underlying(txn).Write(ctx, op, path, doc)
}

func (s *store) checkDataPlugins(path storage.Path) error {
	s.dataPluginsMutex.Lock()
	defer s.dataPluginsMutex.Unlock()

	for name, plugin := range s.dataPlugins {
		if path.HasPrefix(plugin) {
			return types.BadRequestErr(fmt.Sprintf("path %q is owned by plugin %q", path.String(), name))
		}
	}
	return nil
}

// Write is called to modify a document referred to by path.
func (s *store) Write(ctx context.Context, txn storage.Transaction, op storage.PatchOp, path storage.Path, doc interface{}) error {
	// check dataplugins path
	if len(path) != 0 && s.dataPlugins != nil {
		if err := s.checkDataPlugins(path); err != nil {
			return err
		}
	}

	return s.underlying(txn).Write(ctx, op, path, doc)
}

// Commit is called to finish the transaction. If Commit returns an error, the
// transaction must be automatically aborted by the store implementation.
func (s *store) Commit(ctx context.Context, txn storage.Transaction) error {
	return s.underlying(txn).Commit(ctx)
}

// Truncate is called to make a copy of the underlying store, write documents in the new store
// by creating multiple transactions in the new store as needed and finally swapping
// over to the new storage instance. This method must be called within a transaction on the original store.
func (s *store) Truncate(ctx context.Context, txn storage.Transaction, params storage.TransactionParams, iter storage.Iterator) error {
	return s.underlying(txn).Truncate(ctx, params, iter)
}

// Abort is called to cancel the transaction.
func (s *store) Abort(ctx context.Context, txn storage.Transaction) {
	s.underlying(txn).Abort(ctx)
}

// store supports only traversing into the hierarchy.
func (txn *transaction) Get(ctx context.Context, key interface{}) (interface{}, bool, error) {
	return txn.tree.Get(ctx, key)
}

func (txn *transaction) Iter(ctx context.Context, f func(key, value interface{}) bool) error {
	return txn.tree.Iter(ctx, f)
}

func (txn *transaction) Len(ctx context.Context) (int, error) {
	return txn.tree.Len(ctx)
}

func (txn *transaction) ID() uint64 {
	return txn.xid
}

func (txn *transaction) Read(ctx context.Context, path storage.Path) (interface{}, error) {
	s, err := txn.tree.Find(path)
	if err != nil {
		return nil, err
	}

	t, err := txn.dispatch(ctx, s, false)
	if err != nil {
		return nil, err
	}

	return s.Read(ctx, t, path)
}

func (txn *transaction) ReadBJSON(ctx context.Context, path storage.Path) (bjson.Json, error) {
	s, err := txn.tree.Find(path)
	if err != nil {
		return nil, err
	}

	t, err := txn.dispatch(ctx, s, false)
	if err != nil {
		return nil, err
	}

	return s.(BJSONReader).ReadBJSON(ctx, t, path)
}

func (txn *transaction) Write(ctx context.Context, op storage.PatchOp, path storage.Path, doc interface{}) error {
	s, err := txn.tree.Find(path)
	if err != nil {
		switch err := err.(type) {
		case *storage.Error:
			if err.Code == readsNotSupportedErr {
				err.Code = storage.WritesNotSupportedErr
			}
		}

		return err
	}

	t, err := txn.dispatch(ctx, s, true)
	if err != nil {
		return err
	}

	return s.Write(ctx, t, op, path, doc)
}

func (txn *transaction) Commit(ctx context.Context) error {
	ctx = context.WithValue(ctx, transactionContextKey{}, txn)

	// TODO: Whats the preferred order, given only the root can be written?

	for _, t := range txn.transactions {
		if err := t.store.Commit(ctx, t.txn); err != nil {
			return err
		}
	}

	return nil
}

func (txn *transaction) Truncate(ctx context.Context, params storage.TransactionParams, iter storage.Iterator) error {
	// Only root supports writes, hence truncate only it.
	s := txn.store.root
	t, err := txn.dispatch(ctx, s, true)
	if err != nil {
		return err
	}
	return s.Truncate(ctx, t, params, iter)
}

func (txn *transaction) Abort(ctx context.Context) {
	for _, t := range txn.transactions {
		t.store.Abort(ctx, t.txn)
	}
}

func (txn *transaction) dispatch(ctx context.Context, s storage.Store, write bool) (storage.Transaction, error) {
	txn.mu.Lock()
	defer txn.mu.Unlock()

	// Only writes to root allowed.
	if write && s != txn.store.root {
		return nil, &storage.Error{
			Code: storage.WritesNotSupportedErr,
		}
	}

	for _, t := range txn.transactions {
		if t.store == s {
			return t.txn, nil
		}
	}

	t, err := s.NewTransaction(ctx, txn.params...)
	if err != nil {
		return nil, err
	}

	txn.transactions = append(txn.transactions, nestedTransaction{s, t})

	return t, nil
}

func (txn *transaction) read(ctx context.Context, store storage.Store, path storage.Path) (interface{}, bool, error) {
	var doc interface{}
	var err error

	t, err := txn.dispatch(ctx, store, false)
	if err != nil {
		return nil, false, err
	}

	switch s := store.(type) {
	case BJSONReader:
		doc, err = s.ReadBJSON(ctx, t, path)
	default:
		doc, err = s.Read(ctx, t, path)
	}

	if storage.IsNotFound(err) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}

	return doc, true, nil
}

// WriteUnchecked is a convenience function to invoke the write unchecked.
// It will create a new Transaction to perform the write with, and clean up after itself
func WriteUnchecked(ctx context.Context, store storage.Store, op storage.PatchOp, path storage.Path, value interface{}) error {
	return storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.(WriterUnchecked).WriteUnchecked(ctx, txn, op, path, value)
	})
}

func init() {
	bundleApi.RegisterStore(New)
}
