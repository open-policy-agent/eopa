package json

import (
	"bytes"
	json "encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/styrainc/enterprise-opa-private/pkg/json/internal/utils"
)

func testDiff(t *testing.T, a interface{}, b interface{}) *deltaReader {
	var x *snapshotReader
	var y contentReader
	var xn int64

	if _, ok := a.(string); ok {
		x, xn = getContent(t, a.(string))
	}

	if _, ok := a.([]byte); ok {
		x, xn = getBinaryContent(t, a.([]byte))
	}

	if _, ok := b.(string); ok {
		y, _ = getContent(t, b.(string))
	}

	if _, ok := b.([]byte); ok {
		y, _ = getBinaryContent(t, b.([]byte))
	}

	delta, _, _, err := diff(x, xn, y)
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}

	reader, err := newDeltaReader(x.Reader(), xn, utils.NewMultiReaderFromBytesReader(delta))
	if err != nil {
		t.Fatalf("delta reader construction failed: %v", err)
	}

	return reader
}

func testType(t *testing.T, reader contentReader, offset int64, expected int) {
	val, err := reader.ReadType(offset)
	if err != nil {
		t.Fatalf("read type for delta string failed: %v", err)
	}

	if val != expected {
		t.Fatalf("read type returned wrong type: %v", val)
	}
}

func testString(t *testing.T, reader contentReader, offset int64, expected string) {
	s, err := reader.ReadString(offset)
	if err != nil {
		t.Fatalf("read string failed at offset %d: %v", offset, err)
	}

	if s != expected {
		t.Fatalf("read string result wrong: %s", s)
	}
}

func testBytes(t *testing.T, reader contentReader, offset int64, expected []byte) {
	v, err := reader.ReadBytes(offset)
	if err != nil {
		t.Fatalf("read bytes failed at offset %d: %v", offset, err)
	}

	if !bytes.Equal(v, expected) {
		t.Fatalf("read bytes result wrong: %s", v)
	}
}

func testStringInt(t *testing.T, reader contentReader, expected string) {
	i, err := reader.ReadVarInt(0)
	if err != nil {
		t.Fatalf("read string int failed: %v", err)
	}

	if fmt.Sprintf("%d", i) != expected {
		t.Fatalf("read string int result wrong: %d", i)
	}
}

func testNumber(t *testing.T, reader contentReader, offset int64, expected json.Number) {
	f, err := reader.ReadString(offset)
	if err != nil {
		t.Fatalf("read number failed: %v", err)
	}

	if json.Number(f) != expected {
		t.Fatalf("read number result wrong: %s", f)
	}
}

func testArrayLen(t *testing.T, reader contentReader, offset int64, expectedLen int) {
	a, err := reader.ReadArray(offset)
	if err != nil {
		t.Fatalf("read array failed: %v", err)
	}

	l, err := a.ArrayLen()
	if err != nil {
		t.Fatalf("read array len failed: %v", err)
	}

	if l != expectedLen {
		t.Fatalf("read array len result wrong: %d", l)
	}
}

func testArrayValue(t *testing.T, reader contentReader, offset int64, i int) int64 {
	a, err := reader.ReadArray(offset)
	if err != nil {
		t.Fatalf("read array failed: %v", err)
	}

	voff, err := a.ArrayValueOffset(i)
	if err != nil {
		t.Fatalf("read array element offset failed: %v", err)
	}

	return voff
}

func testObjectName(t *testing.T, reader *deltaReader, name string) {
	obj, err := reader.ReadObject(0)
	if err != nil {
		t.Fatalf("read object failed: %v", err)
	}

	noff, ok, err := obj.ObjectNameOffset(name)
	if err != nil || !ok {
		t.Fatalf("read object name offset failed (%v): %v", ok, err)
	}

	n, err := readString(reader.content, noff)
	if err != nil {
		t.Fatalf("read object name offset string failed (%v): %v", ok, err)
	}

	if name != n {
		t.Fatalf("read object name offset result wrong: %s", n)
	}
}

