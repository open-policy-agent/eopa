package json

import (
	"fmt"
	"reflect"
	"testing"
)

func TestObject2(t *testing.T) {
	obj := zeroObject2

	for i := 0; i < 32; i++ {
		t.Run("", func(t *testing.T) {
			expected := make(map[string]string)
			for j := 0; j < i; j++ {
				expected[fmt.Sprintf("%d", j)] = fmt.Sprintf("%d", j*2)
			}

			// Iter
			contents := make(map[string]string)
			obj.Iter(func(k, v Json) (bool, error) {
				contents[k.(*String).Value()] = v.(*String).Value()
				return false, nil
			})
			c := 0
			obj.Iter(func(_, _ Json) (bool, error) {
				c++
				return true, nil
			})
			if c > 1 {
				t.Fatal("unexpected iteration")
			}

			if !reflect.DeepEqual(contents, expected) {
				t.Fatal("unexpected map contents")
			}

			// Len
			if n := obj.Len(); n != len(expected) {
				t.Fatal("unxpected map length")
			}

			// Get
			var hash uint64
			for k, v := range expected {
				found, ok, err := obj.Get(NewString(k))
				if err != nil {
					t.Fatal(err)
				}

				if !ok {
					t.Fatal("key not found")
				}

				if v != found.(*String).Value() {
					t.Fatal("value not found")
				}

				hash, _ = objectHashEntry(hash, NewString(k), NewString(v))
			}

			// Hash
			if h, err := obj.Hash(); h != hash || err != nil {
				t.Fatal("unexpected hash")
			}

			// Diff
			other, _ := NewObject2(0).Insert(NewString("0"), NewString("0"))

			diff, err := obj.Diff(other)
			if err != nil {
				t.Fatal("unexpected diff")
			}

			delete(expected, "0")
			if n := diff.Len(); n != len(expected) {
				t.Fatalf("unxpected map length: %v %v", n, len(expected))
			}

			// Insert
			obj, _ = obj.Insert(NewString(fmt.Sprintf("%d", i)), NewString(fmt.Sprintf("%d", i*2)))
		})
	}
}
