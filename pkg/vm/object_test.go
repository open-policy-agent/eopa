package vm

import (
	"fmt"
	"reflect"
	"testing"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

func TestObject(t *testing.T) {
	obj := zeroObject

	for i := 0; i < 32; i++ {
		t.Run("", func(t *testing.T) {
			expected := make(map[string]string)
			for j := 0; j < i; j++ {
				expected[fmt.Sprintf("%d", j)] = fmt.Sprintf("%d", j*2)
			}

			// Iter
			contents := make(map[string]string)
			obj.Iter(func(k, v fjson.Json) (bool, error) {
				contents[k.(*fjson.String).Value()] = v.(*fjson.String).Value()
				return false, nil
			})
			c := 0
			obj.Iter(func(_, _ fjson.Json) (bool, error) {
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
				found, ok, err := obj.Get(fjson.NewString(k))
				if err != nil {
					t.Fatal(err)
				}

				if !ok {
					t.Fatal("key not found")
				}

				if v != found.(*fjson.String).Value() {
					t.Fatal("value not found")
				}

				hash, _ = objectHashEntry(hash, fjson.NewString(k), fjson.NewString(v))
			}

			// Hash
			if h, err := obj.Hash(); h != hash || err != nil {
				t.Fatal("unexpected hash")
			}

			// Diff
			other, _ := NewObject().Insert(fjson.NewString("0"), fjson.NewString("0"))

			diff, err := obj.Diff(other)
			if err != nil {
				t.Fatal("unexpected diff")
			}

			delete(expected, "0")
			if n := diff.Len(); n != len(expected) {
				t.Fatalf("unxpected map length: %v %v", n, len(expected))
			}

			// Insert
			obj, _ = obj.Insert(fjson.NewString(fmt.Sprintf("%d", i)), fjson.NewString(fmt.Sprintf("%d", i*2)))
		})
	}
}
