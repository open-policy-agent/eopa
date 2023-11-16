package json

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/cespare/xxhash/v2"
)

func TestSet(t *testing.T) {
	set := zeroSet

	for i := 0; i < 32; i++ {
		t.Run("", func(t *testing.T) {
			expected := make(map[string]struct{})
			for j := 0; j < i; j++ {
				expected[fmt.Sprintf("%d", j)] = struct{}{}
			}

			// Iter
			contents := make(map[string]struct{})
			set.Iter(func(v Json) (bool, error) {
				contents[v.(*String).Value()] = struct{}{}
				return false, nil
			})
			c := 0
			set.Iter(func(_ Json) (bool, error) {
				c++
				return true, nil
			})
			if c > 1 {
				t.Fatal("unexpected iteration")
			}

			if !reflect.DeepEqual(contents, expected) {
				t.Fatal("unexpected set contents")
			}

			// Iter2
			contents = make(map[string]struct{})
			set.Iter2(func(v, vv interface{}) (bool, error) {
				if v.(*String).Value() != vv.(*String).Value() {
					t.Fatal("unexpected iteration")
				}
				contents[v.(*String).Value()] = struct{}{}
				return false, nil
			})
			c = 0
			set.Iter2(func(_, _ interface{}) (bool, error) {
				c++
				return true, nil
			})
			if c > 1 {
				t.Fatal("unexpected iteration")
			}

			if !reflect.DeepEqual(contents, expected) {
				t.Fatal("unexpected set contents")
			}

			// Len
			if n := set.Len(); n != len(expected) {
				t.Fatal("unxpected set length")
			}

			// Get
			var hash uint64
			for v := range expected {
				found, ok := set.Get(NewString(v))
				if !ok {
					t.Fatal("value not found")
				}

				if v != found.(*String).Value() {
					t.Fatal("value not found")
				}

				h := xxhash.New()
				hashImpl(NewString(v), h)
				hash += h.Sum64()
			}

			// Hash
			if h := set.Hash(); h != hash {
				t.Fatal("unexpected hash")
			}

			// Equal
			// XXX

			// MergeWith
			merged := NewSet(0).Add(NewString("merge"))

			expected["merge"] = struct{}{}

			merged = merged.MergeWith(set)

			contents = make(map[string]struct{})
			merged.Iter(func(v Json) (bool, error) {
				contents[v.(*String).Value()] = struct{}{}
				return false, nil
			})

			if !reflect.DeepEqual(contents, expected) {
				t.Fatal("unexpected set contents")
			}

			// Add
			set = set.Add(NewString(fmt.Sprintf("%d", i)))
		})
	}
}
