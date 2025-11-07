// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/open-policy-agent/eopa/pkg/json/utils"
)

type testResource struct {
	V any
}

func (t *testResource) Meta(now time.Time) (string, string) {
	return "timestamp", fmt.Sprintf("%d", now.UnixNano())
}

type (
	testCollection map[string]testResource
	testOperation  func(c Collections) Collections
)

func testWriteBlob(path string, v []byte) testOperation {
	return func(c Collections) Collections {
		c.WriteBlob(path, NewBlob(v))
		return c
	}
}

func testWriteJSON(path string, v any) testOperation {
	return func(c Collections) Collections {
		c.WriteJSON(path, MustNew(v))
		return c
	}
}

func testPatchJSON(path string, patch JsonPatchSpec) testOperation {
	return func(c Collections) Collections {
		p, err := NewPatch(patch)
		if err != nil {
			panic(err)
		}

		ok, err := c.PatchJSON(path, p)
		if err != nil {
			panic(err)
		}

		if !ok {
			panic("invalid patch")
		}

		return c
	}
}

func testWriteDirectory(path string) testOperation {
	return func(c Collections) Collections {
		c.WriteDirectory(path)
		return c
	}
}

func testWriteTimestamp(path string, now time.Time) testOperation {
	return func(c Collections) Collections {
		if !c.WriteMeta(path, "timestamp", strconv.FormatInt(now.UnixNano(), 10)) {
			panic("unable to write meta")
		}
		return c
	}
}

func testRemove(path string) testOperation {
	return func(c Collections) Collections {
		if !c.Remove(path) {
			panic("invalid path: " + path)
		}
		return c
	}
}

func testNonexistingRemove(path string) testOperation {
	return func(c Collections) Collections {
		if c.Remove(path) {
			panic("invalid path")
		}
		return c
	}
}

// testCollectionCreate creates a collection based on the spec.
func testCollectionCreate(files map[string]testResource, now time.Time) Collections {
	w := NewCollections().(*writableSnapshot)

	for name, resource := range files {
		if resource.V == nil {
			w.WriteDirectory(name)
			continue
		}

		if v, ok := resource.V.([]byte); ok {
			w.WriteBlob(name, NewBlob(v))
		} else {
			w.WriteJSON(name, MustNew(resource.V))
		}
	}

	return w.Prepare(now)
}

func testCollectionOperation(c Collections, operations []testOperation, _ time.Time) Collections {
	for _, op := range operations {
		c = op(c)
	}

	return c
}

// testCollectionVerify the resulting collection is as as expected.
func testCollectionVerify(t *testing.T, c Collections, expected map[string]testResource, now time.Time) {
	files := make(map[string]testResource)
	maps.Copy(files, expected)

	f := func(r Resource) bool {
		hit, ok := files[r.Name()]
		if !ok {
			t.Errorf("file '%s' not found", r.Name())
			return true
		}

		delete(files, r.Name())

		if hit.V == nil {
			if r.Kind() != Directory {
				t.Errorf("file %s is not a directory %v", r.Name(), r.Kind())
			}
		} else {
			if !reflect.DeepEqual(hit.V, r.File().Contents()) {
				t.Errorf("file %s doesn't match: %v %T", r.Name(), r.File().Contents(), r.File().Contents())
			}
		}

		key, value := hit.Meta(now)
		if v, ok := r.Meta(key); !ok || v != value {
			t.Fatalf("file '%s' meta doesn't match, key: %v '%v' (%t), %v", r.Name(), key, v, ok, r.(*resourceImpl).obj.Names())
		}

		// Check the resource has valid keys: "data" or "data:<name>" and "kind" for non-directories.

		names := r.(*resourceImpl).obj.Names()
		if slices.Contains(names, "data") {
			for _, name := range names {
				if strings.HasPrefix(name, "data:") {
					t.Errorf("data and data: both defined")
				}
			}

			if !slices.Contains(names, "kind") {
				t.Errorf("data without kind")
			}
		}

		return true
	}

	// Check the collection right after patching, still backed by a delta patch type.

	c.Walk(f)
	if len(files) > 0 {
		t.Errorf("extra files around: %v", files)
	}

	// Then reconstruct the collection from bytes and re-run the check.

	source := c.(*snapshot).content.(*deltaPatchObjectReader).snapshot
	s := make([]byte, source.Len())
	if _, err := source.ReadAt(s, 0); err != nil {
		panic(err)
	}

	source = c.DeltaReader()
	d := make([]byte, source.Len())
	if _, err := source.ReadAt(d, 0); err != nil {
		panic(err)
	}

	c, err := NewCollectionsFromReaders(
		utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(s)), int64(len(s)),
		utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(d)), int64(len(d)), nil, nil,
	)
	if err != nil {
		panic(err)
	}

	files = expected
	c.Walk(f)
}

