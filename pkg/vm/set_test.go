package vm

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"

	"github.com/cespare/xxhash/v2"
)

func TestSet(t *testing.T) {
	ctx := context.Background()
	set := zeroSet

	for i := 0; i < 32; i++ {
		t.Run("", func(t *testing.T) {
			expected := make(map[string]struct{})
			for j := 0; j < i; j++ {
				expected[fmt.Sprintf("%d", j)] = struct{}{}
			}

			// Iter
			contents := make(map[string]struct{})
			set.Iter(func(v fjson.Json) (bool, error) {
				contents[v.(*fjson.String).Value()] = struct{}{}
				return false, nil
			})
			c := 0
			set.Iter(func(_ fjson.Json) (bool, error) {
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
				found, ok, err := set.Get(ctx, fjson.NewString(v))
				if err != nil {
					t.Fatal(err)
				}

				if !ok {
					t.Fatal("value not found")
				}

				if v != found.(*fjson.String).Value() {
					t.Fatal("value not found")
				}

				h := xxhash.New()
				if err := hashImpl(ctx, fjson.NewString(v), h); err != nil {
					t.Fatal(err)
				}
				hash += h.Sum64()
			}

			// Hash
			if h, err := set.Hash(ctx); h != hash || err != nil {
				t.Fatal("unexpected hash")
			}

			// Add
			set, _ = set.Add(ctx, fjson.NewString(fmt.Sprintf("%d", i)))
		})
	}
}
