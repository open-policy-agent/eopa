// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"fmt"
	"reflect"
	"testing"
)

func TestObject2(t *testing.T) {
	obj := zeroObject2

	for i := range 32 {
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
				found, ok := obj.Get(NewString(k))
				if !ok {
					t.Fatal("key not found")
				}

				if v != found.(*String).Value() {
					t.Fatal("value not found")
				}

				hash = objectHashEntry(hash, NewString(k), NewString(v))
			}

			// Hash
			if h := obj.Hash(); h != hash {
				t.Fatal("unexpected hash")
			}

			// Equal
			// XXX

			// Diff
			other := NewObject2(0).Insert(NewString("0"), NewString("0"))
			diff := obj.Diff(other)

			delete(expected, "0")
			if n := diff.Len(); n != len(expected) {
				t.Fatalf("unxpected map length: %v %v", n, len(expected))
			}

			// Insert
			obj = obj.Insert(NewString(fmt.Sprintf("%d", i)), NewString(fmt.Sprintf("%d", i*2)))
		})
	}
}