func TestDeltaPatchSnapshot(t *testing.T) {
	now := time.Now().UTC()
	testTime = now

	type test struct {
		Description    string
		PreCollection  map[string]testResource
		Operations     []testOperation
		PostCollection map[string]testResource
	}

	for _, test := range []test{
		{
			Description:    "Write JSON root (overwrite)",
			PreCollection:  testCollection{"": testResource{V: "foo"}},
			Operations:     []testOperation{testWriteJSON("", "bar")},
			PostCollection: testCollection{"": testResource{V: "bar"}},
		},
		{
			Description:    "Write JSON root (type change)",
			PreCollection:  testCollection{"": testResource{V: []byte("foo")}},
			Operations:     []testOperation{testWriteJSON("", "bar")},
			PostCollection: testCollection{"": testResource{V: "bar"}},
		},
		{
			Description:   "Write JSON non-root (create, nesting)",
			PreCollection: testCollection{},
			Operations:    []testOperation{testWriteJSON("a", "bar")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: "bar"},
			},
		},
		{
			Description:   "Write JSON non-root (overwrite, nesting)",
			PreCollection: testCollection{"a": testResource{V: "foo"}},
			Operations:    []testOperation{testWriteJSON("a", "bar")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: "bar"},
			},
		},
		{
			Description:   "Write JSON non-root (overwrite, directory)",
			PreCollection: testCollection{"a/b": testResource{V: "foo"}},
			Operations:    []testOperation{testWriteJSON("a", "bar")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: "bar"},
			},
		},
		{
			Description:   "Write JSON non-root (type change, nesting)",
			PreCollection: testCollection{"a": testResource{V: []byte("foo")}},
			Operations:    []testOperation{testWriteJSON("a", "bar")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: "bar"},
			},
		},
		{
			Description:   "Write JSON non-root (overwrite leaf, deeper nesting)",
			PreCollection: testCollection{"a/b": testResource{V: "foo"}},
			Operations:    []testOperation{testWriteJSON("a/b", "bar")},
			PostCollection: testCollection{
				"":    testResource{},
				"a":   testResource{},
				"a/b": testResource{V: "bar"},
			},
		},
		{
			Description:   "Write JSON non-root (overwrite middle, deeper nesting)",
			PreCollection: testCollection{"a": testResource{V: "foo"}},
			Operations:    []testOperation{testWriteJSON("a/b", "bar")},
			PostCollection: testCollection{
				"":    testResource{},
				"a":   testResource{},
				"a/b": testResource{V: "bar"},
			},
		},
		{
			Description:   "Write JSON non-root (type change, deeper nesting)",
			PreCollection: testCollection{"a/b": testResource{V: []byte("foo")}},
			Operations:    []testOperation{testWriteJSON("a/b", "bar")},
			PostCollection: testCollection{
				"":    testResource{},
				"a":   testResource{},
				"a/b": testResource{V: "bar"},
			},
		},
		{
			Description:   "Patch JSON root",
			PreCollection: testCollection{"a": testResource{V: map[string]any{"foo": "abc"}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]any{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]any{"foo": "def"}},
			},
		},
		{
			Description:   "Patch JSON non-root (add)",
			PreCollection: testCollection{"a": testResource{V: map[string]any{"foo": "abc"}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]any{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]any{"foo": "def"}},
			},
		},
		{
			Description:   "Patch JSON non-root (create 1)",
			PreCollection: testCollection{"a": testResource{V: map[string]any{}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]any{
					"op":    "create",
					"path":  "/a",
					"value": "value",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]any{"a": "value"}},
			},
		},
		{
			Description:   "Patch JSON non-root (create 2)",
			PreCollection: testCollection{"a": testResource{V: map[string]any{}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]any{
					"op":    "create",
					"path":  "/a/b",
					"value": "value",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]any{"a": map[string]any{"b": "value"}}},
			},
		},
		{
			Description:   "Patch JSON non-root (create 3)",
			PreCollection: testCollection{"a": testResource{V: map[string]any{}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]any{
					"op":    "create",
					"path":  "/a/b/c",
					"value": "value",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]any{"a": map[string]any{"b": map[string]any{"c": "value"}}}},
			},
		},
		{
			Description: "Patch JSON non-root (two non-overlapping patches)",
			PreCollection: testCollection{"a": testResource{V: map[string]any{
				"foo": "abc",
				"bar": "def",
			}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]any{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
				map[string]any{
					"op":    "add",
					"path":  "/bar",
					"value": "ghi",
				},
			})},
			PostCollection: testCollection{
				"": testResource{},
				"a": testResource{V: map[string]any{
					"foo": "def",
					"bar": "ghi",
				}},
			},
		},
		{
			Description: "Patch JSON non-root (two overlapping patches #1)",
			PreCollection: testCollection{"a": testResource{V: map[string]any{
				"foo": "abc",
				"bar": "def",
			}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]any{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
				map[string]any{
					"op":   "add",
					"path": "",
					"value": map[string]any{
						"foo": "bar",
					},
				},
			})},
			PostCollection: testCollection{
				"": testResource{},
				"a": testResource{V: map[string]any{
					"foo": "bar",
				}},
			},
		},
		{
			Description: "Patch JSON non-root (two overlapping patches #2)",
			PreCollection: testCollection{"a": testResource{V: map[string]any{
				"foo": "abc",
				"bar": "def",
			}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]any{
					"op":   "add",
					"path": "",
					"value": map[string]any{
						"foo": "bar",
					},
				},
				map[string]any{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
			})},
			PostCollection: testCollection{
				"": testResource{},
				"a": testResource{V: map[string]any{
					"foo": "def",
				}},
			},
		},
		{
			Description: "Patch JSON root (nested patches #1)",
			PreCollection: testCollection{"": testResource{V: map[string]any{
				"foo": map[string]any{
					"nested": "abc",
				},
				"bar": "def",
			}}},
			Operations: []testOperation{testPatchJSON("", JsonPatchSpec{
				map[string]any{
					"op":    "add",
					"path":  "/new",
					"value": "value",
				},
				map[string]any{
					"op":    "add",
					"path":  "/foo/nested",
					"value": "patched", // This value will be incorrectly included, unless patches are recursively removed.
				},
				map[string]any{
					"op":   "add",
					"path": "",
					"value": map[string]any{
						"foo": map[string]any{
							"nested": "abc",
						},

						"bar": "patched",
					},
				},
			})},
			PostCollection: testCollection{
				"": testResource{V: map[string]any{
					"foo": map[string]any{
						"nested": "abc",
					},
					"bar": "patched",
				}},
			},
		},
		{
			Description: "Patch JSON root (nested patches #2)",
			PreCollection: testCollection{"": testResource{V: map[string]any{
				"abc": "123",
			}}},
			Operations: []testOperation{testPatchJSON("", JsonPatchSpec{
				map[string]any{
					"op":    "add",
					"path":  "/abc",
					"value": "456",
				},
				map[string]any{ // This operation should not remove the previous patch, even though it'll modify the root.
					"op":    "add",
					"path":  "/def",
					"value": "789",
				},
			})},
			PostCollection: testCollection{
				"": testResource{V: map[string]any{
					"abc": "456",
					"def": "789",
				}},
			},
		},
		{
			Description: "Patch JSON root (embedded types patched)",
			PreCollection: testCollection{"": testResource{V: map[string]any{
				"abc": nil, // Embedded types (nil, booleans), have no offsets to remove.
			}}},
			Operations: []testOperation{testPatchJSON("", JsonPatchSpec{
				map[string]any{
					"op":   "add",
					"path": "/abc",
					"value": map[string]any{
						"def": true,
						"ghi": false,
					},
				},
				map[string]any{
					"op":    "add",
					"path":  "",
					"value": map[string]any{},
				},
			})},
			PostCollection: testCollection{
				"": testResource{V: map[string]any{}},
			},
		},
		{
			Description:    "Write binary root (overwrite)",
			PreCollection:  testCollection{"": testResource{V: []byte("foo")}},
			Operations:     []testOperation{testWriteBlob("", []byte("bar"))},
			PostCollection: testCollection{"": testResource{V: []byte("bar")}},
		},
		{
			Description:    "Write binary root (type change)",
			PreCollection:  testCollection{"": testResource{V: "foo"}},
			Operations:     []testOperation{testWriteBlob("", []byte("bar"))},
			PostCollection: testCollection{"": testResource{V: []byte("bar")}},
		},
		{
			Description:   "Write binary root (create, nesting)",
			PreCollection: testCollection{},
			Operations:    []testOperation{testWriteBlob("a", []byte("bar"))},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: []byte("bar")},
			},
		},
		{
			Description:   "Write binary root (overwrite, nesting)",
			PreCollection: testCollection{"a": testResource{V: []byte("foo")}},
			Operations:    []testOperation{testWriteBlob("a", []byte("bar"))},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: []byte("bar")},
			},
		},
		{
			Description:   "Write binary root (overwrite, directory)",
			PreCollection: testCollection{"a/b": testResource{V: []byte("foo")}},
			Operations:    []testOperation{testWriteBlob("a", []byte("bar"))},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: []byte("bar")},
			},
		},
		{
			Description:   "Write binary root (type change, nesting)",
			PreCollection: testCollection{"a": testResource{V: "foo"}},
			Operations:    []testOperation{testWriteBlob("a", []byte("bar"))},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: []byte("bar")},
			},
		},
		{
			Description:   "Write directory (create)",
			PreCollection: testCollection{},
			Operations:    []testOperation{testWriteDirectory("a")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{},
			},
		},
		{
			Description:   "Write directory (existing)",
			PreCollection: testCollection{"a": testResource{}},
			Operations:    []testOperation{testWriteDirectory("a")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{},
			},
		},
		{
			Description:   "Write directory (type change)",
			PreCollection: testCollection{"a": testResource{V: "foo"}},
			Operations:    []testOperation{testWriteDirectory("a")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{},
			},
		},
		{
			Description:   "Write directory (create, nested)",
			PreCollection: testCollection{},
			Operations:    []testOperation{testWriteDirectory("a/b")},
			PostCollection: testCollection{
				"":    testResource{},
				"a":   testResource{},
				"a/b": testResource{},
			},
		},
		{
			Description:   "Remove directory",
			PreCollection: testCollection{"a": testResource{}},
			Operations:    []testOperation{testRemove("a")},
			PostCollection: testCollection{
				"": testResource{},
			},
		},
		{
			Description: "Remove file",
			PreCollection: testCollection{
				"a": testResource{V: "foo"},
				"b": testResource{V: "bar"},
			},
			Operations: []testOperation{testRemove("a")},
			PostCollection: testCollection{
				"":  testResource{},
				"b": testResource{V: "bar"},
			},
		},
		{
			Description: "Remove file (nonexisting)",
			PreCollection: testCollection{
				"a": testResource{V: "foo"},
				"b": testResource{V: "bar"},
			},
			Operations: []testOperation{testNonexistingRemove("c")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: "foo"},
				"b": testResource{V: "bar"},
			},
		},
		{
			Description:   "Remove, write JSON non-root (deeper nesting)",
			PreCollection: testCollection{"a/a": testResource{V: "foo"}},
			Operations:    []testOperation{testRemove("a/a"), testWriteJSON("a", "bar")},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: "bar"},
			},
		},
		{
			Description:   "Remove, write JSON non-root (deeper nesting)",
			PreCollection: testCollection{"a": testResource{V: "foo"}},
			Operations:    []testOperation{testWriteTimestamp("", now), testRemove("a"), testWriteDirectory("a"), testWriteTimestamp("", now), testWriteTimestamp("a", now), testWriteJSON("a/a", "foo")},
			PostCollection: testCollection{
				"":    testResource{},
				"a":   testResource{},
				"a/a": testResource{V: "foo"},
			},
		},
	} {
		t.Run(test.Description, func(t *testing.T) {
			c := testCollectionCreate(test.PreCollection, now)
			c = testCollectionOperation(c, test.Operations, now)
			testCollectionVerify(t, c, test.PostCollection, now)
		})
	}
}

