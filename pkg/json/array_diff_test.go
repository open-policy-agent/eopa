package json

import (
	gojson "encoding/json"
	"reflect"
	"testing"
)

func TestArrayDiff(t *testing.T) {
	testArrayDiff(t, `[]`, `[]`, []ai{})
	testArrayDiff(t, `[]`, `[null]`, []ai{{true, 0}})
	testArrayDiff(t, `[null]`, `[]`, []ai{})
	testArrayDiff(t, `[null]`, `[null]`, []ai{})
	testArrayDiff(t, `[null]`, `["foo"]`, []ai{{true, 0}})
	testArrayDiff(t, `["foo"]`, `["foo"]`, []ai{})
	testArrayDiff(t, `["foo"]`, `["bar", "foo"]`, []ai{{true, 0}, {false, 0}})
	testArrayDiff(t, `["bar", "foo"]`, `["foo"]`, []ai{{false, 1}})
	testArrayDiff(t, `["bar", "foo"]`, `["bar", "foo"]`, []ai{})
	testArrayDiff(t, `["bar", "foo"]`, `["foo", "bar"]`, []ai{{false, 1}, {false, 0}})
	testArrayDiff(t, `[{"bar": "foo"}]`, `[{"bar": "foo"}]`, []ai{})
	testArrayDiff(t, `[{"bar": "foo"}]`, `[{"bar": "baz"}]`, []ai{{true, 0}})
	testArrayDiff(t, `[{"bar": "foo"}]`, `[{}, {"bar": "foo"}]`, []ai{{true, 0}, {false, 0}})
}

type ai struct {
	b bool
	i int
}

func testArrayDiff(t *testing.T, a string, b string, result []ai) {
	var data1 interface{}
	err := gojson.Unmarshal([]byte(a), &data1)
	if err != nil {
		t.Fatalf("Unable to unmarshal: %v", err)
	}

	var data2 interface{}
	err = gojson.Unmarshal([]byte(b), &data2)
	if err != nil {
		t.Fatalf("Unable to unmarshal: %v", err)
	}

	expected := reflect.DeepEqual(data1, data2)

	reader1, _ := getContent(t, a)
	reader2, _ := getContent(t, b)

	t1, err := reader1.ReadType(0)
	if err != nil || t1 != typeArray {
		t.Fatalf("Unable to read type: %v, or it's not array: %d", err, t1)
	}

	t2, err := reader2.ReadType(0)
	if err != nil || t2 != typeArray {
		t.Fatalf("Unable to read type: %v, or it's not array: %d", err, t2)
	}

	aa, err := reader1.ReadArray(0)
	if err != nil {
		t.Fatalf("Unable to read array: %v", err)
	}

	ab, err := reader2.ReadArray(0)
	if err != nil {
		t.Fatalf("Unable to read array: %v", err)
	}

	ha := newHashCache(reader1)
	hb := newHashCache(reader2)

	ai, changed, err := arrayDiff(reader1, aa, reader2, ab, ha, hb)
	if err != nil {
		t.Fatal(err)
	}

	if changed == expected {
		t.Fatalf("unexpected equality result: %v vs. %v", expected, changed)
	}

	if len(ai) != len(result) {
		t.Fatalf("diff resulted in wrong results: %d vs %d, %v", len(ai), len(result), changed)
	}

	for i := 0; i < len(ai); i++ {
		if ai[i].b != result[i].b {
			t.Fatalf("diff did not detect value reuse")
		}

		var off int64
		if ai[i].b {
			off, err = ab.ArrayValueOffset(result[i].i)
		} else {
			off, err = aa.ArrayValueOffset(result[i].i)
		}

		if err != nil {
			t.Fatalf("Unable to read array: %v", err)
		}
		if ai[i].offset != off {
			t.Fatalf("diff reported wrong offset")
		}
	}
}

