package vm

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

func TestObject(t *testing.T) {
	ctx := context.Background()
	obj := zeroObject

	for i := 0; i < 32; i++ {
		t.Run("", func(t *testing.T) {
			expected := make(map[string]string)
			for j := 0; j < i; j++ {
				expected[fmt.Sprintf("%d", j)] = fmt.Sprintf("%d", j*2)
			}

			// Iter
			contents := make(map[string]string)
			obj.Iter(ctx, func(k, v interface{}) (bool, error) {
				contents[k.(*fjson.String).Value()] = v.(*fjson.String).Value()
				return false, nil
			})
			c := 0
			obj.Iter(ctx, func(_, _ interface{}) (bool, error) {
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
			if n, _ := obj.Len(ctx); n != len(expected) {
				t.Fatal("unxpected map length")
			}

			// Get
			var hash uint64
			for k, v := range expected {
				found, ok, err := obj.Get(ctx, fjson.NewString(k))
				if err != nil {
					t.Fatal(err)
				}

				if !ok {
					t.Fatal("key not found")
				}

				if v != found.(*fjson.String).Value() {
					t.Fatal("value not found")
				}

				hash, _ = ObjectHashEntry(ctx, hash, fjson.NewString(k), fjson.NewString(v))
			}

			// Hash
			if h, err := obj.Hash(ctx); h != hash || err != nil {
				t.Fatal("unexpected hash")
			}

			// Insert
			obj, _ = obj.Insert(ctx, fjson.NewString(fmt.Sprintf("%d", i)), fjson.NewString(fmt.Sprintf("%d", i*2)))
		})
	}
}
