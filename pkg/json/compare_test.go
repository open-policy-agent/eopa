// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"testing"
)

func testJSONCompare(t *testing.T, a any, b any, expected int) {
	x, err1 := buildJSON(a)
	y, err2 := buildJSON(b)
	if err1 != nil || err2 != nil {
		t.Errorf("Unsupported types: %v, %v", a, b)
		return
	}

	result := x.Compare(y)
	if expected != result {
		t.Errorf("Comparison not producing expected result %d but %d with: %v and %v", expected, result, a, b)
	}
}

func TestJSONCompare(t *testing.T) {
	// Elementary types

	testJSONCompare(t, "foo", "goo", -1)
	testJSONCompare(t, "foo", "foo", 0)
	testJSONCompare(t, "goo", "foo", 1)
	testJSONCompare(t, float64(-1), float64(1), -1)
	testJSONCompare(t, float64(-3), float64(-2), -1)
	testJSONCompare(t, float64(1), float64(1), 0)
	testJSONCompare(t, float64(3), float64(2), 1)
	testJSONCompare(t, float64(1), float64(-1), 1)
	testJSONCompare(t, float64(1), float64(-1), 1)
	testJSONCompare(t, true, false, 1)
	testJSONCompare(t, false, false, 0)
	testJSONCompare(t, true, true, 0)
	testJSONCompare(t, false, true, -1)
	testJSONCompare(t, nil, nil, 0)

	// Arrays

	testJSONCompare(t, []any{}, []any{"foo"}, -1)
	testJSONCompare(t, []any{}, []any{}, 0)
	testJSONCompare(t, []any{"foo"}, []any{"foo"}, 0)
	testJSONCompare(t, []any{"foo"}, []any{}, 1)

	// Maps (and interfaces as values).

	testJSONCompare(t, map[string]any{}, map[string]any{"key1": "foo"}, -1)
	testJSONCompare(t, map[string]any{"key1": "foo"}, map[string]any{"key1": "foo"}, 0)
	testJSONCompare(t, map[string]any{"key1": "foo"}, map[string]any{}, 1)

	testJSONCompare(t, map[string]any{"key1": "foo", "key2": "bar0"}, map[string]any{"key1": "foo", "key2": "bar1"}, -1)
	testJSONCompare(t, map[string]any{"key1": "foo", "key2": "bar"}, map[string]any{"key1": "foo", "key2": "bar"}, 0)
	testJSONCompare(t, map[string]any{"key1": "foo", "key2": "bar1"}, map[string]any{"key1": "foo", "key2": "bar0"}, 1)

	// Mixed types

	testJSONCompare(t, nil, "foo", -1)
	testJSONCompare(t, "foo", nil, 1)
	testJSONCompare(t, "foo", float64(0), -1)
	testJSONCompare(t, float64(0), "foo", 1)

	// Binary types in arrays and objects

	testJSONCompare(t, []any{NewBlob([]byte("bar"))}, []any{NewBlob([]byte("foo"))}, -1)
	testJSONCompare(t, []any{NewBlob([]byte("foo"))}, []any{NewBlob([]byte("foo"))}, 0)
	testJSONCompare(t, []any{NewBlob([]byte("foo"))}, []any{NewBlob([]byte("bar"))}, 1)

	testJSONCompare(t, map[string]any{"key1": NewBlob([]byte("bar"))}, map[string]any{"key1": NewBlob([]byte("foo"))}, -1)
	testJSONCompare(t, map[string]any{"key1": NewBlob([]byte("foo"))}, map[string]any{"key1": NewBlob([]byte("foo"))}, 0)
	testJSONCompare(t, map[string]any{"key1": NewBlob([]byte("foo"))}, map[string]any{"key1": NewBlob([]byte("bar"))}, 1)
}
