package vm

import (
	"context"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"

	"github.com/cespare/xxhash/v2"
)

var (
	_       fjson.Json = (*setCompact[[0]hashSetCompactEntry])(nil)
	_       fjson.Json = (*setLarge)(nil)
	_       Set        = (*setCompact[[0]hashSetCompactEntry])(nil)
	_       Set        = (*setLarge)(nil)
	zeroSet Set        = &setCompact[[0]hashSetCompactEntry]{}
)

type Set interface {
	fjson.Json
	Add(ctx context.Context, v fjson.Json) (Set, error)
	Append(hash uint64, v fjson.Json) error
	Get(ctx context.Context, v fjson.Json) (fjson.Json, bool, error)
	Iter(iter func(v fjson.Json) (bool, error)) (bool, error)
	Iter2(iter func(v, vv interface{}) (bool, error)) (bool, error)
	Equal(ctx context.Context, other Set) (bool, error)
	Len() int
	Hash(ctx context.Context) (uint64, error)
	MergeWith(ctx context.Context, other Set) (Set, error)
	mergeTo(ctx context.Context, other Set) (Set, error)
}

func NewSet() Set {
	return newSet(0)
}

func newSet(n int) Set {
	switch {
	case n == 0:
		return zeroSet
	case n <= 2:
		return &setCompact[[2]hashSetCompactEntry]{}
	case n <= 4:
		return &setCompact[[4]hashSetCompactEntry]{}
	case n <= 8:
		return &setCompact[[8]hashSetCompactEntry]{}
	case n <= 16:
		return &setCompact[[16]hashSetCompactEntry]{}
	default:
		return newSetLarge(n)
	}
}

// setLarge is the default implementation.
type setLarge struct {
	fjson.Json
	table map[uint64]*hashSetLargeEntry
	n     int
}

type hashSetLargeEntry struct {
	v    fjson.Json
	next *hashSetLargeEntry
}

func newSetLarge(n int) *setLarge {
	return &setLarge{table: make(map[uint64]*hashSetLargeEntry, n)}
}

func (o *setLarge) Add(ctx context.Context, v fjson.Json) (Set, error) {
	hash, err := o.hash(ctx, v)
	if err != nil {
		return o, err
	}

	head := o.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		eq, err := o.eq(ctx, entry.v, v)
		if err != nil {
			return o, err
		} else if eq {
			entry.v = v
			return o, nil
		}
	}

	o.table[hash] = &hashSetLargeEntry{v: v, next: head}
	o.n++

	return o, nil
}

func (o *setLarge) MergeWith(ctx context.Context, other Set) (Set, error) {
	return other.mergeTo(ctx, o)
}

func (o *setLarge) mergeTo(ctx context.Context, other Set) (Set, error) {
	_, err := o.Iter(func(v fjson.Json) (bool, error) {
		var err error
		other, err = other.Add(ctx, v) // TODO: Avoid recomputing the hash.
		return false, err
	})
	return other, err
}

func (o *setLarge) Append(hash uint64, v fjson.Json) error {
	o.table[hash] = &hashSetLargeEntry{v: v, next: o.table[hash]}
	o.n++
	return nil
}

func (o *setLarge) Get(ctx context.Context, v fjson.Json) (fjson.Json, bool, error) {
	hash, err := o.hash(ctx, v)
	if err != nil {
		return nil, false, err
	}

	for entry := o.table[hash]; entry != nil; entry = entry.next {
		if eq, err := o.eq(ctx, entry.v, v); err != nil {
			return nil, false, err
		} else if eq {
			return entry.v, true, nil
		}
	}

	return nil, false, nil
}

func (o *setLarge) Iter(iter func(v fjson.Json) (bool, error)) (bool, error) {
	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			if stop, err := iter(entry.v); err != nil {
				return false, err
			} else if stop {
				return true, nil
			}
		}
	}

	return false, nil
}
func (o *setLarge) Iter2(iter func(v, vv interface{}) (bool, error)) (bool, error) {
	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			if stop, err := iter(entry.v, entry.v); err != nil {
				return false, err
			} else if stop {
				return true, nil
			}
		}
	}

	return false, nil
}

func (o *setLarge) Len() int {
	return o.n
}

func (o *setLarge) Equal(ctx context.Context, other Set) (bool, error) {
	if o == other {
		return true, nil
	}

	if o.Len() != other.Len() {
		return false, nil
	}

	match := true // TODO
	_, err := o.Iter(func(v fjson.Json) (bool, error) {
		var err error
		_, match, err = other.Get(ctx, v)
		if err != nil {
			return true, err
		}
		return !match, err
	})
	if err != nil {
		return false, err
	}

	return match, nil
}

