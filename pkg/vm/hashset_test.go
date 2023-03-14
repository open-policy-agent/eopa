package vm

import (
	"fmt"
	"hash/fnv"
	"reflect"
	"testing"

	fjson "github.com/styrainc/load-private/pkg/json"
)

func TestHashSetPutDelete(t *testing.T) {
	m := stringHashSet()
	m.Put(str("a"))
	m.Put(str("b"))
	m.Delete(str("b"))
	if ok := m.Get(str("a")); !ok {
		t.Fatal("Expected a to be intact")
	}
	if ok := m.Get(str("b")); ok {
		t.Fatalf("Expected b to be removed: %v", "b")
	}
	m.Delete(str("b"))
	if ok := m.Get(str("a")); !ok {
		t.Fatal("Expected a to be intact")
	}
}

func TestHashSetOverwrite(t *testing.T) {
	m := stringHashSet()
	key := str("hello")
	m.Put(key)
	m.Put(key)

	if ok := m.Get(key); !ok {
		t.Errorf("Expected existing value to be there: %v", key)
	}
}

func TestHashSetIter(t *testing.T) {
	m := NewHashSet(func(a, b T) bool {
		n1 := a.(fjson.Float)
		n2 := b.(fjson.Float)
		return n1 == n2
	}, func(v fjson.Json) int {
		n := v.(fjson.Float)
		f, _ := n.Value().Float64()
		return int(f)
	})
	keys := []fjson.Float{fjson.NewFloat("1"), fjson.NewFloat("2"), fjson.NewFloat("1.4")}
	for _, k := range keys {
		m.Put(k)
	}
	// 1 and 1.4 should both hash to 1.
	if len(m.table) != 2 {
		panic(fmt.Sprintf("Expected collision: %v", m))
	}
	results := map[float64]struct{}{}
	m.Iter(func(k fjson.Json) bool {
		f, _ := k.(fjson.Float).Value().Float64()
		results[f] = struct{}{}
		return false
	})
	expected := map[float64]struct{}{
		float64(1):   {},
		float64(2):   {},
		float64(1.4): {},
	}
	if !reflect.DeepEqual(results, expected) {
		t.Errorf("Expected %v but got %v", expected, results)
	}
}

func TestHashSetCompare(t *testing.T) {
	m := stringHashSet()
	n := stringHashSet()
	k1 := str("k1")
	k2 := str("k2")
	k3 := str("k3")

	m.Put(k1)
	if m.Equal(n) {
		t.Errorf("Expected hash sets of different size to be non-equal for %v and %v", m, n)
		return
	}
	n.Put(k1)
	if m.Hash() != n.Hash() {
		t.Errorf("Expected hashes to equal for %v and %v", m, n)
		return
	}
	if !m.Equal(n) {
		t.Errorf("Expected hash sets to be equal for %v and %v", m, n)
		return
	}
	m.Put(k2)
	n.Put(k3)
	if m.Hash() == n.Hash() {
		t.Errorf("Did not expect hashes to equal for %v and %v", m, n)
		return
	}
	if m.Equal(n) {
		t.Errorf("Did not expect hash sets to be equal for %v and %v", m, n)
	}
}

func TestHashSetCopy(t *testing.T) {
	m := stringHashSet()

	k1 := str("k1")
	k2 := str("k2")

	m.Put(k1)
	m.Put(k2)

	n := m.Copy()

	if !n.Equal(m) {
		t.Errorf("Expected hash sets to be equal: %v != %v", n, m)
		return
	}
}

func TestHashSetUpdate(t *testing.T) {
	m := stringHashSet()
	n := stringHashSet()
	x := stringHashSet()

	k1 := str("k1")
	k2 := str("k2")

	m.Put(k1)
	n.Put(k2)
	x.Put(k1)
	x.Put(k2)

	o := n.Update(m)

	if !x.Equal(o) {
		t.Errorf("Expected update to merge hash sets: %v != %v", x, o)
	}
}

func TestHashSetString(t *testing.T) {
	x := stringHashSet()
	x.Put(str("x"))
	str := x.String()
	exp := `{"x"}`
	if exp != str {
		t.Errorf("expected x.String() == {x: y}: %v != %v", exp, str)
	}
}

func stringHashSet() *HashSet {
	return NewHashSet(func(a, b fjson.Json) bool {
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
