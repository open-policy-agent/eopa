package json

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/open-policy-agent/eopa/pkg/json/internal/utils"
)

type testResource struct {
	V interface{}
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

func testWriteJSON(path string, v interface{}) testOperation {
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
	for n, r := range expected {
		files[n] = r
	}

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
		if utils.Contains(names, "data") {
			for _, name := range names {
				if strings.HasPrefix(name, "data:") {
					t.Errorf("data and data: both defined")
				}
			}

			if !utils.Contains(names, "kind") {
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
			PreCollection: testCollection{"a": testResource{V: map[string]interface{}{"foo": "abc"}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]interface{}{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]interface{}{"foo": "def"}},
			},
		},
		{
			Description:   "Patch JSON non-root (add)",
			PreCollection: testCollection{"a": testResource{V: map[string]interface{}{"foo": "abc"}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]interface{}{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]interface{}{"foo": "def"}},
			},
		},
		{
			Description:   "Patch JSON non-root (create 1)",
			PreCollection: testCollection{"a": testResource{V: map[string]interface{}{}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]interface{}{
					"op":    "create",
					"path":  "/a",
					"value": "value",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]interface{}{"a": "value"}},
			},
		},
		{
			Description:   "Patch JSON non-root (create 2)",
			PreCollection: testCollection{"a": testResource{V: map[string]interface{}{}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]interface{}{
					"op":    "create",
					"path":  "/a/b",
					"value": "value",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]interface{}{"a": map[string]interface{}{"b": "value"}}},
			},
		},
		{
			Description:   "Patch JSON non-root (create 3)",
			PreCollection: testCollection{"a": testResource{V: map[string]interface{}{}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]interface{}{
					"op":    "create",
					"path":  "/a/b/c",
					"value": "value",
				},
			})},
			PostCollection: testCollection{
				"":  testResource{},
				"a": testResource{V: map[string]interface{}{"a": map[string]interface{}{"b": map[string]interface{}{"c": "value"}}}},
			},
		},
		{
			Description: "Patch JSON non-root (two non-overlapping patches)",
			PreCollection: testCollection{"a": testResource{V: map[string]interface{}{
				"foo": "abc",
				"bar": "def",
			}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]interface{}{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
				map[string]interface{}{
					"op":    "add",
					"path":  "/bar",
					"value": "ghi",
				},
			})},
			PostCollection: testCollection{
				"": testResource{},
				"a": testResource{V: map[string]interface{}{
					"foo": "def",
					"bar": "ghi",
				}},
			},
		},
		{
			Description: "Patch JSON non-root (two overlapping patches #1)",
			PreCollection: testCollection{"a": testResource{V: map[string]interface{}{
				"foo": "abc",
				"bar": "def",
			}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]interface{}{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
				map[string]interface{}{
					"op":   "add",
					"path": "",
					"value": map[string]interface{}{
						"foo": "bar",
					},
				},
			})},
			PostCollection: testCollection{
				"": testResource{},
				"a": testResource{V: map[string]interface{}{
					"foo": "bar",
				}},
			},
		},
		{
			Description: "Patch JSON non-root (two overlapping patches #2)",
			PreCollection: testCollection{"a": testResource{V: map[string]interface{}{
				"foo": "abc",
				"bar": "def",
			}}},
			Operations: []testOperation{testPatchJSON("a", JsonPatchSpec{
				map[string]interface{}{
					"op":   "add",
					"path": "",
					"value": map[string]interface{}{
						"foo": "bar",
					},
				},
				map[string]interface{}{
					"op":    "add",
					"path":  "/foo",
					"value": "def",
				},
			})},
			PostCollection: testCollection{
				"": testResource{},
				"a": testResource{V: map[string]interface{}{
					"foo": "def",
				}},
			},
		},
		{
			Description: "Patch JSON root (nested patches #1)",
			PreCollection: testCollection{"": testResource{V: map[string]interface{}{
				"foo": map[string]interface{}{
					"nested": "abc",
				},
				"bar": "def",
			}}},
			Operations: []testOperation{testPatchJSON("", JsonPatchSpec{
				map[string]interface{}{
					"op":    "add",
					"path":  "/new",
					"value": "value",
				},
				map[string]interface{}{
					"op":    "add",
					"path":  "/foo/nested",
					"value": "patched", // This value will be incorrectly included, unless patches are recursively removed.
				},
				map[string]interface{}{
					"op":   "add",
					"path": "",
					"value": map[string]interface{}{
						"foo": map[string]interface{}{
							"nested": "abc",
						},

						"bar": "patched",
					},
				},
			})},
			PostCollection: testCollection{
				"": testResource{V: map[string]interface{}{
					"foo": map[string]interface{}{
						"nested": "abc",
					},
					"bar": "patched",
				}},
			},
		},
		{
			Description: "Patch JSON root (nested patches #2)",
			PreCollection: testCollection{"": testResource{V: map[string]interface{}{
				"abc": "123",
			}}},
			Operations: []testOperation{testPatchJSON("", JsonPatchSpec{
				map[string]interface{}{
					"op":    "add",
					"path":  "/abc",
					"value": "456",
				},
				map[string]interface{}{ // This operation should not remove the previous patch, even though it'll modify the root.
					"op":    "add",
					"path":  "/def",
					"value": "789",
				},
			})},
			PostCollection: testCollection{
				"": testResource{V: map[string]interface{}{
					"abc": "456",
					"def": "789",
				}},
			},
		},
		{
			Description: "Patch JSON root (embedded types patched)",
			PreCollection: testCollection{"": testResource{V: map[string]interface{}{
				"abc": nil, // Embedded types (nil, booleans), have no offsets to remove.
			}}},
			Operations: []testOperation{testPatchJSON("", JsonPatchSpec{
				map[string]interface{}{
					"op":   "add",
					"path": "/abc",
					"value": map[string]interface{}{
						"def": true,
						"ghi": false,
					},
				},
				map[string]interface{}{
					"op":    "add",
					"path":  "",
					"value": map[string]interface{}{},
				},
			})},
			PostCollection: testCollection{
				"": testResource{V: map[string]interface{}{}},
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
	obj := make(map[string]interface{})
	for i := 0; i < 10000; i++ {
		obj[fmt.Sprintf("key:%d", i)] = fmt.Sprintf("value:%d", i)
	}

	now := time.Now()
	snapshot := testCollectionCreate(testCollection{"": testResource{V: obj}}, now)
	delta := testPatchJSON("", JsonPatchSpec{
		map[string]interface{}{
			"op":    "add",
			"path":  "/key:0",
			"value": "patched",
		},
	})(snapshot)

	for n := 0; n < b.N; n++ {
		testPatchJSON("", JsonPatchSpec{
			map[string]interface{}{
				"op":    "add",
				"path":  "/key:1",
				"value": "patched",
			},
		})(delta)
	}
}
