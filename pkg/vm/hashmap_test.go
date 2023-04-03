// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// nolint: goconst // string duplication is for test readability.
package vm

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	fjson "github.com/styrainc/load-private/pkg/json"
)

func TestHashMapPutDelete(t *testing.T) {
	ctx := context.Background()
	m := stringHashMap()
	m.Put(ctx, str("a"), str("b"))
	m.Put(ctx, str("b"), str("c"))
	if err := m.Delete(ctx, str("b")); err != nil {
		t.Fatal(err)
	}
	r, _, _ := m.Get(ctx, str("a"))
	if *r.(*fjson.String) != *str("b") {
		t.Fatal("Expected a to be intact")
	}
	r, ok, _ := m.Get(ctx, str("b"))
	if ok {
		t.Fatalf("Expected b to be removed: %v", r)
	}
	if err := m.Delete(ctx, str("b")); err != nil {
		t.Fatal(err)
	}
	r, _, _ = m.Get(ctx, str("a"))
	if *r.(*fjson.String) != *str("b") {
		t.Fatal("Expected a to be intact")
	}
}

func TestHashMapOverwrite(t *testing.T) {
	ctx := context.Background()
	m := stringHashMap()
	key := str("hello")
	expected := str("goodbye")
	m.Put(ctx, key, str("world"))
	m.Put(ctx, key, expected)
	result, _, _ := m.Get(ctx, key)
	if result != expected {
		t.Errorf("Expected existing value to be overwritten but got %v for key %v", result, key)
	}
}

func TestHashMapIter(t *testing.T) {
	ctx := context.Background()
	m := NewHashMap()
	keys := []fjson.Json{testHashType{fjson.NewFloat("1"), 1}, testHashType{fjson.NewFloat("2"), 2}, testHashType{fjson.NewFloat("1.4"), 1}}
	value := str("")
	for _, k := range keys {
		m.Put(ctx, k, value)
	}
	// 1 and 1.4 should both hash to 1.
	if len(m.table) != 2 {
		panic(fmt.Sprintf("Expected collision: %v", m))
	}
	results := map[float64]string{}
	m.Iter(func(k, v interface{}) bool {
		f, _ := k.(testHashType).Json.(fjson.Float).Value().Float64()
		results[f] = v.(*fjson.String).Value()
		return false
	})
	expected := map[float64]string{
		float64(1):   value.Value(),
		float64(2):   value.Value(),
		float64(1.4): value.Value(),
	}
	if !reflect.DeepEqual(results, expected) {
		t.Errorf("Expected %v but got %v", expected, results)
	}
}

func TestHashMapCompare(t *testing.T) {
	ctx := context.Background()
	m := stringHashMap()
	n := stringHashMap()
	k1 := str("k1")
	k2 := str("k2")
	k3 := str("k3")
	v1 := str("hello")
	v2 := str("goodbye")

	m.Put(ctx, k1, v1)
	if eq, err := m.Equal(ctx, n); eq || err != nil {
		t.Errorf("Expected hash maps of different size to be non-equal for %v and %v", m, n)
		return
	}
	n.Put(ctx, k1, v1)
	hm, _ := m.Hash(ctx)
	hn, _ := n.Hash(ctx)
	if hm != hn {
		t.Errorf("Expected hashes to equal for %v and %v", m, n)
		return
	}
	if eq, err := m.Equal(ctx, n); !eq || err != nil {
		t.Errorf("Expected hash maps to be equal for %v and %v", m, n)
		return
	}
	m.Put(ctx, k2, v2)
	n.Put(ctx, k3, v2)
	hm, _ = m.Hash(ctx)
	hn, _ = n.Hash(ctx)
	if hm == hn {
		t.Errorf("Did not expect hashes to equal for %v and %v", m, n)
		return
	}
	if eq, err := m.Equal(ctx, n); eq || err != nil {
		t.Errorf("Did not expect hash maps to be equal for %v and %v", m, n)
	}
}

func TestHashMapCopy(t *testing.T) {
	ctx := context.Background()
	m := stringHashMap()

	k1 := str("k1")
	k2 := str("k2")
	v1 := str("hello")
	v2 := str("goodbye")

	m.Put(ctx, k1, v1)
	m.Put(ctx, k2, v2)

	n, _ := m.Copy(ctx)

	if eq, err := n.Equal(ctx, m); !eq || err != nil {
		t.Errorf("Expected hash maps to be equal: %v != %v", n, m)
		return
	}

	m.Put(ctx, k2, str("world"))

	if eq, err := n.Equal(ctx, m); eq || err != nil {
		t.Errorf("Expected hash maps to be non-equal: %v == %v", n, m)
	}
}

func TestHashMapUpdate(t *testing.T) {
	ctx := context.Background()
	m := stringHashMap()
	n := stringHashMap()
	x := stringHashMap()

	k1 := str("k1")
	k2 := str("k2")
	v1 := str("hello")
	v2 := str("goodbye")

	m.Put(ctx, k1, v1)
	n.Put(ctx, k2, v2)
	x.Put(ctx, k1, v1)
	x.Put(ctx, k2, v2)

	o, _ := n.Update(ctx, m)

	if eq, err := x.Equal(ctx, o); !eq || err != nil {
		t.Errorf("Expected update to merge hash maps: %v != %v", x, o)
	}
}

func TestHashMapString(t *testing.T) {
	ctx := context.Background()
	x := stringHashMap()
	x.Put(ctx, str("x"), str("y"))
	str := x.String()
	exp := `{"x": "y"}`
	if exp != str {
		t.Errorf("expected x.String() == {x: y}: %v != %v", exp, str)
	}
}

func stringHashMap() *HashMap {
	return NewHashMap()
}

func str(s string) *fjson.String {
	return fjson.NewString(s)
}

type testHashType struct {
	fjson.Json
	hash uint64
}

func (t testHashType) Hash() uint64 {
	return t.hash
}

func (t testHashType) Equal(other hashable) bool {
	h, ok := other.(testHashType)
	if !ok {
		return false
	}

	return t.Json.Compare(h.Json) == 0
}
