// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"encoding/json"
	"fmt"

	//"math/big"
	"reflect"
	"testing"
)

func TestJSONPointerEscaping(t *testing.T) {
	// Test the plain escaping of path segments (used by JSON patching).
	if EscapePointerSeg("foo/bar") != "foo~1bar" || UnescapePointerSeg(EscapePointerSeg("foo/bar")) != "foo/bar" {
		t.Errorf("/ not (un)escaped properly in JSON pointer path segment")
	}

	if EscapePointerSeg("foo~bar") != "foo~0bar" || UnescapePointerSeg(EscapePointerSeg("foo~bar")) != "foo~bar" {
		t.Errorf("~ not escaped properly in JSON pointer path segment")
	}

	if EscapePointerSeg("foo~1bar") != "foo~01bar" || UnescapePointerSeg(EscapePointerSeg("foo~1bar")) != "foo~1bar" {
		t.Errorf("~1 not escaped properly in JSON pointer path segment")
	}
}

func TestJSONPointer(t *testing.T) {
	rfc6901 := testBuildJSON(`{"foo": ["bar", "baz"], "": 0, "a/b": 1, "c%d": 2, "e^f": 3, "g|h": 4, "i\\\\j": 5, "k\"l": 6, " ": 7, "m~n": 8, "n": null, "m~m": {"m/m": 9}}`)

	type t3 struct {
		Def string `json:"def"`
	}
	type t2 struct {
		Abc string `json:"abc"`
	}
	type t1 struct {
		Foo string `json:"foo"`
		Bar *int   `json:"bar"`
		T2  t2     `json:"t2"`
		t2
		t3 `json:"t3"`
	}

	i := 1
	s := t1{"x", &i, t2{"z"}, t2{"w"}, t3{"q"}}
	str := "v"
	pstr := &str
	var iface, iface2 any
	iface = str
	iface2 = &str

	tests := []struct {
		doc      any
		pointer  string
		expected any
	}{
		// Test document and paths from RFC 6901.
		{rfc6901, "", rfc6901},
		{rfc6901, "/foo", []any{"bar", "baz"}},
		{rfc6901, "/foo/0", "bar"},
		{rfc6901, "/", float64(0)},
		{rfc6901, "/a~1b", float64(1)},
		{rfc6901, "/c%d", float64(2)},
		{rfc6901, "/e^f", float64(3)},
		{rfc6901, "/g|h", float64(4)},
		{rfc6901, "/i\\\\j", float64(5)},
		{rfc6901, "/k\"l", float64(6)},
		{rfc6901, "/ ", float64(7)},
		{rfc6901, "/m~0n", float64(8)},
		{rfc6901, "/m~0m/m~1m", float64(9)},
		{rfc6901, "/n", nil},
		// Test struct access.
		{s, "", s},
		{s, "/t2", s.T2},
		{s, "/t2/abc", s.T2.Abc},
		{s, "/bar", *s.Bar},
		{s, "/abc", s.Abc},
		{s, "/t3/def", s.Def},
		// Exercise both map field access and more complicated primitive values.
		{map[int]string{1: "v"}, "/1", "v"},
		{map[int]*string{1: &str}, "/1", "v"},
		{map[int]any{1: &str}, "/1", "v"},
		{map[int]any{1: iface}, "/1", "v"},
		{map[int]any{1: iface2}, "/1", "v"},
		{map[int]any{1: &iface}, "/1", "v"},
		{map[int]any{1: &iface2}, "/1", "v"},
		{map[int8]string{1: "v"}, "/1", "v"},
		{map[int16]string{1: "v"}, "/1", "v"},
		{map[int32]string{1: "v"}, "/1", "v"},
		{map[int64]string{1: "v"}, "/1", "v"},
		{map[uint]string{1: "v"}, "/1", "v"},
		{map[uint8]string{1: "v"}, "/1", "v"},
		{map[uint16]string{1: "v"}, "/1", "v"},
		{map[uint32]string{1: "v"}, "/1", "v"},
		{map[uint64]string{1: "v"}, "/1", "v"},
		{map[uint64]string{1: "v"}, "/1", "v"},
		// Test map text marshaller (implemented by big.Int) and integer keys.
		//{map[*big.Int]string{big.NewInt(1): "v"}, "/1", "v"},
		// Test further nesting.
		{map[int][]string{1: {"v"}}, "/1/0", "v"},
		{map[int]*[]string{1: &([]string{"v"})}, "/1/0", "v"},
		{map[int]*[]*string{1: &([]*string{&str})}, "/1/0", "v"},
		{map[int]*[]**string{1: &([]**string{&pstr})}, "/1/0", "v"},
		{map[int]map[string]string{1: {"x": "v"}}, "/1/x", "v"},
	}

	for n, test := range tests {
		t.Run(fmt.Sprintf("%d", n), func(t *testing.T) {
			segs, err := ParsePointer(test.pointer)
			if err != nil {
				t.Errorf("Unexpected pointer error: %s", err)
			} else {
				if test.pointer != NewPointer(segs) {
					t.Errorf("Parsing, constructing did not result in identical ptr: %s", NewPointer(segs))
				}
			}

			// Try extracting from the native type.

			result, err := Extract(test.doc, test.pointer)
			if err != nil {
				t.Errorf("Unexpected pointer error: %s", err)
			}

			if !reflect.DeepEqual(result, test.expected) {
				t.Errorf("Extracted object does not equal to the expected output: %v and %v", result, test.expected)
			}

			// Then try the same with our JSON type.

			jdoc := MustNew(test.doc)
			jexpected := MustNew(test.expected)

			jresult, err := jdoc.Extract(test.pointer)
			if err != nil {
				t.Errorf("Unexpected pointer error: %s, %#v, %v", err, test.doc, jexpected)
			}

			if jresult.Compare(jexpected) != 0 {
				t.Errorf("Extracted object does not equal to the expected output: %v and %v", result, jexpected)
			}
		})
	}
}

func TestJSONPointerNotFound(t *testing.T) {
	v := testBuildJSON(`{"foo": ["bar", "baz"], "a": true, "b": null, "c": 0}`)

	tests := []struct {
		doc      any
		pointer  string
		expected any
	}{
		{v, "/bar", nil},
		{v, "/foo/2", nil},
		{v, "/foo/x", nil},
		{v, "/a/0", nil},
		{v, "/b/0", nil},
		{v, "/c/0", nil},
	}

	for n, test := range tests {
		t.Run(fmt.Sprintf("%d", n), func(t *testing.T) {
			// Try extracting from the native type.

			result, err := Extract(test.doc, test.pointer)
			if err == nil {
				t.Errorf("Unexpected pointer non-failure")
			}

			if err.Error() != "json: path not found" {
				t.Errorf("Unexpected pointer failure type: %v", err)
			}

			if result != nil {
				t.Errorf("Unexpected value")
			}

			// Then try the same with our JSON type.

			jdoc, err := buildJSON(test.doc)
			if err != nil {
				panic("Invalid JSON snippet")
			}

			jresult, err := jdoc.Extract(test.pointer)
			if err == nil {
				t.Errorf("Unexpected pointer non-failure")
			}

			if err.Error() != "json: path not found" {
				t.Errorf("Unexpected pointer failure type: %v", err)
			}

			if jresult != nil {
				t.Errorf("Unexpected value")
			}
		})
	}
}

func testBuildJSON(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic("Invalid JSON snippet")
	}
	return v
}