func testObjectNameMissing(t *testing.T, reader contentReader, name string) {
	obj, err := reader.ReadObject(0)
	if err != nil {
		t.Fatalf("read object failed: %v", err)
	}

	_, ok, err := obj.ObjectNameOffset(name)
	if err != nil || ok {
		t.Fatalf("read object name offset failed (%v): %v", ok, err)
	}
}

func testObjectValue(t *testing.T, reader contentReader, offset int64, name string) int64 {
	obj, err := reader.ReadObject(offset)
	if err != nil {
		t.Fatalf("read object failed: %v", err)
	}

	voff, ok, err := obj.ObjectValueOffset(name)
	if err != nil || !ok {
		t.Fatalf("read object value offset failed (%v): %v", ok, err)
	}

	properties, offsets, err := obj.objectNameValueOffsets()
	if err != nil {
		t.Fatalf("read object failed: %v", err)
	}

	if !sort.SliceIsSorted(properties, func(i, j int) bool { return properties[i].name < properties[j].name }) {
		t.Fatalf("object properties are not sorted")
	}

	i := 0
	for ; i < len(properties); i++ {
		if properties[i].name == name {
			break
		}
	}

	if i == len(properties) {
		t.Fatalf("object property not found (name)")
	}

	if offsets[i] != voff {
		t.Fatalf("object property not found (value)")
	}

	return voff
}

func testObjectValueMissing(t *testing.T, reader contentReader, offset int64, name string) {
	obj, err := reader.ReadObject(offset)
	if err != nil {
		t.Fatalf("read object failed: %v", err)
	}

	voff, ok, err := obj.ObjectValueOffset(name)
	if err != nil || ok {
		t.Fatalf("read object value offset failed (%v): %v at %d", ok, err, voff)
	}

	properties, _, err := obj.objectNameValueOffsets()
	if err != nil {
		t.Fatalf("read object failed: %v", err)
	}

	if !sort.SliceIsSorted(properties, func(i, j int) bool { return properties[i].name < properties[j].name }) {
		t.Fatalf("object properties are not sorted")
	}

	i := 0
	for ; i < len(properties); i++ {
		if properties[i].name == name {
			break
		}
	}

	if i != len(properties) {
		t.Fatalf("object property found (name)")
	}
}

func testObjectLenNames(t *testing.T, reader contentReader, offset int64, expectedNames []string) {
	expectedLen := len(expectedNames)

	obj, err := reader.ReadObject(offset)
	if err != nil {
		t.Fatalf("read object failed: %v", err)
	}

	l := obj.ObjectLen()
	if l != expectedLen {
		t.Fatalf("read object len result wrong: %d", l)
	}

	names, err := obj.ObjectNames()
	if err != nil {
		t.Fatalf("read object names failed: %v", err)
	}

	if !reflect.DeepEqual(names, expectedNames) {
		t.Fatalf("read object names result wrong: %v", names)
	}
}