// BenchmarkDeltaPatchWrite benchmarks patching a large delta snapshot with a small patch.
func BenchmarkDeltaPatchWrite(b *testing.B) {
	obj := make(map[string]any)
	for i := range 10000 {
		obj[fmt.Sprintf("key:%d", i)] = fmt.Sprintf("value:%d", i)
	}

	now := time.Now()
	snapshot := testCollectionCreate(testCollection{"": testResource{V: obj}}, now)
	delta := testPatchJSON("", JsonPatchSpec{
		map[string]any{
			"op":    "add",
			"path":  "/key:0",
			"value": "patched",
		},
	})(snapshot)

	for b.Loop() {
		testPatchJSON("", JsonPatchSpec{
			map[string]any{
				"op":    "add",
				"path":  "/key:1",
				"value": "patched",
			},
		})(delta)
	}
}

// TestPatchToDelta demonstrates building a delta from a snapshot and JSON patch,
// then tests accessing the snapshot+delta to verify the changes were applied correctly.
func TestPatchToDelta(t *testing.T) {
	// Test data as JSON strings
	original := `{"name": "Alice", "age": 25}`

	patchJSON := `[
		{"op": "replace", "path": "/name", "value": "Bob"},
		{"op": "add", "path": "/city", "value": "NYC"}
	]`

	expected := `{"name": "Bob", "age": 25, "city": "NYC"}`

	// Create snapshot from original JSON
	snapshot, snapshotLen := getContent(t, original)

	// Parse patch from JSON
	var patchSpec JsonPatchSpec
	err := json.Unmarshal([]byte(patchJSON), &patchSpec)
	if err != nil {
		t.Fatalf("Failed to parse patch JSON: %v", err)
	}

	patch, err := NewPatch(patchSpec)
	if err != nil {
		t.Fatalf("Failed to create patch: %v", err)
	}

	// Create delta from snapshot and patch
	deltaPatch := newDeltaPatch(snapshot.Reader(), snapshotLen, nil, []interface{}{nil, nil})

	err = deltaPatch.apply(patch)
	if err != nil {
		t.Fatalf("Failed to apply patch: %v", err)
	}

	delta, err := deltaPatch.serialize()
	if err != nil {
		t.Fatalf("Failed to serialize delta: %v", err)
	}

	// Test accessing snapshot+delta to verify the patched result
	deltaReader, err := newDeltaReader(snapshot.Reader(), snapshotLen, utils.NewMultiReaderFromBytesReader(delta))
	if err != nil {
		t.Fatalf("Failed to create deltaReader: %v", err)
	}

	// Convert result to JSON and compare with expected
	obj := newObject(deltaReader, 0)
	resultJSON, err := json.Marshal(obj.JSON())
	if err != nil {
		t.Fatalf("Failed to marshal result: %v", err)
	}

	// Parse both to compare (handles different ordering)
	var resultData, expectedData interface{}
	json.Unmarshal(resultJSON, &resultData)
	json.Unmarshal([]byte(expected), &expectedData)

	if !reflect.DeepEqual(resultData, expectedData) {
		t.Errorf("Expected %s, got %s", expected, string(resultJSON))
	}

	// Apply second patch on top of the first one
	secondPatchJSON := `[
		{"op": "replace", "path": "/age", "value": 30},
		{"op": "add", "path": "/country", "value": "USA"}
	]`

	secondExpected := `{"name": "Bob", "age": 30, "city": "NYC", "country": "USA"}`

	// Parse second patch from JSON
	var secondPatchSpec JsonPatchSpec
	err = json.Unmarshal([]byte(secondPatchJSON), &secondPatchSpec)
	if err != nil {
		t.Fatalf("Failed to parse second patch JSON: %v", err)
	}

	secondPatch, err := NewPatch(secondPatchSpec)
	if err != nil {
		t.Fatalf("Failed to create second patch: %v", err)
	}

	// Apply second patch to the same deltaPatch instance
	err = deltaPatch.apply(secondPatch)
	if err != nil {
		t.Fatalf("Failed to apply second patch: %v", err)
	}

	secondDelta, err := deltaPatch.serialize()
	if err != nil {
		t.Fatalf("Failed to serialize second delta: %v", err)
	}

	// Test accessing snapshot+delta with both patches applied
	secondDeltaReader, err := newDeltaReader(snapshot.Reader(), snapshotLen, utils.NewMultiReaderFromBytesReader(secondDelta))
	if err != nil {
		t.Fatalf("Failed to create second deltaReader: %v", err)
	}

	// Convert second result to JSON and compare with second expected
	secondObj := newObject(secondDeltaReader, 0)
	secondResultJSON, err := json.Marshal(secondObj.JSON())
	if err != nil {
		t.Fatalf("Failed to marshal second result: %v", err)
	}

	// Parse both to compare (handles different ordering)
	var secondResultData, secondExpectedData interface{}
	json.Unmarshal(secondResultJSON, &secondResultData)
	json.Unmarshal([]byte(secondExpected), &secondExpectedData)

	if !reflect.DeepEqual(secondResultData, secondExpectedData) {
		t.Errorf("Expected %s, got %s", secondExpected, string(secondResultJSON))
	}
}
