// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"github.com/open-policy-agent/opa/v1/ast"
)

var (
	_           Json    = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_           Json    = (*objectLarge)(nil)
	_           Object2 = (*objectCompact[[0]hashObjectCompactEntry])(nil)
	_           Object2 = (*objectLarge)(nil)
	zeroObject2 Object2 = &objectCompact[[0]hashObjectCompactEntry]{}
)

type T = any

type Object2 interface {
	Json
	Insert(k, v Json) Object2
	insert(hash uint64, k, v Json) Object2
	// add adds a key-value pair into the object w/o checking its presence.
	add(hash uint64, k, v Json) Object2
	Get(k Json) (Json, bool)
	get(hash uint64, k Json) (Json, bool)
	Iter(iter func(key, value Json) (bool, error)) error
	iter(iter func(hash uint64, key, value Json))
	Iter2(iter func(key, value any) (bool, error)) error
	Equal(other Object2) bool
	Diff(other Object2) Object2
	Len() int
	Hash() uint64
	AST() ast.Value
	Union(other Object2) Object2
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

func (o *objectLarge) insert(hash uint64, k, v Json) Object2 {
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

func (o *objectLarge) add(hash uint64, k, v Json) Object2 {
	o.table[hash] = &hashLargeEntry{k: k, v: v, next: o.table[hash]}
	o.n++
	return o
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

func (o *objectLarge) get(hash uint64, k Json) (Json, bool) {
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

func (o *objectLarge) iter(iter func(hash uint64, key, value Json)) {
	for hash, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			iter(hash, entry.k, entry.v)
		}
	}
}

func (o *objectLarge) Iter2(iter func(key, value any) (bool, error)) error {
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

	for hash, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			v, ok := other.get(hash, entry.k)
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

	for hash, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			if _, ok := other.get(hash, entry.k); !ok {
				result = result.add(hash, entry.k, entry.v)
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
	o.iter(func(_ uint64, k, v Json) {
		obj.Insert(ast.NewTerm(k.AST()), ast.NewTerm(v.AST()))
	})
	return obj
}

func (o *objectLarge) Union(other Object2) Object2 {
	result := other.Diff(o)

	o.iter(func(hash uint64, key, value Json) {
		v2, ok := other.get(hash, key)
		if !ok {
			result = result.add(hash, key, value)
			return
		}

		m := UnionObjects(value, v2)
		result = result.insert(hash, key, m)
	})

	return result
}

func (o *objectLarge) hash(k any) uint64 {
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
			obj = obj.add(o.table[i].hash, o.table[i].k, o.table[i].v)
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

func (o *objectCompact[T]) insert(hash uint64, k, v Json) Object2 {
	if o.n == len(o.table) {
		obj := NewObject2(o.n + 1) // enough space for both appends and insert.
		for i := 0; i < o.n; i++ {
			obj = obj.add(o.table[i].hash, o.table[i].k, o.table[i].v)
		}

		return obj.Insert(k, v)
	}

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

func (o *objectCompact[T]) add(hash uint64, k, v Json) Object2 {
	if o.n == len(o.table) {
		obj := NewObject2(o.n + 1)

		for i := 0; i < o.n; i++ {
			obj = obj.add(o.table[i].hash, o.table[i].k, o.table[i].v)
		}

		return obj.add(hash, k, v)
	}

	o.table[o.n] = hashObjectCompactEntry{hash: hash, k: k, v: v}
	o.n++
	return o
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

func (o *objectCompact[T]) get(hash uint64, k Json) (Json, bool) {
	if o.n == 0 {
		return nil, false
	}

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

func (o *objectCompact[T]) iter(iter func(hash uint64, key, value Json)) {
	for i := 0; i < o.n; i++ {
		iter(o.table[i].hash, o.table[i].k, o.table[i].v)
	}
}

func (o *objectCompact[T]) Iter2(iter func(key, value any) (bool, error)) error {
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
		v, ok := other.get(o.table[i].hash, o.table[i].k)
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
		if _, ok := other.get(o.table[i].hash, o.table[i].k); !ok {
			result = result.add(o.table[i].hash, o.table[i].k, o.table[i].v)
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

func (o *objectCompact[T]) Union(other Object2) Object2 {
	result := other.Diff(o)

	o.iter(func(hash uint64, key, value Json) {
		v2, ok := other.get(hash, key)
		if !ok {
			result = result.add(hash, key, value)
			return
		}

		m := UnionObjects(value, v2)
		result = result.insert(hash, key, m)
	})

	return result
}

func (o *objectCompact[T]) hash(k any) uint64 {
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
