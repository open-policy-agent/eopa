package json

import (
	"bufio"
	"bytes"
	gojson "encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/open-policy-agent/eopa/pkg/json/internal/utils"
)

func TestCollectionsSerialization(t *testing.T) {
	testCollectionsSerialization(t, ``)
	testCollectionsSerialization(t, `{"foo": ["bar", "baz"], "": 0, "a/b": 1, "c%d": 2, "e^f": 3, "g|h": 4, "i\\\\j": 5, "k\"l": 6, " ": 7, "m~n": 8, "n": null}`)
}

func testCollectionsSerialization(t *testing.T, jsonStr string) {
	collections := NewCollections()

	if jsonStr != "" {
		var data interface{}
		gojson.Unmarshal([]byte(jsonStr), &data)

		c, err := New(data)
		if err != nil {
			t.Fatalf("Cannot translate to binary format, err: %v", err)
		}
		collections.WriteJSON("coll", c)
	}

	// serialize binary json to a byte buffer
	var buff bytes.Buffer
	w := bufio.NewWriter(&buff)
	defer w.Flush()

	col := collections.Prepare(time.Now())
	col.WriteTo(w)

	// load back the serialized binary json
	col1, err := NewCollectionsFromReaders(utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(buff.Bytes())), int64(len(buff.Bytes())), nil, 0, nil)
	if err != nil {
		t.Fatalf("Cannot load binary format, err: %v", err)
	}

	if !equalCollections(t, col, col1) {
		t.Fatalf("Loaded binary json does not match original binary json")
	}
}

func TestCollectionsDeltaSerialization(t *testing.T) {
	// Test overwriting the entire collection with an empty one and check it behaves like an empty one.

	w1 := NewCollections()

	blob := NewBlob([]byte("foo"))
	w1.WriteBlob("a/b/c", blob)
	w2 := NewCollections()

	c1 := w1.Prepare(time.Now())
	c2 := w2.Prepare(time.Now())

	delta, n, _, err := c1.Diff(c2)
	if err != nil {
		t.Fatalf("unable to compute a diff: %v", err)
	}

	d, err := NewCollectionsFromReaders(
		utils.NewMultiReaderFromMultiReaders(
			c1.(*snapshot).content.(*snapshotObjectReader).content,
			0,
			int64(c1.Len()),
			nil),
		c1.Len(),
		utils.NewMultiReaderFromBytesReader(delta),
		n,
		nil, nil)
	if err != nil {
		t.Fatalf("unable to construct a snapshot: %v", err)
	}

	root := d.Resource("")
	if len(root.Resources()) != 0 {
		t.Errorf("able to access a removed resource")
	}

	if d.Resource("a/b/c") != nil {
		t.Errorf("able to access a removed resource")
	}
}