func TestElementEqual(t *testing.T) {
	// Trivial comparisons.

	testEqual(t, `null`, `null`)
	testEqual(t, `null`, `false`)
	testEqual(t, `null`, `true`)
	testEqual(t, `null`, `"foo"`)
	testEqual(t, `null`, `"1234"`)
	testEqual(t, `null`, `1234.1`)
	testEqual(t, `null`, `[null]`)
	testEqual(t, `null`, `{"a": "b"}`)

	testEqual(t, `false`, `null`)
	testEqual(t, `false`, `false`)
	testEqual(t, `false`, `true`)
	testEqual(t, `false`, `"foo"`)
	testEqual(t, `false`, `"1234"`)
	testEqual(t, `false`, `1234.1`)
	testEqual(t, `false`, `[null]`)
	testEqual(t, `false`, `{"a": "b"}`)

	testEqual(t, `true`, `null`)
	testEqual(t, `true`, `false`)
	testEqual(t, `true`, `true`)
	testEqual(t, `true`, `"foo"`)
	testEqual(t, `true`, `"1234"`)
	testEqual(t, `true`, `1234.1`)
	testEqual(t, `true`, `[null]`)
	testEqual(t, `true`, `{"a": "b"}`)

	testEqual(t, `"foo"`, `null`)
	testEqual(t, `"foo"`, `false`)
	testEqual(t, `"foo"`, `true`)
	testEqual(t, `"foo"`, `"foo"`)
	testEqual(t, `"foo"`, `"bar"`)
	testEqual(t, `"foo"`, `"1234"`)
	testEqual(t, `"foo"`, `1234.1`)
	testEqual(t, `"foo"`, `[null]`)
	testEqual(t, `"foo"`, `{"a": "b"}`)

	testEqual(t, `"1234"`, `null`)
	testEqual(t, `"1234"`, `false`)
	testEqual(t, `"1234"`, `true`)
	testEqual(t, `"1234"`, `"foo"`)
	testEqual(t, `"1234"`, `"1234"`)
	testEqual(t, `"1234"`, `1234.1`)
	testEqual(t, `"1234"`, `[null]`)
	testEqual(t, `"1234"`, `{"a": "b"}`)

	testEqual(t, `1234.1`, `null`)
	testEqual(t, `1234.1`, `false`)
	testEqual(t, `1234.1`, `true`)
	testEqual(t, `1234.1`, `"foo"`)
	testEqual(t, `1234.1`, `"1234"`)
	testEqual(t, `1234.1`, `1234.1`)
	testEqual(t, `1234.1`, `[null]`)
	testEqual(t, `1234.1`, `{"a": "b"}`)

	testEqual(t, `[]`, `null`)
	testEqual(t, `[]`, `false`)
	testEqual(t, `[]`, `true`)
	testEqual(t, `[]`, `"foo"`)
	testEqual(t, `[]`, `"1234"`)
	testEqual(t, `[]`, `1234.1`)
	testEqual(t, `[]`, `[null]`)
	testEqual(t, `[]`, `{"a": "b"}`)

	testEqual(t, `[null]`, `null`)
	testEqual(t, `[null]`, `false`)
	testEqual(t, `[null]`, `true`)
	testEqual(t, `[null]`, `"foo"`)
	testEqual(t, `[null]`, `"1234"`)
	testEqual(t, `[null]`, `1234.1`)
	testEqual(t, `[null]`, `[null]`)
	testEqual(t, `[null]`, `["foo"]`)
	testEqual(t, `[null]`, `{"a": "b"}`)

	testEqual(t, `["foo", "bar"]`, `null`)
	testEqual(t, `["foo", "bar"]`, `false`)
	testEqual(t, `["foo", "bar"]`, `true`)
	testEqual(t, `["foo", "bar"]`, `"foo"`)
	testEqual(t, `["foo", "bar"]`, `"1234"`)
	testEqual(t, `["foo", "bar"]`, `1234.1`)
	testEqual(t, `["foo", "bar"]`, `[null]`)
	testEqual(t, `["foo", "bar"]`, `["foo"]`)
	testEqual(t, `["foo", "bar"]`, `["foo", "bar"]`)
	testEqual(t, `["foo", "bar"]`, `["foo", "foo"]`)
	testEqual(t, `["foo", "bar"]`, `{"a": "b"}`)

	testEqual(t, `{}`, `null`)
	testEqual(t, `{}`, `false`)
	testEqual(t, `{}`, `true`)
	testEqual(t, `{}`, `"foo"`)
	testEqual(t, `{}`, `"1234"`)
	testEqual(t, `{}`, `1234.1`)
	testEqual(t, `{}`, `[null]`)
	testEqual(t, `{}`, `["foo"]`)
	testEqual(t, `{}`, `{"a": "b"}`)

	testEqual(t, `{"a": "b"}`, `null`)
	testEqual(t, `{"a": "b"}`, `false`)
	testEqual(t, `{"a": "b"}`, `true`)
	testEqual(t, `{"a": "b"}`, `"foo"`)
	testEqual(t, `{"a": "b"}`, `"1234"`)
	testEqual(t, `{"a": "b"}`, `1234.1`)
	testEqual(t, `{"a": "b"}`, `[null]`)
	testEqual(t, `{"a": "b"}`, `["foo"]`)
	testEqual(t, `{"a": "b"}`, `{"a": "b"}`)
	testEqual(t, `{"a": "b"}`, `{"a": "b", "c": "d"}`)

	testEqual(t, `{"a": "b", "c": "d"}`, `null`)
	testEqual(t, `{"a": "b", "c": "d"}`, `false`)
	testEqual(t, `{"a": "b", "c": "d"}`, `true`)
	testEqual(t, `{"a": "b", "c": "d"}`, `"foo"`)
	testEqual(t, `{"a": "b", "c": "d"}`, `"1234"`)
	testEqual(t, `{"a": "b", "c": "d"}`, `1234.1`)
	testEqual(t, `{"a": "b", "c": "d"}`, `[null]`)
	testEqual(t, `{"a": "b", "c": "d"}`, `["foo"]`)
	testEqual(t, `{"a": "b", "c": "d"}`, `{"a": "b"}`)
	testEqual(t, `{"a": "b", "c": "d"}`, `{"a": "b", "c": "d"}`)

	// Differences are deeper.

	testEqual(t, `["a", {"c": "d"}]`, `["a", {"c": "d"}]`)
	testEqual(t, `["a", {"c": "d"}]`, `["a", {"c": "e"}]`)

	testEqual(t, `{"a": {"c": "d"}}`, `{"a": {"c": "d"}}`)
	testEqual(t, `{"a": {"c": "d"}}`, `{"a": {"c": "e"}}`)
}

