// Copyright 2016 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// nolint: goconst // string duplication is for test readability.
package vm

import (
	"fmt"
	"hash/fnv"
	"reflect"
	"testing"

	fjson "github.com/styrainc/load-private/pkg/json"
)

func TestHashMapPutDelete(t *testing.T) {
	m := stringHashMap()
	m.Put(str("a"), str("b"))
	m.Put(str("b"), str("c"))
	m.Delete(str("b"))
	r, _ := m.Get(str("a"))
	if r != str("b") {
		t.Fatal("Expected a to be intact")
	}
	r, ok := m.Get(str("b"))
	if ok {
		t.Fatalf("Expected b to be removed: %v", r)
	}
	m.Delete(str("b"))
	r, _ = m.Get(str("a"))
	if r != str("b") {
		t.Fatal("Expected a to be intact")
	}
}

func TestHashMapOverwrite(t *testing.T) {
	m := stringHashMap()
	key := str("hello")
	expected := str("goodbye")
	m.Put(key, str("world"))
	m.Put(key, expected)
	result, _ := m.Get(key)
	if result != expected {
		t.Errorf("Expected existing value to be overwritten but got %v for key %v", result, key)
	}
}

func TestHashMapIter(t *testing.T) {
	m := NewHashMap(func(a, b T) bool {
		n1 := a.(fjson.Float)
		n2 := b.(fjson.Float)
		return n1 == n2
	}, func(v fjson.Json) int {
		n := v.(fjson.Float)
		f, _ := n.Value().Float64()
		return int(f)
	})
	keys := []fjson.Float{fjson.NewFloat("1"), fjson.NewFloat("2"), fjson.NewFloat("1.4")}
	value := str("")
	for _, k := range keys {
		m.Put(k, value)
	}
	// 1 and 1.4 should both hash to 1.
	if len(m.table) != 2 {
		panic(fmt.Sprintf("Expected collision: %v", m))
	}
	results := map[float64]string{}
	m.Iter(func(k fjson.Json, v fjson.Json) bool {
		f, _ := k.(fjson.Float).Value().Float64()
		results[f] = v.(fjson.String).Value()
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
	m := stringHashMap()
	n := stringHashMap()
	k1 := str("k1")
	k2 := str("k2")
	k3 := str("k3")
	v1 := str("hello")
	v2 := str("goodbye")

	m.Put(k1, v1)
	if m.Equal(n) {
		t.Errorf("Expected hash maps of different size to be non-equal for %v and %v", m, n)
		return
	}
	n.Put(k1, v1)
	if m.Hash() != n.Hash() {
		t.Errorf("Expected hashes to equal for %v and %v", m, n)
		return
	}
	if !m.Equal(n) {
		t.Errorf("Expected hash maps to be equal for %v and %v", m, n)
		return
	}
	m.Put(k2, v2)
	n.Put(k3, v2)
	if m.Hash() == n.Hash() {
		t.Errorf("Did not expect hashes to equal for %v and %v", m, n)
		return
	}
	if m.Equal(n) {
		t.Errorf("Did not expect hash maps to be equal for %v and %v", m, n)
	}
}

func TestHashMapCopy(t *testing.T) {
	m := stringHashMap()

	k1 := str("k1")
	k2 := str("k2")
	v1 := str("hello")
	v2 := str("goodbye")

	m.Put(k1, v1)
	m.Put(k2, v2)

	n := m.Copy()

	if !n.Equal(m) {
		t.Errorf("Expected hash maps to be equal: %v != %v", n, m)
		return
	}

	m.Put(k2, str("world"))

	if n.Equal(m) {
		t.Errorf("Expected hash maps to be non-equal: %v == %v", n, m)
	}
}

func TestHashMapUpdate(t *testing.T) {
	m := stringHashMap()
	n := stringHashMap()
	x := stringHashMap()

	k1 := str("k1")
	k2 := str("k2")
	v1 := str("hello")
	v2 := str("goodbye")

	m.Put(k1, v1)
	n.Put(k2, v2)
	x.Put(k1, v1)
	x.Put(k2, v2)

	o := n.Update(m)

	if !x.Equal(o) {
		t.Errorf("Expected update to merge hash maps: %v != %v", x, o)
	}
}

func TestHashMapString(t *testing.T) {
	x := stringHashMap()
	x.Put(str("x"), str("y"))
	str := x.String()
	exp := `{"x": "y"}`
	if exp != str {
		t.Errorf("expected x.String() == {x: y}: %v != %v", exp, str)
	}
}

func stringHashMap() *HashMap {
	return NewHashMap(func(a, b fjson.Json) bool {
		s1 := a.(fjson.String)
		s2 := b.(fjson.String)
		return s1 == s2
	}, func(v fjson.Json) int {
		s := v.(fjson.String)
		h := fnv.New64a()
		h.Write([]byte(s))
		return int(h.Sum64())
	})
}

func str(s string) fjson.String {
	return fjson.NewString(s)
}
