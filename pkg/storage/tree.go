package storage

import (
	"context"
	"errors"

	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

// errConflict indicates a conflicting path existed in the tree and insert failed. Currently it is not
// exposed to endusers; once this changes, improve the error with details.
var errConflict = errors.New("conflict")

type (
	// node is the interface all the elements in the namespace tree implement.
	node interface {
		vm.IterableObject
		Find(path storage.Path) (storage.Store, error)
		Insert(path storage.Path, child node) error
		Clone(txn *transaction) node
	}

	tree struct {
		children map[string]node
		path     storage.Path
		store    storage.Store
		txn      *transaction
	}

	// lazyTree delays the invocation of the storage read until
	// iteration or path is complete enough. This is to prefer
	// (cheaper) precise reads over less precise (more
	// expensive) reads.
	lazyTree struct {
		complete int
		path     storage.Path
		store    storage.Store
		txn      *transaction
	}
)

func newTree(path storage.Path, store storage.Store) *tree {
	return &tree{children: make(map[string]node), path: path, store: store}
}

func (n *tree) Insert(path storage.Path, child node) error {
	if len(path) == 1 {
		key := path[0]
		n.children[key] = child
		return nil
	}

	key := path[0]
	c, ok := n.children[key]
	if !ok {
		c = newTree(append(n.path, key), nil)
		n.children[key] = c
	}

	return c.Insert(path[1:], child)
}

func (n *tree) Get(ctx context.Context, key interface{}) (interface{}, bool, error) {
	skey, ok := key.(string)
	if !ok {
		k, err := n.txn.store.ops.ToInterface(ctx, key)
		if err != nil {
			return nil, false, err
		}

		skey, ok = k.(string)
		if !ok {
			return nil, false, err
		}
	}

	if child, ok := n.children[skey]; ok {
		return child.Clone(n.txn), true, nil
	}

	if len(n.children) == 0 && n.store != nil {
		return n.txn.read(ctx, n.store, append(n.path, skey))
	}

	return nil, false, nil
}

func (n *tree) Iter(ctx context.Context, f func(key, value interface{}) bool) error {
	if len(n.children) == 0 && n.store != nil {
		v, ok, err := n.txn.read(ctx, n.store, n.path)
		if err != nil {
			return err
		} else if !ok {
			return nil
		}

		return n.txn.store.ops.Iter(ctx, v, f)
	}

	return &storage.Error{
		Code:    readsNotSupportedErr,
		Message: n.path.String(),
	}
}

func (n *tree) Len(ctx context.Context) (int, error) {
	i := 0
	err := n.Iter(ctx, func(_, _ interface{}) bool {
		i++
		return false
	})
	return i, err
}

func (n *tree) Hash(ctx context.Context) (uint64, error) {
	var h uint64
	var err2 error
	if err := n.Iter(ctx, func(k, v interface{}) bool {
		h, err2 = vm.ObjectHashEntry(ctx, h, k, v)
		return err2 != nil
	}); err != nil {
		return 0, err
	} else if err2 != nil {
		return 0, err2
	}

	return h, nil
}

// Find returns the store corresping the path.
func (n *tree) Find(path storage.Path) (storage.Store, error) {
	if len(path) == 0 {
		if len(n.children) == 0 {
			return n.store, nil
		}

		// Path is not specific enough to resolve the ambiguity.
		return nil, &storage.Error{
			Code:    readsNotSupportedErr,
			Message: n.path.String(),
		}

	}

	if child, ok := n.children[path[0]]; ok {
		return child.Find(path[1:])
	}

	if n.store == nil {
		return nil, &storage.Error{
			Code:    readsNotSupportedErr,
			Message: storage.Path(append(n.path, path...)).String(),
		}
	}

	return n.store, nil
}

func (n tree) Clone(txn *transaction) node {
	n.txn = txn
	return &n
}

func newLazyTree(columns int, path storage.Path, store storage.Store, txn *transaction) node {
	return &lazyTree{columns, path, store, txn}
}

func (t *lazyTree) Get(ctx context.Context, key interface{}) (interface{}, bool, error) {
	skey, ok := key.(string)
	if !ok {
		k, err := t.txn.store.ops.ToInterface(ctx, key)
		if err != nil {
			return nil, false, err
		}

		skey, ok = k.(string)
		if !ok {
			return nil, false, nil
		}
	}

	path := make([]string, len(t.path)+1)
	copy(path, t.path)
	path[len(t.path)] = skey

	if len(path) == t.complete {
		return t.txn.read(ctx, t.store, path)
	}

	lt := *t
	lt.path = path
	return &lt, true, nil
}

func (t *lazyTree) Find(storage.Path) (storage.Store, error) {
	return t.store, nil
}

func (t *lazyTree) Iter(ctx context.Context, f func(key, value interface{}) bool) error {
	// TODO: Ideally the store would have a native iteration
	// interface, to avoid loading the whole document in memory at
	// once.

	doc, ok, err := t.txn.read(ctx, t.store, t.path)
	if err != nil {
		return err
	} else if !ok {
		return nil
	}

	return t.txn.store.ops.Iter(ctx, doc, f)
}

func (t *lazyTree) Len(ctx context.Context) (int, error) {
	n := 0
	err := t.Iter(ctx, func(_, _ interface{}) bool {
		n++
		return false
	})
	return n, err
}

func (t *lazyTree) Hash(ctx context.Context) (uint64, error) {
	var h uint64
	var err2 error
	if err := t.Iter(ctx, func(k, v interface{}) bool {
		h, err2 = vm.ObjectHashEntry(ctx, h, k, v)
		return err2 != nil
	}); err != nil {
		return 0, err
	} else if err2 != nil {
		return 0, err2
	}

	return h, nil
}

func (t *lazyTree) Insert(storage.Path, node) error {
	return errConflict
}

func (t lazyTree) Clone(txn *transaction) node {
	t.txn = txn
	return &t
}