func (o *setLarge) Hash(ctx context.Context) (uint64, error) {
	var (
		m uint64
	)
	_, err := o.Iter(func(v fjson.Json) (bool, error) {
		h := xxhash.New()
		err := hashImpl(ctx, v, h)
		m += h.Sum64()
		return err != nil, err
	})

	return m, err
}

func (o *setLarge) hash(ctx context.Context, k interface{}) (uint64, error) {
	x, err := hash(ctx, k)
	return x, err
}

func (o *setLarge) eq(ctx context.Context, a, b interface{}) (bool, error) {
	return equalOp(ctx, a, b)
}

// setCompact is the compact implementation.
type setCompact[T indexableHashSetTable] struct {
	fjson.Json
	table T
	n     int
}

type hashSetCompactEntry struct {
	hash uint64
	v    fjson.Json
}

func (o *setCompact[T]) Add(ctx context.Context, v fjson.Json) (Set, error) {
	if o.n == len(o.table) {
		set := newSet(o.n + 1)
		for i := 0; i < o.n; i++ {
			if err := set.Append(o.table[i].hash, o.table[i].v); err != nil {
				return o, err
			}
		}

		return set.Add(ctx, v)
	}

	hash, err := o.hash(ctx, v)
	if err != nil {
		return o, err
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq, err := o.eq(ctx, o.table[i].v, v); err != nil {
				return o, err
			} else if eq {
				return o, nil
			}
		}
	}

	o.table[o.n] = hashSetCompactEntry{hash: hash, v: v}
	o.n++
	return o, nil
}

func (o *setCompact[T]) MergeWith(ctx context.Context, other Set) (Set, error) {
	return other.mergeTo(ctx, o)
}

func (o *setCompact[T]) mergeTo(ctx context.Context, other Set) (Set, error) {
	_, err := o.Iter(func(v fjson.Json) (bool, error) {
		var err error
		other, err = other.Add(ctx, v) // TODO: Avoid recomputing the hash.
		return false, err
	})
	return other, err
}

func (o *setCompact[T]) Append(hash uint64, v fjson.Json) error {
	o.table[o.n] = hashSetCompactEntry{hash: hash, v: v}
	o.n++
	return nil
}

func (o *setCompact[T]) Get(ctx context.Context, v fjson.Json) (fjson.Json, bool, error) {
	if o.n == 0 {
		return nil, false, nil
	}

	hash, err := o.hash(ctx, v)
	if err != nil {
		return nil, false, err
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq, err := o.eq(ctx, o.table[i].v, v); err != nil {
				return nil, false, err
			} else if eq {
				return o.table[i].v, true, nil
			}
		}
	}

	return nil, false, nil
}

func (o *setCompact[T]) Iter(iter func(v fjson.Json) (bool, error)) (bool, error) {
	var (
		stop bool
		err  error
	)

	for i := 0; i < o.n && !stop && err == nil; i++ {
		stop, err = iter(o.table[i].v)
	}

	return stop, err
}

func (o *setCompact[T]) Iter2(iter func(v, vv interface{}) (bool, error)) (bool, error) {
	var (
		stop bool
		err  error
	)

	for i := 0; i < o.n && !stop && err == nil; i++ {
		stop, err = iter(o.table[i].v, o.table[i].v)
	}

	return stop, err
}

func (o *setCompact[T]) Len() int {
	return o.n
}

func (o *setCompact[T]) Equal(ctx context.Context, other Set) (bool, error) {
	if o == other {
		return true, nil
	}

	if o.Len() != other.Len() {
		return false, nil
	}

	match := true // TODO
	_, err := o.Iter(func(v fjson.Json) (bool, error) {
		var err error
		_, match, err = other.Get(ctx, v)
		if err != nil {
			return true, err
		}
		return !match, err
	})
	if err != nil {
		return false, err
	}

	return match, nil
}

func (o *setCompact[T]) Hash(ctx context.Context) (uint64, error) {
	var (
		m   uint64
		err error
	)

	for i := 0; i < o.n && err == nil; i++ {
		h := xxhash.New()
		err = hashImpl(ctx, o.table[i].v, h)
		m += h.Sum64()
	}

	return m, err
}

func (o *setCompact[T]) hash(ctx context.Context, k interface{}) (uint64, error) {
	x, err := hash(ctx, k)
	return x, err
}

func (o *setCompact[T]) eq(ctx context.Context, a, b interface{}) (bool, error) {
	return equalOp(ctx, a, b)
}

type indexableHashSetTable interface {
	~[0]hashSetCompactEntry |
		~[2]hashSetCompactEntry |
		~[4]hashSetCompactEntry |
		~[8]hashSetCompactEntry |
		~[16]hashSetCompactEntry
}
