package vm

import (
	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"

	"github.com/open-policy-agent/opa/ast"
)

var (
	_          fjson.Json = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_          fjson.Json = (*objectLarge)(nil)
	_          Object     = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_          Object     = (*objectLarge)(nil)
	zeroObject Object     = &objectCompact[[0]hashObjectCompactEntry]{}
)

type T = interface{}

type Object interface {
	fjson.Json
	Insert(k, v fjson.Json) (Object, error)
	Append(hash uint64, k, v fjson.Json) error
	Get(k fjson.Json) (fjson.Json, bool, error)
	Iter(iter func(key, value fjson.Json) (bool, error)) error
	Iter2(iter func(key, value interface{}) (bool, error)) error
	Equal(other Object) (bool, error)
	Len() int
	Hash() (uint64, error)
	AST() ast.Value
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
	k    fjson.Json
	v    fjson.Json
	next *hashLargeEntry
}

func newObjectLarge(n int) *objectLarge {
	return &objectLarge{table: make(map[uint64]*hashLargeEntry, n)}
}

func (o *objectLarge) Insert(k, v fjson.Json) (Object, error) {
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

func (o *objectLarge) Append(hash uint64, k, v fjson.Json) error {
	o.table[hash] = &hashLargeEntry{k: k, v: v, next: o.table[hash]}
	o.n++
	return nil
}

func (o *objectLarge) Get(k fjson.Json) (fjson.Json, bool, error) {
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

func (o *objectLarge) Iter(iter func(key, value fjson.Json) (bool, error)) error {
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

func (o *objectLarge) Equal(other Object) (bool, error) {
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

func (o *objectLarge) Len() int {
	return o.n
}

func (o *objectLarge) Hash() (uint64, error) {
	var (
		h uint64
	)
	err := o.Iter(func(key, value fjson.Json) (bool, error) {
		var err error
		h, err = objectHashEntry(h, key, value)
		return err != nil, err
	})

	return h, err
}

func (o *objectLarge) AST() ast.Value {
	obj := ast.NewObject()
	o.Iter(func(k, v fjson.Json) (bool, error) {
		obj.Insert(ast.NewTerm(k.AST()), ast.NewTerm(v.AST()))
		return false, nil
	})
	return obj
}

func (o *objectLarge) hash(k interface{}) (uint64, error) {
	x, err := hash(k)
	return x, err
}

func (o *objectLarge) eq(a, b fjson.Json) (bool, error) {
	return equalOp(a, b)
}

// objectCompact is the compact implementation.
type objectCompact[T indexable] struct {
	fjson.Json
	table T
	n     int
}

type hashObjectCompactEntry struct {
	hash uint64
	k    fjson.Json
	v    fjson.Json
}

func (o *objectCompact[T]) Insert(k, v fjson.Json) (Object, error) {
	if o.n == len(o.table) {
		obj := newObject(o.n + 1)
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

func (o *objectCompact[T]) Append(hash uint64, k, v fjson.Json) error {
	o.table[o.n] = hashObjectCompactEntry{hash: hash, k: k, v: v}
	o.n++
	return nil
}

func (o *objectCompact[T]) Get(k fjson.Json) (fjson.Json, bool, error) {
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

func (o *objectCompact[T]) Iter(iter func(key, value fjson.Json) (bool, error)) error {
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

func (o *objectCompact[T]) Equal(other Object) (bool, error) {
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
	o.Iter(func(k, v fjson.Json) (bool, error) {
		obj.Insert(ast.NewTerm(k.AST()), ast.NewTerm(v.AST()))
		return false, nil
	})
	return obj
}

func (o *objectCompact[T]) hash(k interface{}) (uint64, error) {
	return hash(k)
}

func (o *objectCompact[T]) eq(a, b fjson.Json) (bool, error) {
	return equalOp(a, b)
}

type indexable interface {
	~[0]hashObjectCompactEntry |
		~[2]hashObjectCompactEntry |
		~[4]hashObjectCompactEntry |
		~[8]hashObjectCompactEntry |
		~[16]hashObjectCompactEntry
}
