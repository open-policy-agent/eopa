// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"github.com/cespare/xxhash/v2"
	"github.com/open-policy-agent/opa/v1/ast"
	"golang.org/x/exp/slices"
)

var (
	_       Json = (*setCompact[[0]hashSetCompactEntry])(nil)
	_       Json = (*setLarge)(nil)
	_       Set  = (*setCompact[[0]hashSetCompactEntry])(nil)
	_       Set  = (*setLarge)(nil)
	zeroSet Set  = &setCompact[[0]hashSetCompactEntry]{}
)

type Set interface {
	Json
	Add(v Json) Set
	add(hash uint64, v Json) Set
	Get(v Json) (Json, bool)
	Iter(iter func(v Json) (bool, error)) (bool, error)
	Iter2(iter func(v, vv interface{}) (bool, error)) (bool, error)
	Equal(other Set) bool
	Len() int
	Hash() uint64
	AST() ast.Value
	MergeWith(other Set) Set
	mergeTo(other Set) Set
}

func NewSet(n int) Set {
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
	Json
	table map[uint64]*hashSetLargeEntry
	n     int
}

type hashSetLargeEntry struct {
	v    Json
	next *hashSetLargeEntry
}

func newSetLarge(n int) *setLarge {
	return &setLarge{table: make(map[uint64]*hashSetLargeEntry, n)}
}

func (o *setLarge) Add(v Json) Set {
	hash := o.hash(v)
	return o.add(hash, v)
}

func (o *setLarge) add(hash uint64, v Json) Set {
	head := o.table[hash]
	for entry := head; entry != nil; entry = entry.next {
		eq := o.eq(entry.v, v)
		if eq {
			entry.v = v
			return o
		}
	}

	o.table[hash] = &hashSetLargeEntry{v: v, next: head}
	o.n++

	return o
}

func (o *setLarge) MergeWith(other Set) Set {
	return other.mergeTo(o)
}

func (o *setLarge) mergeTo(other Set) Set {
	for hash, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			other = other.add(hash, entry.v)
		}
	}

	return other
}

func (o *setLarge) Get(v Json) (Json, bool) {
	hash := o.hash(v)

	for entry := o.table[hash]; entry != nil; entry = entry.next {
		if eq := o.eq(entry.v, v); eq {
			return entry.v, true
		}
	}

	return nil, false
}

func (o *setLarge) Iter(iter func(v Json) (bool, error)) (bool, error) {
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

func (o *setLarge) Equal(other Set) bool {
	if o == other {
		return true
	}

	if o.Len() != other.Len() {
		return false
	}

	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			if _, match := other.Get(entry.v); !match {
				return false
			}
		}
	}

	return true
}

func (o *setLarge) Hash() uint64 {
	var m uint64

	for _, entry := range o.table {
		for ; entry != nil; entry = entry.next {
			h := xxhash.New()
			hashImpl(entry.v, h)
			m += h.Sum64()
		}
	}

	return m
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

func (o *setLarge) hash(k interface{}) uint64 {
	return hash(k)
}

func (o *setLarge) eq(a, b Json) bool {
	return equalOp(a, b)
}

// setCompact is the compact implementation.
type setCompact[T indexableHashSetTable] struct {
	Json
	table T
	n     int
}

type hashSetCompactEntry struct {
	hash uint64
	v    Json
}

func (o *setCompact[T]) Add(v Json) Set {
	hash := o.hash(v)
	return o.add(hash, v)
}

func (o *setCompact[T]) add(hash uint64, v Json) Set {
	if o.n == len(o.table) {
		set := NewSet(o.n + 1)

		for i := 0; i < o.n; i++ {
			set = set.add(o.table[i].hash, o.table[i].v)
		}

		return set.add(hash, v)
	}

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq := o.eq(o.table[i].v, v); eq {
				return o
			}
		}
	}

	o.table[o.n] = hashSetCompactEntry{hash: hash, v: v}
	o.n++
	return o
}

func (o *setCompact[T]) MergeWith(other Set) Set {
	return other.mergeTo(o)
}

func (o *setCompact[T]) mergeTo(other Set) Set {
	for i := 0; i < o.n; i++ {
		other = other.add(o.table[i].hash, o.table[i].v)
	}
	return other
}

func (o *setCompact[T]) Get(v Json) (Json, bool) {
	if o.n == 0 {
		return nil, false
	}

	hash := o.hash(v)

	for i := 0; i < o.n; i++ {
		if o.table[i].hash == hash {
			if eq := o.eq(o.table[i].v, v); eq {
				return o.table[i].v, true
			}
		}
	}

	return nil, false
}

func (o *setCompact[T]) Iter(iter func(v Json) (bool, error)) (bool, error) {
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

func (o *setCompact[T]) Equal(other Set) bool {
	if o == other {
		return true
	}

	if o.Len() != other.Len() {
		return false
	}

	for i := 0; i < o.n; i++ {
		if _, match := other.Get(o.table[i].v); !match {
			return false
		}
	}

	return true
}

func (o *setCompact[T]) Hash() uint64 {
	var m uint64

	for i := 0; i < o.n; i++ {
		h := xxhash.New()
		hashImpl(o.table[i].v, h)
		m += h.Sum64()
	}

	return m
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

func (o *setCompact[T]) hash(k interface{}) uint64 {
	return hash(k)
}

func (o *setCompact[T]) eq(a, b Json) bool {
	return equalOp(a, b)
}

type indexableHashSetTable interface {
	~[0]hashSetCompactEntry |
		~[2]hashSetCompactEntry |
		~[4]hashSetCompactEntry |
		~[8]hashSetCompactEntry |
		~[16]hashSetCompactEntry
}
