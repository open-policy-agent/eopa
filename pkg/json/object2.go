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
	Insert(k, v Json) (Object2, error)
	Append(hash uint64, k, v Json) error
	Get(k Json) (Json, bool, error)
	Iter(iter func(key, value Json) (bool, error)) error
	Iter2(iter func(key, value interface{}) (bool, error)) error
	Equal(other Object2) (bool, error)
	Diff(other Object2) (Object2, error)
	Len() int
	Hash() (uint64, error)
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

func (o *objectLarge) Insert(k, v Json) (Object2, error) {
	hash, err := o.hash(k)
	if err != nil {
		return o, err
	}

	head := o.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		eq, err := o.eq(entry.k, k)
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

func (o *objectLarge) Append(hash uint64, k, v Json) error {
	o.table[hash] = &hashLargeEntry{k: k, v: v, next: o.table[hash]}
	o.n++
	return nil
}

func (o *objectLarge) Get(k Json) (Json, bool, error) {
	hash, err := o.hash(k)
	if err != nil {
		return nil, false, err
	}

	for entry := o.table[hash]; entry != nil; entry = entry.next {
		if eq, err := o.eq(entry.k, k); err != nil {
			return nil, false, err
		} else if eq {
			return entry.v, true, nil
		}
	}

	return nil, false, nil
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

func (o *objectLarge) Equal(other Object2) (bool, error) {
	if o.Len() != other.Len() {
		return false, nil
	}

	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			v, ok, err := other.Get(entry.k)
			if err != nil {
				return false, err
			} else if !ok {
				return false, nil
			}

			if eq, err := equalOp(entry.v, v); err != nil {
				return false, err
			} else if !eq {
				return false, nil
			}
		}
	}

	return true, nil
}

func (o *objectLarge) Diff(other Object2) (Object2, error) {
	n := o.Len()
	if m := other.Len(); m > n {
		n = m
	}

	result := NewObject2(n)

	if err := o.Iter(func(key, value Json) (bool, error) {
		if _, ok, err := other.Get(key); err != nil {
			return true, err
		} else if !ok {
			result, err = result.Insert(key, value)
			if err != nil {
				return false, err
			}
		}

		return false, nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func (o *objectLarge) Len() int {
	return o.n
}

func (o *objectLarge) Hash() (uint64, error) {
	var (
		h uint64
	)
	err := o.Iter(func(key, value Json) (bool, error) {
		var err error
		h, err = objectHashEntry(h, key, value)
		return err != nil, err
	})

	return h, err
}

func (o *objectLarge) AST() ast.Value {
	obj := ast.NewObject()
	o.Iter(func(k, v Json) (bool, error) {
		obj.Insert(ast.NewTerm(k.AST()), ast.NewTerm(v.AST()))
		return false, nil
	})
	return obj
}

func (o *objectLarge) hash(k interface{}) (uint64, error) {
	x, err := hash(k)
	return x, err
}

func (o *objectLarge) eq(a, b Json) (bool, error) {
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

func (o *objectCompact[T]) Insert(k, v Json) (Object2, error) {
	if o.n == len(o.table) {
		obj := NewObject2(o.n + 1)
		for i := 0; i < o.n; i++ {
			if err := obj.Append(o.table[i].hash, o.table[i].k, o.table[i].v); err != nil {
				return o, err
			}
		}

		return obj.Insert(k, v)
	}

	hash, err := o.hash(k)
	if err != nil {
		return o, err
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq, err := o.eq(o.table[i].k, k); err != nil {
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

func (o *objectCompact[T]) Append(hash uint64, k, v Json) error {
	o.table[o.n] = hashObjectCompactEntry{hash: hash, k: k, v: v}
	o.n++
	return nil
}

func (o *objectCompact[T]) Get(k Json) (Json, bool, error) {
	if o.n == 0 {
		return nil, false, nil
	}

	hash, err := o.hash(k)
	if err != nil {
		return nil, false, err
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq, err := o.eq(o.table[i].k, k); err != nil {
				return nil, false, err
			} else if eq {
				return o.table[i].v, true, nil
			}
		}
	}

	return nil, false, nil
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

func (o *objectCompact[T]) Equal(other Object2) (bool, error) {
	if o.Len() != other.Len() {
		return false, nil
	}

	for i := 0; i < o.n; i++ {
		v, ok, err := other.Get(o.table[i].k)
		if err != nil {
			return false, err
		} else if !ok {
			return false, nil
		}

		if eq, err := equalOp(o.table[i].v, v); err != nil {
			return false, err
		} else if !eq {
			return false, nil
		}
	}

	return true, nil
}

func (o *objectCompact[T]) Diff(other Object2) (Object2, error) {
	n := o.Len()
	if m := other.Len(); m > n {
		n = m
	}

	result := NewObject2(n)

	if err := o.Iter(func(key, value Json) (bool, error) {
		if _, ok, err := other.Get(key); err != nil {
			return true, err
		} else if !ok {
			result, err = result.Insert(key, value)
			if err != nil {
				return false, err
			}
		}

		return false, nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func (o *objectCompact[T]) Len() int {
	return o.n
}

func (o *objectCompact[T]) Hash() (uint64, error) {
	var (
		h   uint64
		err error
	)

	for i := 0; i < o.n && err == nil; i++ {
		h, err = objectHashEntry(h, o.table[i].k, o.table[i].v)
	}

	return h, err
}

func (o *objectCompact[T]) AST() ast.Value {
	obj := ast.NewObject()
	o.Iter(func(k, v Json) (bool, error) {
		obj.Insert(ast.NewTerm(k.AST()), ast.NewTerm(v.AST()))
		return false, nil
	})
	return obj
}

func (o *objectCompact[T]) hash(k interface{}) (uint64, error) {
	return hash(k)
}

func (o *objectCompact[T]) eq(a, b Json) (bool, error) {
	return equalOp(a, b)
}

type indexable2 interface {
	~[0]hashObjectCompactEntry |
		~[2]hashObjectCompactEntry |
		~[4]hashObjectCompactEntry |
		~[8]hashObjectCompactEntry |
		~[16]hashObjectCompactEntry
}
