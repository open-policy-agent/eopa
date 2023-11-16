package json

import (
	"github.com/open-policy-agent/opa/ast"
)

var (
	_           Json    = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_           Json    = (*objectLarge)(nil)
	_           Object2 = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_           Object2 = (*objectLarge)(nil)
	zeroObject2 Object2 = &objectCompact[[0]hashObjectCompactEntry]{}
)

type T = interface{}

type Object2 interface {
	Json
	Insert(k, v Json) Object2
	add(hash uint64, k, v Json)
	Get(k Json) (Json, bool)
	Iter(iter func(key, value Json) (bool, error)) error
	Iter2(iter func(key, value interface{}) (bool, error)) error
	Equal(other Object2) bool
	Diff(other Object2) Object2
	Len() int
	Hash() uint64
	AST() ast.Value
}

func NewObject2(n int) Object2 {
	switch {
	case n == 0:
		return zeroObject2
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
	Json
	table map[uint64]*hashLargeEntry
	n     int
}

type hashLargeEntry struct {
	k    Json
	v    Json
	next *hashLargeEntry
}

func newObjectLarge(n int) *objectLarge {
	return &objectLarge{table: make(map[uint64]*hashLargeEntry, n)}
}

func (o *objectLarge) Insert(k, v Json) Object2 {
	hash := o.hash(k)

	head := o.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		if eq := o.eq(entry.k, k); eq {
			entry.v = v
			return o
		}
	}

	o.table[hash] = &hashLargeEntry{k: k, v: v, next: head}
	o.n++

	return o
}

func (o *objectLarge) add(hash uint64, k, v Json) {
	o.table[hash] = &hashLargeEntry{k: k, v: v, next: o.table[hash]}
	o.n++
}

func (o *objectLarge) Get(k Json) (Json, bool) {
	hash := o.hash(k)

	for entry := o.table[hash]; entry != nil; entry = entry.next {
		if eq := o.eq(entry.k, k); eq {
			return entry.v, true
		}
	}

	return nil, false
}

func (o *objectLarge) Iter(iter func(key, value Json) (bool, error)) error {
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

func (o *objectLarge) Iter2(iter func(key, value interface{}) (bool, error)) error {
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

func (o *objectLarge) Equal(other Object2) bool {
	if o.Len() != other.Len() {
		return false
	}

	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			v, ok := other.Get(entry.k)
			if !ok {
				return false
			}

			if eq := equalOp(entry.v, v); !eq {
				return false
			}
		}
	}

	return true
}

func (o *objectLarge) Diff(other Object2) Object2 {
	n := o.Len()
	if m := other.Len(); m > n {
		n = m
	}

	result := NewObject2(n)

	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			if _, ok := other.Get(entry.k); !ok {
				result = result.Insert(entry.k, entry.v)
			}
		}
	}

	return result
}

func (o *objectLarge) Len() int {
	return o.n
}

func (o *objectLarge) Hash() uint64 {
	var (
		h uint64
	)
	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			h = objectHashEntry(h, entry.k, entry.v) // TODO: Reuse the precomputed key hash?
		}
	}

	return h
}

func (o *objectLarge) AST() ast.Value {
	obj := ast.NewObject()
	o.Iter(func(k, v Json) (bool, error) {
		obj.Insert(ast.NewTerm(k.AST()), ast.NewTerm(v.AST()))
		return false, nil
	})
	return obj
}

func (o *objectLarge) hash(k interface{}) uint64 {
	return hash(k)
}

func (o *objectLarge) eq(a, b Json) bool {
	return equalOp(a, b)
}

// objectCompact is the compact implementation.
type objectCompact[T indexable2] struct {
	Json
	table T
	n     int
}

type hashObjectCompactEntry struct {
	hash uint64
	k    Json
	v    Json
}

func (o *objectCompact[T]) Insert(k, v Json) Object2 {
	if o.n == len(o.table) {
		obj := NewObject2(o.n + 1) // enough space for both appends and insert.
		for i := 0; i < o.n; i++ {
			obj.add(o.table[i].hash, o.table[i].k, o.table[i].v)
		}

		return obj.Insert(k, v)
	}

	hash := o.hash(k)

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq := o.eq(o.table[i].k, k); eq {
				o.table[i].v = v
				return o
			}
		}
	}

	o.table[o.n] = hashObjectCompactEntry{hash: hash, k: k, v: v}
	o.n++
	return o
}

// add does not check over space left.
func (o *objectCompact[T]) add(hash uint64, k, v Json) {
	o.table[o.n] = hashObjectCompactEntry{hash: hash, k: k, v: v}
	o.n++
}

func (o *objectCompact[T]) Get(k Json) (Json, bool) {
	if o.n == 0 {
		return nil, false
	}

	hash := o.hash(k)

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq := o.eq(o.table[i].k, k); eq {
				return o.table[i].v, true
			}
		}
	}

	return nil, false
}

func (o *objectCompact[T]) Iter(iter func(key, value Json) (bool, error)) error {
	var (
		stop bool
		err  error
	)

	for i := 0; i < o.n && !stop && err == nil; i++ {
		stop, err = iter(o.table[i].k, o.table[i].v)
	}

	return err
}

func (o *objectCompact[T]) Iter2(iter func(key, value interface{}) (bool, error)) error {
	var (
		stop bool
		err  error
	)

	for i := 0; i < o.n && !stop && err == nil; i++ {
		stop, err = iter(o.table[i].k, o.table[i].v)
	}

	return err
}

func (o *objectCompact[T]) Equal(other Object2) bool {
	if o.Len() != other.Len() {
		return false
	}

	for i := 0; i < o.n; i++ {
		v, ok := other.Get(o.table[i].k)
		if !ok {
			return false
		}

		if eq := equalOp(o.table[i].v, v); !eq {
			return false
		}
	}

	return true
}

func (o *objectCompact[T]) Diff(other Object2) Object2 {
	n := o.Len()
	if m := other.Len(); m > n {
		n = m
	}

	result := NewObject2(n)

	for i := 0; i < o.n; i++ {
		if _, ok := other.Get(o.table[i].k); !ok {
			result = result.Insert(o.table[i].k, o.table[i].v)
		}
	}

	return result
}

func (o *objectCompact[T]) Len() int {
	return o.n
}

func (o *objectCompact[T]) Hash() uint64 {
	var h uint64

	for i := 0; i < o.n; i++ {
		h = objectHashEntry(h, o.table[i].k, o.table[i].v)
	}

	return h
}

func (o *objectCompact[T]) AST() ast.Value {
	obj := ast.NewObject()
	o.Iter(func(k, v Json) (bool, error) {
		obj.Insert(ast.NewTerm(k.AST()), ast.NewTerm(v.AST()))
		return false, nil
	})
	return obj
}

func (o *objectCompact[T]) hash(k interface{}) uint64 {
	return hash(k)
}

func (o *objectCompact[T]) eq(a, b Json) bool {
	return equalOp(a, b)
}

type indexable2 interface {
	~[0]hashObjectCompactEntry |
		~[2]hashObjectCompactEntry |
		~[4]hashObjectCompactEntry |
		~[8]hashObjectCompactEntry |
		~[16]hashObjectCompactEntry
}