func TestCollectionsNamespace(t *testing.T) {
	// Populate a writable collection with three collections.

	data1 := map[string]interface{}{"foo": "foo"}
	data2 := map[string]interface{}{"foo": "bar"}
	data3 := map[string]interface{}{"foo": "foobar"}

	wcollections := NewCollections()
	coll1, err := New(data1)
	if err != nil {
		t.Fatal("unable to create collection")
	}
	wcollections.WriteJSON("coll1", coll1)
	coll2, err := New(data2)
	if err != nil {
		t.Fatal("unable to create collection")
	}
	wcollections.WriteJSON("ns1/coll2", coll2)
	coll3, err := New(data3)
	if err != nil {
		t.Fatal("unable to create collection")
	}
	wcollections.WriteJSON("ns1/ns2/coll3", coll3)

	// Verify the collections are accessible.

	testVerifyWResource(t, wcollections, "", "", Directory)
	testVerifyWResource(t, wcollections, "invalid", "", Invalid)
	testVerifyWResource(t, wcollections, "coll1", "coll1", JSON)
	testVerifyWResource(t, wcollections, "ns1", "ns1", Directory)
	testVerifyWResource(t, wcollections, "/ns1", "ns1", Directory)
	testVerifyWResource(t, wcollections, "ns1/invalid", "", Invalid)
	testVerifyWResource(t, wcollections, "ns1/coll2", "ns1/coll2", JSON)
	testVerifyWResource(t, wcollections, "ns1/ns2", "ns1/ns2", Directory)
	testVerifyWResource(t, wcollections, "ns1/ns2/invalid", "", Invalid)
	testVerifyWResource(t, wcollections, "ns1/ns2/coll3", "ns1/ns2/coll3", JSON)
	testVerifyWResource(t, wcollections, "ns1/ns2/coll3/foo", "", Invalid)
	testVerifyWResources(t, wcollections, "", []string{"coll1", "ns1"})
	testVerifyWResources(t, wcollections, "ns1", []string{"ns1/coll2", "ns1/ns2"})
	testVerifyWResources(t, wcollections, "ns1/ns2", []string{"ns1/ns2/coll3"})

	rcollections := wcollections.Prepare(time.Now())

	testVerifyRResource(t, rcollections, "", "", Directory)
	testVerifyRResource(t, rcollections, "invalid", "", Invalid)
	testVerifyRResource(t, rcollections, "coll1", "coll1", JSON)
	testVerifyRResource(t, rcollections, "ns1", "ns1", Directory)
	testVerifyRResource(t, rcollections, "/ns1", "ns1", Directory)
	testVerifyRResource(t, rcollections, "ns1/invalid", "", Invalid)
	testVerifyRResource(t, rcollections, "ns1/coll2", "ns1/coll2", JSON)
	testVerifyRResource(t, rcollections, "ns1/ns2", "ns1/ns2", Directory)
	testVerifyRResource(t, rcollections, "ns1/ns2/invalid", "", Invalid)
	testVerifyRResource(t, rcollections, "ns1/ns2/coll3", "ns1/ns2/coll3", JSON)
	testVerifyRResource(t, rcollections, "ns1/ns2/coll3/foo", "", Invalid)
	testVerifyRResources(t, rcollections, "", []string{"coll1", "ns1"})
	testVerifyRResources(t, rcollections, "ns1", []string{"ns1/coll2", "ns1/ns2"})
	testVerifyRResources(t, rcollections, "ns1/ns2", []string{"ns1/ns2/coll3"})

	// Root being a non-directory resource itself.

	wcollections = NewCollections()
	coll1, err = New("foo")
	if err != nil {
		t.Fatal("unable to create collection")
	}

	wcollections.WriteJSON("", coll1)
	testVerifyWResource(t, wcollections, "", "", JSON)

	rcollections = wcollections.Prepare(time.Now())
	testVerifyRResource(t, rcollections, "", "", JSON)
}

func TestCollectionsPrepare(t *testing.T) {
	w := NewCollections()
	w.WriteBlob("a/b/c", NewBlob([]byte("foo")))
	w.WriteBlob("a/b/d", NewBlob([]byte("bar")))

	r1 := w.Prepare(time.Now())
	r2 := r1.Writable().Prepare(time.Now())

	if _, _, identical, err := r1.Diff(r2); !identical || err != nil {
		t.Errorf("readable->writable->readable cycle did not result in identical result: %v %v", identical, err)
	}
}

func TestCollectionsMeta(t *testing.T) {
	w := NewCollections()
	w.WriteBlob("a/b", NewBlob([]byte("foo")))
	w.WriteDirectory("b/c")

	if ok := w.WriteMeta("", "key", "value0"); !ok {
		t.Errorf("unable to write a meta key")
	}

	if ok := w.WriteMeta("a", "key", "value1"); !ok {
		t.Errorf("unable to write a meta key")
	}

	if ok := w.WriteMeta("a/b", "key", "value2"); !ok {
		t.Errorf("unable to write a meta key")
	}

	if ok := w.WriteMeta("b/c", "key", "value3"); !ok {
		t.Errorf("unable to write a meta key")
	}

	if ok := w.WriteMeta("a/b/c", "key", "value3"); ok {
		t.Errorf("able to write a meta key to nonexisting resource")
	}

	if v, ok := w.Resource("").Meta("key"); !ok || v != "value0" {
		t.Errorf("nonexisting meta key not found")
	}

	if v, ok := w.Resource("a").Meta("key"); !ok || v != "value1" {
		t.Errorf("existing meta key not found")
	}

	if v, ok := w.Resource("a/b").Meta("key"); !ok || v != "value2" {
		t.Errorf("existing meta key not found")
	}

	if v, ok := w.Resource("b/c").Meta("key"); !ok || v != "value3" {
		t.Errorf("existing meta key not found")
	}

	w2 := w.Prepare(time.Now())
	if v, ok := w2.Resource("a/b").Meta("key"); !ok || v != "value2" {
		t.Errorf("existing meta key not found (after preparing)")
	}
}

