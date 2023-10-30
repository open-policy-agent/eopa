package vm

import (
	"context"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

var (
	_          fjson.Json     = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_          fjson.Json     = (*objectLarge)(nil)
	_          IterableObject = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_          IterableObject = (*objectLarge)(nil)
	_          Object         = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_          Object         = (*objectLarge)(nil)
	zeroObject Object         = &objectCompact[[0]hashObjectCompactEntry]{}
)

type T = interface{}

type Object interface {
	fjson.Json
	Insert(ctx context.Context, k, v interface{}) (Object, error)
	Append(hash uint64, k, v interface{}) error
	Get(ctx context.Context, k interface{}) (interface{}, bool, error)
	Iter(ctx context.Context, iter func(key, value interface{}) (bool, error)) error
	Len(context.Context) (int, error)
	Hash(ctx context.Context) (uint64, error)
}

func NewObject() Object {
	return newObject(0)
}

func newObject(n int) Object {
	switch {
	case n == 0:
		return zeroObject
	case n <= 2:
		return &objectCompact[[2]hashObjectCompactEntry]{}
	case n <= 4:
		return &objectCompact[[4]hashObjectCompactEntry]{}
	case n <= 8:
		return &objectCompact[[8]hashObjectCompactEntry]{}
	case n <= 16:
		return &objectCompact[[16]hashObjectCompactEntry]{}
	default:
		return newObjectLarge(n)
	}
}

// objectLarge is the default implementation.
type objectLarge struct {
	fjson.Json
	table map[uint64]*hashLargeEntry
	n     int
}

type hashLargeEntry struct {
	k    T
	v    T
	next *hashLargeEntry
}

func newObjectLarge(n int) *objectLarge {
	return &objectLarge{table: make(map[uint64]*hashLargeEntry, n)}
}

func (o *objectLarge) Insert(ctx context.Context, k, v interface{}) (Object, error) {
	hash, err := o.hash(ctx, k)
	if err != nil {
		return o, err
	}

	head := o.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		eq, err := o.eq(ctx, entry.k, k)
		if err != nil {
			return o, err
		} else if eq {
			entry.v = v
			return o, nil
		}
	}

	o.table[hash] = &hashLargeEntry{k: k, v: v, next: head}
	o.n++

	return o, nil
}

func (o *objectLarge) Append(hash uint64, k, v interface{}) error {
	o.table[hash] = &hashLargeEntry{k: k, v: v, next: o.table[hash]}
	o.n++
	return nil
}

func (o *objectLarge) Get(ctx context.Context, k interface{}) (interface{}, bool, error) {
	hash, err := o.hash(ctx, k)
	if err != nil {
		return nil, false, err
	}

	for entry := o.table[hash]; entry != nil; entry = entry.next {
		if eq, err := o.eq(ctx, entry.k, k); err != nil {
			return nil, false, err
		} else if eq {
			return entry.v, true, nil
		}
	}

	return nil, false, nil
}

func (o *objectLarge) Iter(_ context.Context, iter func(key, value interface{}) (bool, error)) error {
	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			if stop, err := iter(entry.k, entry.v); err != nil {
				return err
			} else if stop {
				return nil
			}
		}
	}

	return nil
}

func (o *objectLarge) Len(context.Context) (int, error) {
	return o.n, nil
}

func (o *objectLarge) Hash(ctx context.Context) (uint64, error) {
	var (
		h uint64
	)
	err := o.Iter(ctx, func(key, value interface{}) (bool, error) {
		var err error
		h, err = ObjectHashEntry(ctx, h, key, value)
		return err != nil, err
	})

	return h, err
}

func (o *objectLarge) hash(ctx context.Context, k interface{}) (uint64, error) {
	x, err := hash(ctx, k)
	return x, err
}

func (o *objectLarge) eq(ctx context.Context, a, b interface{}) (bool, error) {
	return equalOp(ctx, a, b)
}

// objectCompact is the compact implementation.
type objectCompact[T indexable] struct {
	fjson.Json
	table T
	n     int
}

type hashObjectCompactEntry struct {
	hash uint64
	k    T
	v    T
}

func (o *objectCompact[T]) Insert(ctx context.Context, k, v interface{}) (Object, error) {
	if o.n == len(o.table) {
		obj := newObject(o.n + 1)
		for i := 0; i < o.n; i++ {
			if err := obj.Append(o.table[i].hash, o.table[i].k, o.table[i].v); err != nil {
				return o, err
			}
		}

		return obj.Insert(ctx, k, v)
	}

	hash, err := o.hash(ctx, k)
	if err != nil {
		return o, err
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq, err := o.eq(ctx, o.table[i].k, k); err != nil {
				return o, err
			} else if eq {
				o.table[i].v = v
				return o, nil
			}
		}
	}

	o.table[o.n] = hashObjectCompactEntry{hash: hash, k: k, v: v}
	o.n++
	return o, nil
}

func (o *objectCompact[T]) Append(hash uint64, k, v interface{}) error {
	o.table[o.n] = hashObjectCompactEntry{hash: hash, k: k, v: v}
	o.n++
	return nil
}

func (o *objectCompact[T]) Get(ctx context.Context, k interface{}) (interface{}, bool, error) {
	hash, err := o.hash(ctx, k)
	if err != nil {
		return nil, false, err
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq, err := o.eq(ctx, o.table[i].k, k); err != nil {
				return nil, false, err
			} else if eq {
				return o.table[i].v, true, nil
			}
		}
	}

	return nil, false, nil
}

func (o *objectCompact[T]) Iter(_ context.Context, iter func(key, value interface{}) (bool, error)) error {
	var (
		stop bool
		err  error
	)

	for i := 0; i < o.n && !stop && err == nil; i++ {
		stop, err = iter(o.table[i].k, o.table[i].v)
	}

	return err
}

func (o *objectCompact[T]) Len(context.Context) (int, error) {
	return o.n, nil
}

func (o *objectCompact[T]) Hash(ctx context.Context) (uint64, error) {
	var (
		h   uint64
		err error
	)

	for i := 0; i < o.n && err == nil; i++ {
		h, err = ObjectHashEntry(ctx, h, o.table[i].k, o.table[i].v)
	}

	return h, err
}

func (o *objectCompact[T]) hash(ctx context.Context, k interface{}) (uint64, error) {
	x, err := hash(ctx, k)
	return x, err
}

func (o *objectCompact[T]) eq(ctx context.Context, a, b interface{}) (bool, error) {
	return equalOp(ctx, a, b)
}

type indexable interface {
	~[0]hashObjectCompactEntry |
		~[2]hashObjectCompactEntry |
		~[4]hashObjectCompactEntry |
		~[8]hashObjectCompactEntry |
		~[16]hashObjectCompactEntry
}