func TestHashCache(t *testing.T) {
	hashes := make(map[uint64]bool)

	testHash(t, `null`, hashes)
	testHash(t, `false`, hashes)
	testHash(t, `true`, hashes)
	testHash(t, `"foo"`, hashes)
	testHash(t, `"1234"`, hashes)
	testHash(t, `1234.1`, hashes)
	testHash(t, `[]`, hashes)
	testHash(t, `["foo", "bar"]`, hashes)
	testHash(t, `["foo", "baz"]`, hashes)
	testHash(t, `{}`, hashes)
	testHash(t, `{"foo": "bar", "bar": "foo"}`, hashes)
	testHash(t, `{"foo": "bar", "bar": true}`, hashes)
	testHash(t, `{"foo": "bar", "bar": {"a": "b"}}`, hashes)
	testHash(t, `{"foo": "bar", "bar": {"x": "b"}}`, hashes)

	if len(hashes) != 14 {
		t.Errorf("Hashes did not result in unique values: %d", len(hashes))
	}
}

func testEqual(t *testing.T, a string, b string) {
	var data1 interface{}
	err := gojson.Unmarshal([]byte(a), &data1)
	if err != nil {
		t.Fatalf("Unable to unmarshal: %v", err)
	}

	var data2 interface{}
	err = gojson.Unmarshal([]byte(b), &data2)
	if err != nil {
		t.Fatalf("Unable to unmarshal: %v", err)
	}

	expected := reflect.DeepEqual(data1, data2)

	reader1, _ := getContent(t, a)
	reader2, _ := getContent(t, b)

	eq, err := elementEqual(reader1, 0, reader2, 0)
	if err != nil {
		t.Fatalf("Unable to compare: %v", err)
	}

	if eq != expected {
		t.Error("Equal does not compare")
	}
}

func testHash(t *testing.T, js string, hashes map[uint64]bool) uint64 {
	reader1, _ := getContent(t, js)
	h1 := newHashCache(reader1)
	hash1, err := h1.Hash(0)
	if err != nil {
		t.Fatalf("Unable to hash: %v", err)
	}

	reader2, _ := getContent(t, js)
	h2 := newHashCache(reader2)
	hash2, err := h2.Hash(0)
	if err != nil {
		t.Fatalf("Unable to hash: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("Non-deterministic hash")
	}

	chash1, err := h1.Hash(0)
	if err != nil || chash1 != hash1 {
		t.Errorf("Hash caching does not work: %v", err)
	}

	chash2, err := h2.Hash(0)
	if err != nil || chash2 != hash2 {
		t.Errorf("Hash caching does not work: %v", err)
	}

	hashes[hash1] = true

	return hash1
}