func TestDeltaNil(t *testing.T) {
	// Identical types.

	reader := testDiff(t, `nil`, `nil`)
	testType(t, reader, 0, typeNil)

	// Different types.

	reader = testDiff(t, `nil`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `nil`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `nil`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `nil`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `nil`, `1234.2`)
	testType(t, reader, 0, typeNumber)
	testNumber(t, reader, 0, json.Number("1234.2"))

	reader = testDiff(t, `nil`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff := testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `nil`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `nil`, []byte("foo"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("foo"))
}

func TestDeltaBoolean(t *testing.T) {
	// Identical types.

	reader := testDiff(t, `false`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `true`, `true`)
	testType(t, reader, 0, typeTrue)

	// Different types.

	reader = testDiff(t, `false`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `false`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `false`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `false`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `false`, `1234.2`)
	testType(t, reader, 0, typeNumber)
	testNumber(t, reader, 0, json.Number("1234.2"))

	reader = testDiff(t, `false`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff := testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `false`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `true`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `true`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `true`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `true`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `true`, `1234.2`)
	testType(t, reader, 0, typeNumber)
	testNumber(t, reader, 0, json.Number("1234.2"))

	reader = testDiff(t, `true`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff = testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `true`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `true`, []byte("foo"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("foo"))
}

func TestDeltaStringPlain(t *testing.T) {
	// Identical types, identical values.

	reader := testDiff(t, `"foo"`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	// Identical types, different values.

	reader = testDiff(t, `"foo"`, `"bar"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "bar")

	// Different types.

	reader = testDiff(t, `"foo"`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `"foo"`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `"foo"`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `"foo"`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `"foo"`, `1234.2`)
	testType(t, reader, 0, typeNumber)
	testNumber(t, reader, 0, json.Number("1234.2"))

	reader = testDiff(t, `"foo"`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff := testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `"foo"`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `"foo"`, []byte("foo"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("foo"))
}

func TestDeltaStringInt(t *testing.T) {
	// Identical types, identical values.

	reader := testDiff(t, `"1234"`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	// Identical types, different values.

	reader = testDiff(t, `"1234"`, `"1235"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1235")

	// Different types.

	reader = testDiff(t, `"1234"`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `"1234"`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `"1234"`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `"1234"`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `"1234"`, `1234.2`)
	testType(t, reader, 0, typeNumber)
	testNumber(t, reader, 0, json.Number("1234.2"))

	reader = testDiff(t, `"1234"`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff := testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `"1234"`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `"1234"`, []byte("foo"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("foo"))
}

func TestDeltaNumber(t *testing.T) {
	// Identical types, identical values.

	reader := testDiff(t, `1234.1`, `1234.1`)
	testType(t, reader, 0, typeNumber)
	testNumber(t, reader, 0, json.Number("1234.1"))

	// Identical types, different values.

	reader = testDiff(t, `1234.1`, `1234.2`)
	testType(t, reader, 0, typeNumber)
	testNumber(t, reader, 0, json.Number("1234.2"))

	// Different types.

	reader = testDiff(t, `1234.1`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `1234.1`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `1234.1`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `1234.1`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `1234.1`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `1234.1`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff := testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `1234.1`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `1234.1`, []byte("foo"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("foo"))
}

func TestDeltaArray(t *testing.T) {
	// Identical types, identical values.

	reader := testDiff(t, `[]`, `[]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 0)

	reader = testDiff(t, `["bar"]`, `["bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 1)
	voff := testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `["foo", "bar"]`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff = testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	// Identical types, changed values.

	reader = testDiff(t, `[]`, `["bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 1)
	voff = testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `["foo"]`, `["bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 1)
	voff = testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `["foo"]`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff = testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `["foo", "bar"]`, `["foo"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 1)
	voff = testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")

	reader = testDiff(t, `["foo", "bar"]`, `[]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 0)

	reader = testDiff(t, `["foo"]`, `[]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 0)

	// Different types (base type is empty array).

	reader = testDiff(t, `[]`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `[]`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `[]`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `[]`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `[]`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `[]`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	// Different types (base type is simple object).

	reader = testDiff(t, `["foo", "bar"]`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `["foo", "bar"]`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `["foo", "bar"]`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `["foo", "bar"]`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `["foo", "bar"]`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `["foo", "bar"]`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `["foo", "bar"]`, []byte("foo"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("foo"))

	// Nested changes.

	reader = testDiff(t, `["foo", ["abc", "bar"]]`, `["foo", ["abc", "def"]]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff = testArrayValue(t, reader, 0, 0)
	testType(t, reader, voff, typeString)
	testString(t, reader, voff, "foo")

	voff = testArrayValue(t, reader, 0, 1)
	testType(t, reader, voff, typeArray)
	testArrayLen(t, reader, voff, 2)
	voff2 := testArrayValue(t, reader, voff, 0)
	testType(t, reader, voff2, typeString)
	testString(t, reader, voff2, "abc")
	voff2 = testArrayValue(t, reader, voff, 1)
	testType(t, reader, voff2, typeString)
	testString(t, reader, voff2, "def")

	reader = testDiff(t, `["foo", ["abc", "bar"]]`, `["foo", ["abc", "bar"], "xyz"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 3)
	voff = testArrayValue(t, reader, 0, 0)
	testType(t, reader, voff, typeString)
	testString(t, reader, voff, "foo")

	voff = testArrayValue(t, reader, 0, 1)
	testType(t, reader, voff, typeArray)
	testArrayLen(t, reader, voff, 2)
	voff2 = testArrayValue(t, reader, voff, 0)
	testType(t, reader, voff2, typeString)
	testString(t, reader, voff2, "abc")
	voff2 = testArrayValue(t, reader, voff, 1)
	testType(t, reader, voff2, typeString)
	testString(t, reader, voff2, "bar")

	voff = testArrayValue(t, reader, 0, 2)
	testType(t, reader, voff, typeString)
	testString(t, reader, voff, "xyz")
}

func TestDeltaObject(t *testing.T) {
	// Identical types, identical values.

	reader := testDiff(t, `{}`, `{}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{})

	reader = testDiff(t, `{"foo": "bar"}`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	voff := testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `{"abc": "aaa", "def": "bbb", "ghi": "ccc", "jkl": 1234}`, `{"abc": "aaa", "def": "bbb", "ghi": "ccc", "jkl": 1234}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"abc", "def", "ghi", "jkl"})
	testString(t, reader, testObjectValue(t, reader, 0, "abc"), "aaa")
	testString(t, reader, testObjectValue(t, reader, 0, "def"), "bbb")
	testString(t, reader, testObjectValue(t, reader, 0, "ghi"), "ccc")
	testNumber(t, reader, testObjectValue(t, reader, 0, "jkl"), json.Number("1234"))

	// Identical types, changed values.

	reader = testDiff(t, `{}`, `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectPatch)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `{"foo": "bar"}`, `{"foo": "abc"}`)
	testType(t, reader, 0, typeObjectPatch)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "abc")

	reader = testDiff(t, `{"foo": "bar"}`, `{}`)
	testType(t, reader, 0, typeObjectPatch)
	testObjectLenNames(t, reader, 0, []string{})
	testObjectNameMissing(t, reader, "foo")
	testObjectValueMissing(t, reader, 0, "foo")
	testObjectNameMissing(t, reader, "bar")
	testObjectValueMissing(t, reader, 0, "bar")

	// Different types (base type is empty object).

	reader = testDiff(t, `{}`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `{}`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `{}`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `{}`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `{}`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `{}`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff = testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	// Different types (base type is simple object).

	reader = testDiff(t, `{"foo": "bar"}`, `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, `{"foo": "bar"}`, `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, `{"foo": "bar"}`, `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, `{"foo": "bar"}`, `"foo"`)
	testType(t, reader, 0, typeString)
	testString(t, reader, 0, "foo")

	reader = testDiff(t, `{"foo": "bar"}`, `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, `{"foo": "bar"}`, `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff = testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, `{"foo": "bar"}`, []byte("foo"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("foo"))

	// Nested changes.

	reader = testDiff(t, `{"foo": {"abc": "bar"}}`, `{"foo": {"abc": "def"}}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testType(t, reader, voff, typeObjectPatch)
	testObjectLenNames(t, reader, voff, []string{"abc"})
	voff = testObjectValue(t, reader, voff, "abc")
	testString(t, reader, voff, "def")

	reader = testDiff(t, `{"foo": {"abc": "bar"}}`, `{"foo": {"def": "cba"}}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testType(t, reader, voff, typeObjectPatch)
	testObjectLenNames(t, reader, voff, []string{"def"})
	voff = testObjectValue(t, reader, voff, "def")
	testString(t, reader, voff, "cba")

	reader = testDiff(t, `{"foo": {"abc": {"def": "bar"}}}`, `{"foo": {"abc": {"def": "ghi"}}}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testType(t, reader, voff, typeObjectFull)
	testObjectLenNames(t, reader, voff, []string{"abc"})
	voff = testObjectValue(t, reader, voff, "abc")
	testType(t, reader, voff, typeObjectPatch)
	testObjectLenNames(t, reader, voff, []string{"def"})
	voff = testObjectValue(t, reader, voff, "def")
	testString(t, reader, voff, "ghi")

	reader = testDiff(t, `{"foo": {"abc": {"def": "bar"}}}`, `{"foo": {"abc": {"def": "ghi"}, "xyz": "zyx"}}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	fvoff := testObjectValue(t, reader, 0, "foo")
	testType(t, reader, fvoff, typeObjectPatch)
	testObjectLenNames(t, reader, fvoff, []string{"abc", "xyz"})

	voff = testObjectValue(t, reader, fvoff, "abc")
	testType(t, reader, voff, typeObjectPatch)
	testObjectLenNames(t, reader, voff, []string{"def"})
	voff = testObjectValue(t, reader, voff, "def")
	testString(t, reader, voff, "ghi")

	voff = testObjectValue(t, reader, fvoff, "xyz")
	testType(t, reader, voff, typeString)
	testString(t, reader, voff, "zyx")
}

// TestDeltaObjectTypes tests all three binary encoded object types can be diffed against each other without reverting to diff between mismatching types (requiring full new value storing).
func TestDeltaObjectTypes(t *testing.T) {
	// An array with with two objects with the same names: 1) full definition, and 2) thinned one.

	j, _ := buildJSON([]interface{}{
		map[string]interface{}{
			"a": "a",
			"b": "b",
		},
		map[string]interface{}{
			"a": "a",
			"b": "b",
		},
	})
	reader0 := j.(ArrayBinary).content
	fullOffset, err := reader0.ArrayValueOffset(0)
	if err != nil {
		panic(err)
	}

	thinOffset, err := reader0.ArrayValueOffset(1)
	if err != nil {
		panic(err)
	}

	// Manufacture a object patch type (3).

	reader1 := testDiff(t, `{"a": "a"}`, `{"a": "a", "b": "b"}`)
	patchOffset := int64(0)

	// Diff all combinations and check the result is an empty diff.

	tests := []struct {
		ra contentReader
		oa int64
		rb contentReader
		ob int64
	}{
		{reader0, fullOffset, reader0, fullOffset},
		{reader0, fullOffset, reader0, thinOffset},
		{reader0, fullOffset, reader1, patchOffset},
		{reader0, thinOffset, reader0, fullOffset},
		{reader0, thinOffset, reader0, thinOffset},
		{reader0, thinOffset, reader1, patchOffset},
		{reader1, patchOffset, reader0, fullOffset},
		{reader1, patchOffset, reader0, thinOffset},
		{reader1, patchOffset, reader1, patchOffset},
	}

	for _, test := range tests {
		var buffer bytes.Buffer
		patches := make(map[int64]int64)

		if _, _, err := diffImpl(test.ra, test.oa, 0, test.rb, test.ob, true, &buffer, patches, newEncodingCache(), newHashCache(test.ra), newHashCache(test.rb)); err != nil {
			t.Error(err.Error())
		}

		if buffer.Len() > 0 {
			t.Errorf("not empty")
		}
	}
}

func TestDeltaBlob(t *testing.T) {
	// Identical types, identical values.

	reader := testDiff(t, []byte("foo"), []byte("foo"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("foo"))

	// Identical types, different values.

	reader = testDiff(t, []byte("foo"), []byte("bar"))
	testType(t, reader, 0, typeBinaryFull)
	testBytes(t, reader, 0, []byte("bar"))

	// Different types.

	reader = testDiff(t, []byte("foo"), `nil`)
	testType(t, reader, 0, typeNil)

	reader = testDiff(t, []byte("foo"), `false`)
	testType(t, reader, 0, typeFalse)

	reader = testDiff(t, []byte("foo"), `true`)
	testType(t, reader, 0, typeTrue)

	reader = testDiff(t, []byte("foo"), `"1234"`)
	testType(t, reader, 0, typeStringInt)
	testStringInt(t, reader, "1234")

	reader = testDiff(t, []byte("foo"), `1234.2`)
	testType(t, reader, 0, typeNumber)
	testNumber(t, reader, 0, json.Number("1234.2"))

	reader = testDiff(t, []byte("foo"), `["foo", "bar"]`)
	testType(t, reader, 0, typeArray)
	testArrayLen(t, reader, 0, 2)
	voff := testArrayValue(t, reader, 0, 0)
	testString(t, reader, voff, "foo")
	voff = testArrayValue(t, reader, 0, 1)
	testString(t, reader, voff, "bar")

	reader = testDiff(t, []byte("foo"), `{"foo": "bar"}`)
	testType(t, reader, 0, typeObjectFull)
	testObjectLenNames(t, reader, 0, []string{"foo"})
	testObjectName(t, reader, "foo")
	voff = testObjectValue(t, reader, 0, "foo")
	testString(t, reader, voff, "bar")
}

func TestDeltaPrimitiveCaching(t *testing.T) {
	// String caching within the snapshot does not break delta.

	reader := testDiff(t, `{"abc": "bar", "def": "bar"}`, `{"abc": "bar", "def": "foo"}`)
	testType(t, reader, 0, typeObjectPatch)
	testObjectLenNames(t, reader, 0, []string{"abc", "def"})

	testObjectName(t, reader, "abc")
	voff := testObjectValue(t, reader, 0, "abc")
	testString(t, reader, voff, "bar")

	testObjectName(t, reader, "def")
	voff = testObjectValue(t, reader, 0, "def")
	testString(t, reader, voff, "foo")

	// String caching within the delta works.

	reader = testDiff(t, `{"abc": "bar", "def": "bar", "ghi": "pim"}`, `{"abc": "bar", "def": "foo", "ghi": "foo"}`)
	testType(t, reader, 0, typeObjectPatch)
	testObjectLenNames(t, reader, 0, []string{"abc", "def", "ghi"})

	testObjectName(t, reader, "abc")
	voff = testObjectValue(t, reader, 0, "abc")
	testString(t, reader, voff, "bar")

	testObjectName(t, reader, "def")
	voff = testObjectValue(t, reader, 0, "def")
	testString(t, reader, voff, "foo")

	testObjectName(t, reader, "ghi")
	voff = testObjectValue(t, reader, 0, "ghi")
	testString(t, reader, voff, "foo")
}

func TestDeltaEmbedding(t *testing.T) {
	reader := testDiff(t, `{"abc": false, "def": true, "ghi": "foo"}`, `{"abc": true, "def": false, "ghi": null}`)
	testType(t, reader, 0, typeObjectPatch)
	testObjectLenNames(t, reader, 0, []string{"abc", "def", "ghi"})

	testObjectName(t, reader, "abc")
	voff := testObjectValue(t, reader, 0, "abc")
	if voff != -typeTrue {
		t.Errorf("wrong embedded type returned: %d", voff)
	}

	testObjectName(t, reader, "def")
	voff = testObjectValue(t, reader, 0, "def")
	if voff != -typeFalse {
		t.Errorf("wrong embedded type returned: %d", voff)
	}

	testObjectName(t, reader, "ghi")
	voff = testObjectValue(t, reader, 0, "ghi")
	if voff != -typeNil {
		t.Errorf("wrong embedded type returned: %d", voff)
	}
}

func getContent(t *testing.T, jsonStr string) (*snapshotReader, int64) {
	var data interface{}
	json.Unmarshal([]byte(jsonStr), &data)

	c, n, err := translate(data)
	if err != nil {
		t.Fatalf("Cannot translate to binary format, err: %v", err)
	}

	return c, n
}

func getBinaryContent(t *testing.T, data []byte) (*snapshotReader, int64) {
	c, n, err := translate(data)
	if err != nil {
		t.Fatalf("Cannot translate to binary format, err: %v", err)
	}

	return c, n
}

// BenchmarkDeltaWrite benchmarks patching a large delta snapshot with a small patch.
func BenchmarkDeltaWrite(b *testing.B) {
	obj := make(map[string]interface{})

	for i := 0; i < 10000; i++ {
		obj[fmt.Sprintf("key:%d", i)] = fmt.Sprintf("value:%d", i)
	}

	now := time.Now()
	snapshot := testCollectionCreate(testCollection{"": testResource{V: obj}}, now)

	for n := 0; n < b.N; n++ {
		writable := snapshot.Writable()
		doc := writable.Resource("").JSON().Clone(false).(Object)
		doc.Set("key:1", NewString("patched"))
		writable.WriteJSON("", doc)
		snapshot.Diff(writable.Prepare(now))
	}
}
