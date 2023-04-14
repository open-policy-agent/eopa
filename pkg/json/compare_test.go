package json

import (
	"testing"
)

func testJSONCompare(t *testing.T, a interface{}, b interface{}, expected int) {
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

	testJSONCompare(t, []interface{}{}, []interface{}{"foo"}, -1)
	testJSONCompare(t, []interface{}{}, []interface{}{}, 0)
	testJSONCompare(t, []interface{}{"foo"}, []interface{}{"foo"}, 0)
	testJSONCompare(t, []interface{}{"foo"}, []interface{}{}, 1)

	// Maps (and interfaces as values).

	testJSONCompare(t, map[string]interface{}{}, map[string]interface{}{"key1": "foo"}, -1)
	testJSONCompare(t, map[string]interface{}{"key1": "foo"}, map[string]interface{}{"key1": "foo"}, 0)
	testJSONCompare(t, map[string]interface{}{"key1": "foo"}, map[string]interface{}{}, 1)

	testJSONCompare(t, map[string]interface{}{"key1": "foo", "key2": "bar0"}, map[string]interface{}{"key1": "foo", "key2": "bar1"}, -1)
	testJSONCompare(t, map[string]interface{}{"key1": "foo", "key2": "bar"}, map[string]interface{}{"key1": "foo", "key2": "bar"}, 0)
	testJSONCompare(t, map[string]interface{}{"key1": "foo", "key2": "bar1"}, map[string]interface{}{"key1": "foo", "key2": "bar0"}, 1)

	// Mixed types

	testJSONCompare(t, nil, "foo", -1)
	testJSONCompare(t, "foo", nil, 1)
	testJSONCompare(t, "foo", float64(0), -1)
	testJSONCompare(t, float64(0), "foo", 1)

	// Binary types in arrays and objects

	testJSONCompare(t, []interface{}{NewBlob([]byte("bar"))}, []interface{}{NewBlob([]byte("foo"))}, -1)
	testJSONCompare(t, []interface{}{NewBlob([]byte("foo"))}, []interface{}{NewBlob([]byte("foo"))}, 0)
	testJSONCompare(t, []interface{}{NewBlob([]byte("foo"))}, []interface{}{NewBlob([]byte("bar"))}, 1)

	testJSONCompare(t, map[string]interface{}{"key1": NewBlob([]byte("bar"))}, map[string]interface{}{"key1": NewBlob([]byte("foo"))}, -1)
	testJSONCompare(t, map[string]interface{}{"key1": NewBlob([]byte("foo"))}, map[string]interface{}{"key1": NewBlob([]byte("foo"))}, 0)
	testJSONCompare(t, map[string]interface{}{"key1": NewBlob([]byte("foo"))}, map[string]interface{}{"key1": NewBlob([]byte("bar"))}, 1)
}
