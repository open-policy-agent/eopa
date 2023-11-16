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
				found, ok, err := set.Get(NewString(v))
				if err != nil {
					t.Fatal(err)
				}

				if !ok {
					t.Fatal("value not found")
				}

				if v != found.(*String).Value() {
					t.Fatal("value not found")
				}

				h := xxhash.New()
				if err := hashImpl(NewString(v), h); err != nil {
					t.Fatal(err)
				}
				hash += h.Sum64()
			}

			// Hash
			if h, err := set.Hash(); h != hash || err != nil {
				t.Fatal("unexpected hash")
			}

			// MergeWith
			merged := NewSet(0)
			merged, err := merged.Add(NewString("merge"))
			if err != nil {
				t.Fatal("unexpected add error")
			}

			expected["merge"] = struct{}{}

			merged, err = merged.MergeWith(set)
			if err != nil {
				t.Fatal("unexpected merge error")
			}

			contents = make(map[string]struct{})
			merged.Iter(func(v Json) (bool, error) {
				contents[v.(*String).Value()] = struct{}{}
				return false, nil
			})

			if !reflect.DeepEqual(contents, expected) {
				t.Fatal("unexpected set contents")
			}

			// Add
			set, _ = set.Add(NewString(fmt.Sprintf("%d", i)))
		})
	}
}