func TestBlobSerialization(t *testing.T) {
	data := []byte("foo")

	collections := NewCollections()
	b := NewBlob(data)
	collections.WriteBlob("blob", b)

	// serialize binary json to a byte buffer
	var buff bytes.Buffer
	w := bufio.NewWriter(&buff)
	defer w.Flush()

	col := collections.Prepare(time.Now())
	col.WriteTo(w)

	// load back the serialized binary json
	col1, err := NewCollectionsFromReaders(utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(buff.Bytes())), int64(len(buff.Bytes())), nil, 0, nil)
	if err != nil {
		t.Fatalf("Cannot load binary format, err: %v", err)
	}

	if !equalCollections(t, col, col1) {
		t.Fatalf("Loaded binary json does not match original binary json")
	}
}

func testVerifyWResource(t *testing.T, c WritableCollections, name string, expected string, kind Kind) {
	r := c.Resource(name)
	if kind == Invalid {
		if r != nil {
			t.Errorf("resource '%s' should not exist", name)
		}

		return
	} else if r == nil {
		t.Errorf("resource '%s' not found", name)
		return
	}

	if r.Kind() != kind {
		t.Errorf("resource '%s' has wrong type: %d vs %d", name, int(kind), int(r.Kind()))
	}

	if r.Name() != expected {
		t.Errorf("resource '%s' has wrong name: %s vs %s", name, r.Name(), expected)
	}

	// Find the resource by traversing.

	if kind != Invalid {
		name = strings.TrimPrefix(name, "/")
		segs := strings.Split(name, "/")

		r := c.Resource(segs[0])
		for i, seg := range segs[1:] {
			r = r.Resource(seg)
			e := strings.Join(segs[0:i+2], "/")
			if r.Name() != e {
				t.Errorf("path name mismatch: %v vs %v", r.Name(), e)
			}
		}
	}
}

func testVerifyWResources(t *testing.T, c WritableCollections, name string, expected []string) {
	r := c.Resource(name)
	if r == nil {
		t.Errorf("resource '%s' should exist", name)
	}

	resources := r.Resources()
	names := make([]string, 0, len(resources))
	for _, resource := range resources {
		names = append(names, resource.Name())
	}

	if !reflect.DeepEqual(names, expected) {
		t.Errorf("resource '%s' has wrong children: %s vs %s", name, names, expected)
	}
}

func testVerifyRResource(t *testing.T, c Collections, name string, expected string, kind Kind) {
	r := c.Resource(name)
	if kind == Invalid {
		if r != nil {
			t.Errorf("resource '%s' should not exist", name)
		}

		return
	} else if r == nil {
		t.Errorf("resource '%s' not found", name)
		return
	}

	if r.Kind() != kind {
		t.Errorf("resource '%s' has wrong type: %d vs %d", name, int(kind), int(r.Kind()))
	}

	if r.Name() != expected {
		t.Errorf("resource '%s' has wrong name: %s vs %s", name, r.Name(), expected)
	}

	// Find the resource by traversing.

	if kind != Invalid {
		name = strings.TrimPrefix(name, "/")
		segs := strings.Split(name, "/")

		r := c.Resource(segs[0])
		for i, seg := range segs[1:] {
			r = r.Resource(seg)
			e := strings.Join(segs[0:i+2], "/")
			if r.Name() != e {
				t.Errorf("path name mismatch: %v vs %v", r.Name(), e)
			}
		}
	}
}

func testVerifyRResources(t *testing.T, c Collections, name string, expected []string) {
	r := c.Resource(name)
	if r == nil {
		t.Errorf("resource '%s' should exist", name)
	}

	resources := r.Resources()
	names := make([]string, 0, len(resources))
	for _, resource := range resources {
		names = append(names, resource.Name())
	}

	if !reflect.DeepEqual(names, expected) {
		t.Errorf("resource '%s' has wrong children: %s vs %s", name, names, expected)
	}
}

// TODO we should add Equal to Collections,Collection and Document (expensive on large Collections/Documents)
func equalCollections(_ *testing.T, c1, c2 Collections) bool {
	var r1, r2 []Resource

	c1.Walk(func(resource Resource) bool {
		r1 = append(r1, resource)
		return true
	})

	c2.Walk(func(resource Resource) bool {
		r2 = append(r2, resource)
		return true
	})

	if len(r1) != len(r2) {
		return false
	}

	for i := range r1 {
		if r1[i].Name() != r2[i].Name() {
			return false
		}

		if r1[i].Kind() != r2[i].Kind() {
			return false
		}

		if r1[i].Kind() == JSON {
			if r1[i].JSON().Compare(r2[i].JSON()) != 0 {
				return false
			}
		} else if r1[i].Kind() == Unstructured {
			if !bytes.Equal(r1[i].Blob().Value(), r2[i].Blob().Value()) {
				return false
			}
		}
	}

	return true
}
