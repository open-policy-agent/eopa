package vm

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

func TestHashSetPutDelete(t *testing.T) {
	ctx := context.Background()
	m := stringHashSet()
	m.Put(ctx, str("a"))
	m.Put(ctx, str("b"))
	m.Delete(ctx, str("b"))
	if ok, _ := m.Get(ctx, str("a")); !ok {
		t.Fatal("Expected a to be intact")
	}
	if ok, _ := m.Get(ctx, str("b")); ok {
		t.Fatalf("Expected b to be removed: %v", "b")
	}
	m.Delete(ctx, str("b"))
	if ok, _ := m.Get(ctx, str("a")); !ok {
		t.Fatal("Expected a to be intact")
	}
}

func TestHashSetOverwrite(t *testing.T) {
	ctx := context.Background()
	m := stringHashSet()
	key := str("hello")
	m.Put(ctx, key)
	m.Put(ctx, key)

	if ok, _ := m.Get(ctx, key); !ok {
		t.Errorf("Expected existing value to be there: %v", key)
	}
}

func TestHashSetIter(t *testing.T) {
	ctx := context.Background()
	m := NewHashSet()
	keys := []fjson.Json{testHashType{fjson.NewFloat("1"), 1}, testHashType{fjson.NewFloat("2"), 2}, testHashType{fjson.NewFloat("1.4"), 1}}
	for _, k := range keys {
		m.Put(ctx, k)
	}
	// 1 and 1.4 should both hash to 1.
	if len(m.table) != 2 {
		panic(fmt.Sprintf("Expected collision: %v", m))
	}
	results := map[float64]struct{}{}
	m.Iter(func(k interface{}) (bool, error) {
		f, _ := k.(testHashType).Json.(fjson.Float).Value().Float64()
		results[f] = struct{}{}
		return false, nil
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
	ctx := context.Background()
	m := stringHashSet()
	n := stringHashSet()
	k1 := str("k1")
	k2 := str("k2")
	k3 := str("k3")

	m.Put(ctx, k1)
	if ok, _ := m.Equal(ctx, n); ok {
		t.Errorf("Expected hash sets of different size to be non-equal for %v and %v", m, n)
		return
	}
	n.Put(ctx, k1)
	hm, _ := m.Hash(ctx)
	hn, _ := n.Hash(ctx)
	if hm != hn {
		t.Errorf("Expected hashes to equal for %v and %v", m, n)
		return
	}
	if ok, _ := m.Equal(ctx, n); !ok {
		t.Errorf("Expected hash sets to be equal for %v and %v", m, n)
		return
	}
	m.Put(ctx, k2)
	n.Put(ctx, k3)

	hm, _ = m.Hash(ctx)
	hn, _ = n.Hash(ctx)
	if hm == hn {
		t.Errorf("Did not expect hashes to equal for %v and %v", m, n)
		return
	}
	if ok, _ := m.Equal(ctx, n); ok {
		t.Errorf("Did not expect hash sets to be equal for %v and %v", m, n)
	}
}

func TestHashSetCopy(t *testing.T) {
	ctx := context.Background()
	m := stringHashSet()

	k1 := str("k1")
	k2 := str("k2")

	m.Put(ctx, k1)
	m.Put(ctx, k2)

	n, _ := m.Copy(ctx)

	if ok, _ := n.Equal(ctx, m); !ok {
		t.Errorf("Expected hash sets to be equal: %v != %v", n, m)
		return
	}
}

func TestHashSetUpdate(t *testing.T) {
	ctx := context.Background()
	m := stringHashSet()
	n := stringHashSet()
	x := stringHashSet()

	k1 := str("k1")
	k2 := str("k2")

	m.Put(ctx, k1)
	n.Put(ctx, k2)
	x.Put(ctx, k1)
	x.Put(ctx, k2)

	o, _ := n.Update(ctx, m)

	if ok, _ := x.Equal(ctx, o); !ok {
		t.Errorf("Expected update to merge hash sets: %v != %v", x, o)
	}
}

func TestHashSetString(t *testing.T) {
	ctx := context.Background()
	x := stringHashSet()
	x.Put(ctx, str("x"))
	str := x.String()
	exp := `{"x"}`
	if exp != str {
		t.Errorf("expected x.String() == {x: y}: %v != %v", exp, str)
	}
}

func stringHashSet() *HashSet {
	return NewHashSet()
}
