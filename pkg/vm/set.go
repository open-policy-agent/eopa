package vm

import (
	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"

	"github.com/cespare/xxhash/v2"
	"github.com/open-policy-agent/opa/ast"
	"golang.org/x/exp/slices"
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
	Add(v fjson.Json) (Set, error)
	add(hash uint64, v fjson.Json) (Set, error)
	Get(v fjson.Json) (fjson.Json, bool, error)
	Iter(iter func(v fjson.Json) (bool, error)) (bool, error)
	Iter2(iter func(v, vv interface{}) (bool, error)) (bool, error)
	Equal(other Set) (bool, error)
	Len() int
	Hash() (uint64, error)
	AST() ast.Value
	MergeWith(other Set) (Set, error)
	mergeTo(other Set) (Set, error)
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

func (o *setLarge) Add(v fjson.Json) (Set, error) {
	hash, err := o.hash(v)
	if err != nil {
		return o, err
	}

	return o.add(hash, v)
}

func (o *setLarge) add(hash uint64, v fjson.Json) (Set, error) {
	head := o.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		eq, err := o.eq(entry.v, v)
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

func (o *setLarge) MergeWith(other Set) (Set, error) {
	return other.mergeTo(o)
}

func (o *setLarge) mergeTo(other Set) (Set, error) {
	for hash, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			var err error
			other, err = other.add(hash, entry.v)
			if err != nil {
				return other, err
			}
		}
	}

	return other, nil
}

func (o *setLarge) Get(v fjson.Json) (fjson.Json, bool, error) {
	hash, err := o.hash(v)
	if err != nil {
		return nil, false, err
	}

	for entry := o.table[hash]; entry != nil; entry = entry.next {
		if eq, err := o.eq(entry.v, v); err != nil {
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

func (o *setLarge) Equal(other Set) (bool, error) {
	if o == other {
		return true, nil
	}

	if o.Len() != other.Len() {
		return false, nil
	}

	match := true // TODO
	_, err := o.Iter(func(v fjson.Json) (bool, error) {
		var err error
		_, match, err = other.Get(v)
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

func (o *setLarge) Hash() (uint64, error) {
	var (
		m uint64
	)
	_, err := o.Iter(func(v fjson.Json) (bool, error) {
		h := xxhash.New()
		err := hashImpl(v, h)
		m += h.Sum64()
		return err != nil, err
	})

	return m, err
}

func (o *setLarge) AST() ast.Value {
	// TODO: Sorting is for deterministic tests.
	// We prealloc the Term array and sort it here to trim down the total number of allocs.
	terms := make([]*ast.Term, 0, o.Len())
	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			terms = append(terms, ast.NewTerm(entry.v.AST()))
		}
	}

	slices.SortFunc(terms, func(a, b *ast.Term) int {
		return a.Value.Compare(b.Value)
	})
	return ast.NewSet(terms...)
}

func (o *setLarge) hash(k interface{}) (uint64, error) {
	x, err := hash(k)
	return x, err
}

func (o *setLarge) eq(a, b fjson.Json) (bool, error) {
	return equalOp(a, b)
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

func (o *setCompact[T]) Add(v fjson.Json) (Set, error) {
	hash, err := o.hash(v)
	if err != nil {
		return o, err
	}

	return o.add(hash, v)
}

func (o *setCompact[T]) add(hash uint64, v fjson.Json) (Set, error) {
	if o.n == len(o.table) {
		set := newSet(o.n + 1)

		var err error
		for i := 0; i < o.n && err == nil; i++ {
			set, err = set.add(o.table[i].hash, o.table[i].v)
		}

		if err != nil {
			return set, err
		}

		return set.add(hash, v)
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq, err := o.eq(o.table[i].v, v); err != nil {
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

func (o *setCompact[T]) MergeWith(other Set) (Set, error) {
	return other.mergeTo(o)
}

func (o *setCompact[T]) mergeTo(other Set) (Set, error) {
	var err error
	for i := 0; i < o.n && err == nil; i++ {
		other, err = other.add(o.table[i].hash, o.table[i].v)
	}
	return other, err
}

func (o *setCompact[T]) Get(v fjson.Json) (fjson.Json, bool, error) {
	if o.n == 0 {
		return nil, false, nil
	}

	hash, err := o.hash(v)
	if err != nil {
		return nil, false, err
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq, err := o.eq(o.table[i].v, v); err != nil {
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

func (o *setCompact[T]) Equal(other Set) (bool, error) {
	if o == other {
		return true, nil
	}

	if o.Len() != other.Len() {
		return false, nil
	}

	match := true // TODO
	_, err := o.Iter(func(v fjson.Json) (bool, error) {
		var err error
		_, match, err = other.Get(v)
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

func (o *setCompact[T]) Hash() (uint64, error) {
	var (
		m   uint64
		err error
	)

	for i := 0; i < o.n && err == nil; i++ {
		h := xxhash.New()
		err = hashImpl(o.table[i].v, h)
		m += h.Sum64()
	}

	return m, err
}

func (o *setCompact[T]) AST() ast.Value {
	// TODO: Sorting is for deterministic tests.
	// We prealloc the Term array and sort it here to trim down the total number of allocs.
	terms := make([]*ast.Term, 0, o.Len())
	for i := 0; i < o.n; i++ {
		terms = append(terms, ast.NewTerm(o.table[i].v.AST()))
	}

	slices.SortFunc(terms, func(a, b *ast.Term) int {
		return a.Value.Compare(b.Value)
	})
	return ast.NewSet(terms...)
}

func (o *setCompact[T]) hash(k interface{}) (uint64, error) {
	x, err := hash(k)
	return x, err
}

func (o *setCompact[T]) eq(a, b fjson.Json) (bool, error) {
	return equalOp(a, b)
}

type indexableHashSetTable interface {
	~[0]hashSetCompactEntry |
		~[2]hashSetCompactEntry |
		~[4]hashSetCompactEntry |
		~[8]hashSetCompactEntry |
		~[16]hashSetCompactEntry
}
