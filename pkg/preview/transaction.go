package preview

import (
	"context"

	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

// PreviewTransaction abstracts over two potential transactions within a PreviewStore
type PreviewTransaction struct {
	primaryTransaction storage.Transaction
	previewTransaction storage.Transaction
	store              *PreviewStorage
	xid                uint64
}

// ID returns the transaction ID assigned to the PreviewTransaction
func (t *PreviewTransaction) ID() uint64 {
	return t.xid
}

// Get will find the value at the provided key, first iterating the preview store
// transaction, falling back to the primary store transaction in the event the key
// is not available from the preview transaction.
func (t *PreviewTransaction) Get(ctx context.Context, key interface{}) (interface{}, bool, error) {
	var value any
	found := false

	f := func(iterable vm.IterableObject) error {
		v, f, err := iterable.Get(ctx, key)
		if err != nil {
			return err
		}
		value = v
		found = f
		return nil
	}

	err := t.asIterable(t.previewTransaction, f)
	if err != nil {
		return nil, false, err
	}
	if found {
		return value, found, nil
	}
	err = t.asIterable(t.primaryTransaction, f)
	return value, found, err
}

// Iter will iterate over all elements of both the preview and primary transactions
// when defined, calling `f` for each element. If true is returned from the callback
// iteration stops
//
// This method does not deduplicate when a key is declared in both transactions.
//
// Preview data is iterated before primary data
func (t *PreviewTransaction) Iter(ctx context.Context, f func(key, value any) bool) error {
	found := false
	iFunc := func(iterable vm.IterableObject) error {
		return iterable.Iter(ctx, func(k, v any) bool {
			found = f(k, v)
			return found
		})
	}

	err := t.asIterable(t.previewTransaction, iFunc)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	err = t.asIterable(t.primaryTransaction, iFunc)
	if err != nil {
		return err
	}
	return nil
}

// Len will provide the total combined length of both the preview and primary transactions.
//
// This method does not deduplicate when a key is declared in both transactions.
func (t *PreviewTransaction) Len(ctx context.Context) (int, error) {
	final := 0
	f := func(iterable vm.IterableObject) error {
		iLen, err := iterable.Len(ctx)
		if err != nil {
			return err
		}
		final += iLen
		return nil
	}
	err := t.asIterable(t.previewTransaction, f)
	if err != nil {
		return 0, err
	}
	err = t.asIterable(t.primaryTransaction, f)
	if err != nil {
		return 0, err
	}

	return final, nil
}

func (t *PreviewTransaction) Hash(ctx context.Context) (uint64, error) {
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

// asIterable type asserts the provided transaction matches the vm.IterableObject interface,
// and if it does, it will call `f` supplying the transaction as a vm.IterableObject.
func (t *PreviewTransaction) asIterable(txn storage.Transaction, f func(vm.IterableObject) error) error {
	if txn == nil {
		return nil
	}
	if iterable, ok := txn.(vm.IterableObject); ok {
		return f(iterable)
	}
	return nil
}
