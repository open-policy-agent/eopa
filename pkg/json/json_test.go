package json

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"

	//"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/ast"

	"github.com/styrainc/load-private/pkg/json/internal/utils"
)

func TestNil(t *testing.T) {
	j, err := buildJSON(nil)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeNil})
	if _, ok := j.(Null); !ok {
		t.Errorf("Incorrect value")
	}
}

func TestBool(t *testing.T) {
	j, err := buildJSON(false)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeFalse})
	if b, ok := j.(Bool); !ok || b.Value() {
		t.Errorf("Incorrect value")
	}

	j, err = buildJSON(true)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}
	validateSerialization(t, j, []byte{typeTrue})
	if b, ok := j.(Bool); !ok || !b.Value() {
		t.Errorf("Incorrect value")
	}
}

func TestString(t *testing.T) {
	// Empty string.

	j, err := buildJSON("")
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeString, 0x0})
	if s, ok := j.(String); !ok || s.Value() != "" {
		t.Errorf("Incorrect value")
	}

	// Non-empty.

	j, err = buildJSON("foo")
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeString, 0x6, 'f', 'o', 'o'})
	if s, ok := j.(String); !ok || s.Value() != "foo" {
		t.Errorf("Incorrect value")
	}
}

func TestStringInt(t *testing.T) {
	j, err := buildJSON("1234")
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeStringInt, 0xa4, 0x13})
	if s, ok := j.(String); !ok || s.Value() != "1234" {
		t.Errorf("Incorrect value")
	}

	// However, if the conversion to integer doesn't result in the exact same string,
	// use the full string presentation.
	j, err = buildJSON("01234")
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeString, 0x0a, '0', '1', '2', '3', '4'})
	if s, ok := j.(String); !ok || s.Value() != "01234" {
		t.Errorf("Incorrect value")
	}
}

func TestFloat(t *testing.T) {
	j, err := buildJSON(0.1234)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeNumber, 0xc, 0x30, 0x2e, 0x31, 0x32, 0x33, 0x34})
	if f, ok := j.(Float); !ok || f.Value() != json.Number("0.1234") {
		t.Errorf("Incorrect value")
	}
}

func TestArray(t *testing.T) {
	// Empty array

	a := []interface{}{}

	j, err := buildJSON(a)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeArray, 0})

	array, ok := j.(Array)
	if !ok {
		t.Errorf("No array loaded")
	}

	if array.Len() != 0 {
		t.Errorf("Element not loaded")
	}

	// Trivial array

	a = []interface{}{"a"}

	j, err = buildJSON(a)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeArray, 0x2, 0, 0, 0, 0x6, typeString, 0x2, 'a'})

	array, ok = j.(Array)
	if !ok {
		t.Errorf("No array loaded")
	}

	if array.Len() != 1 {
		t.Errorf("Element not loaded")
	}

	elem0 := array.Value(0)
	if s0, ok := elem0.(String); !ok || s0.Value() != "a" {
		t.Errorf("Wrong element 0 loaded")
	}

	// Longer array.

	a = []interface{}{"a", "b", "c"}
	j, err = buildJSON(a)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	array, ok = j.(Array)
	if !ok {
		t.Errorf("No array loaded")
	}

	if array.Len() != 3 {
		t.Errorf("Elements not loaded")
	}

	elem0 = array.Value(0)
	elem1 := array.Value(1)
	elem2 := array.Value(2)
	if s0, ok := elem0.(String); !ok || s0.Value() != "a" {
		t.Errorf("Wrong element 0 loaded")
	}

	if s1, ok := elem1.(String); !ok || s1.Value() != "b" {
		t.Errorf("Wrong element 0 loaded")
	}

	if s2, ok := elem2.(String); !ok || s2.Value() != "c" {
		t.Errorf("Wrong element 0 loaded")
	}

	clone := array.Clone(true).(Array)
	if array.Len() != clone.Len() {
		t.Errorf("Array.Clone is broken")
	}

	clone.RemoveIdx(1)
	if clone.Len() != 2 {
		t.Fatalf("Array.RemoveIdx is broken")
	}
	elem0 = clone.Value(0)
	elem1 = clone.Value(1)

	if s0, ok := elem0.(String); !ok || s0.Value() != "a" {
		t.Fatalf("Array.RemoveIdx is broken")
	}

	if s1, ok := elem1.(String); !ok || s1.Value() != "c" {
		t.Fatalf("Array.RemoveIdx is broken")
	}
}

func TestObjectFull(t *testing.T) {
	// Empty object

	m := make(map[string]interface{})

	j, err := buildJSON(m)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeObjectFull, 0})

	// Trivial object

	m = make(map[string]interface{})
	m["a"] = "b"

	j, err = buildJSON(m)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeObjectFull, 0x2, 0, 0, 0, 0xa, 0, 0, 0, 0xc, 0x2, 'a', typeString, 0x2, 'b'})

	obj, ok := j.(Object)
	if !ok {
		t.Errorf("No object loaded")
	}

	if obj.Len() != 1 {
		t.Errorf("Key-value pair not loaded")
	}

	value := obj.Value("a")
	if str, ok := value.(String); !ok || str.Value() != "b" {
		t.Errorf("Value incorrect")
	}

	// Check nonexisting value returns nil and does not crash.

	if value = obj.Value("nonexisting"); value != nil {
		t.Errorf("nonexisting value found")
	}

	// Larger object.

	m["a"] = "b"
	m["b"] = "c"
	m["c"] = "d"

	j, err = buildJSON(m)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	obj, ok = j.(Object)
	if !ok {
		t.Errorf("No object loaded")
	}

	if obj.Len() != 3 {
		t.Errorf("Key-value pairs not loaded")
	}

	valueA := obj.Value("a")
	if str, ok := valueA.(String); !ok || str.Value() != "b" {
		t.Errorf("Value incorrect: %s", str)
	}

	valueB := obj.Value("b")
	if str, ok := valueB.(String); !ok || str.Value() != "c" {
		t.Errorf("Value incorrect: %s", str)
	}

	valueC := obj.Value("c")
	if str, ok := valueC.(String); !ok || str.Value() != "d" {
		t.Errorf("Value incorrect: %s", str)
	}

	// Clone.

	if cloned := j.Clone(true).(Json); cloned.Compare(obj) != 0 {
		t.Errorf("Cloning doesn't result in the exact same object")
	}
}

func TestObjectThin(t *testing.T) {
	// An array with with two objects with the same names.

	mx := make(map[string]interface{})
	mx["a"] = "xa"
	mx["b"] = "xb"

	my := make(map[string]interface{})
	my["a"] = "ya"
	my["b"] = "yb"

	a := []interface{}{mx, my}

	j, err := buildJSON(a)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{
		// Array header:
		typeArray, 0x4, 0, 0, 0, 0xa, 0, 0, 0, 0x28,

		// First object (full) header:
		typeObjectFull, 0x4, 0, 0, 0, 0x1c, 0, 0, 0, 0x1e, 0, 0, 0, 0x20, 0, 0, 0, 0x24,

		// Names:
		0x2, 'a', 0x2, 'b',

		// Values:
		typeString, 0x4, 'x', 'a', typeString, 0x4, 'x', 'b',

		// Second object (thin) header:
		typeObjectThin, 0, 0, 0, 0xa, 0, 0, 0, 0x35, 0, 0, 0, 0x39,

		// Values:
		typeString, 0x4, 'y', 'a', typeString, 0x4, 'y', 'b',
	})

	array, ok := j.(Array)
	if !ok {
		t.Errorf("No array loaded")
	}

	if array.Len() != 2 {
		t.Errorf("Array elements not loaded")
	}

	value0 := array.Value(0)
	if obj, ok := value0.(Object); !ok {
		t.Errorf("Value 0 incorrect")
	} else {
		if obj.Len() != 2 {
			t.Errorf("Value 0 incorrect")
		}

		valueA := obj.Value("a")
		if str, ok := valueA.(String); !ok || str.Value() != "xa" {
			t.Errorf("Value incorrect: %s", str)
		}

		valueB := obj.Value("b")
		if str, ok := valueB.(String); !ok || str.Value() != "xb" {
			t.Errorf("Value incorrect: %s", str)
		}
	}

	value1 := array.Value(1)
	if obj, ok := value1.(Object); !ok {
		t.Errorf("Value 1 incorrect")
	} else {
		if obj.Len() != 2 {
			t.Errorf("Value 1 incorrect")
		}

		valueA := obj.Value("a")
		if str, ok := valueA.(String); !ok || str.Value() != "ya" {
			t.Errorf("Value incorrect: %s", str)
		}

		valueB := obj.Value("b")
		if str, ok := valueB.(String); !ok || str.Value() != "yb" {
			t.Errorf("Value incorrect: %s", str)
		}
	}

	// An array with with two empty objects (the latter will be thin).

	mx = make(map[string]interface{})
	my = make(map[string]interface{})

	a = []interface{}{mx, my}

	j, err = buildJSON(a)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{
		// Array header:
		typeArray, 0x4, 0, 0, 0, 0xa, 0, 0, 0, 0xc,

		// First object (full) header:
		typeObjectFull, 0x0,

		// Second object (thin) header:
		typeObjectThin, 0, 0, 0, 0xa,
	})

	array, ok = j.(Array)
	if !ok {
		t.Errorf("No array loaded")
	}

	if array.Len() != 2 {
		t.Errorf("Array elements not loaded")
	}

	value0 = array.Value(0)
	if obj, ok := value0.(Object); !ok {
		t.Errorf("Value 0 incorrect")
	} else if obj.Len() != 0 {
		t.Errorf("Value 0 incorrect")
	}

	value1 = array.Value(1)
	if obj, ok := value1.(Object); !ok {
		t.Errorf("Value 1 incorrect")
	} else if obj.Len() != 0 {
		t.Errorf("Value 1 incorrect")
	}

	// Clone.

	if cloned := array.Clone(true).(Json); cloned.Compare(array) != 0 {
		t.Errorf("Cloning doesn't result in the exact same array (with objects in)")
	}
}

func TestDeduplication(t *testing.T) {
	// Test the strings and floats are dedup'ed.

	a := []interface{}{"a", "a", float64(0.1234), float64(0.1234)}
	j, err := buildJSON(a)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{
		// Array header:
		typeArray, 0x8,
		0, 0, 0, 0x12, // First element
		0, 0, 0, 0x12, // Second element (the same as the first)
		0, 0, 0, 0x15, // Third element
		0, 0, 0, 0x15, // Fourth element (the same as the third)

		// Elements:
		typeString, 0x2, 'a',
		typeNumber, 0x0c, 0x30, 0x2e, 0x31, 0x32, 0x33, 0x34,
	})

	array, ok := j.(Array)
	if !ok {
		t.Errorf("No array loaded")
	}

	if array.Len() != 4 {
		t.Errorf("Elements not loaded")
	}

	elem0 := array.Value(0)
	elem1 := array.Value(1)
	elem2 := array.Value(2)
	elem3 := array.Value(3)
	if s0, ok := elem0.(String); !ok || s0.Value() != "a" {
		t.Errorf("Wrong element 0 loaded")
	}

	if s1, ok := elem1.(String); !ok || s1.Value() != "a" {
		t.Errorf("Wrong element 1 loaded")
	}

	if f0, ok := elem2.(Float); !ok || f0.Value() != json.Number("0.1234") {
		t.Errorf("Wrong element 2 loaded")
	}

	if f1, ok := elem3.(Float); !ok || f1.Value() != json.Number("0.1234") {
		t.Errorf("Wrong element 3 loaded")
	}
}

func TestEmbeddingArray(t *testing.T) {
	// Test nils and booleans are properly embedded into offsets within arrays.

	a := []interface{}{nil, false, true}
	j, err := buildJSON(a)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{
		// Array header:
		typeArray, 0x6,
		0xff, 0xff, 0xff, 0xff, // First element (-typeNil)
		0xff, 0xff, 0xff, 0xfe, // First element (-typeFalse)
		0xff, 0xff, 0xff, 0xfd, // First element (-typeTrue)
	})

	array, ok := j.(Array)
	if !ok {
		t.Errorf("No array loaded")
	}

	if array.Len() != 3 {
		t.Errorf("Elements not loaded")
	}

	elem0 := array.Value(0)
	elem1 := array.Value(1)
	elem2 := array.Value(2)
	if _, ok := elem0.(Null); !ok {
		t.Errorf("Wrong element 0 loaded")
	}

	if e1, ok := elem1.(Bool); !ok || e1.Value() {
		t.Errorf("Wrong element 1 loaded")
	}

	if e2, ok := elem2.(Bool); !ok || !e2.Value() {
		t.Errorf("Wrong element 1 loaded")
	}
}

func TestEmbeddingObject(t *testing.T) {
	// Test nils and booleans are properly embedded into offsets within objects.

	a := []interface{}{
		map[string]interface{}{
			"a": nil,
			"b": false,
			"c": true,
		},
		map[string]interface{}{
			"a": nil,
			"b": false,
			"c": true,
		},
	}
	j, err := buildJSON(a)
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{
		// Array header:
		typeArray, 0x4,

		// Element offsets:
		0, 0, 0, 0xa,
		0, 0, 0, 0x2a,

		// First object:
		typeObjectFull, 0x6,

		// Name offsets:
		0, 0, 0, 0x24, 0, 0, 0, 0x26, 0, 0, 0, 0x28,

		// Value offsets:
		0xff, 0xff, 0xff, 0xff, // First element (-typeNil)
		0xff, 0xff, 0xff, 0xfe, // First element (-typeFalse)
		0xff, 0xff, 0xff, 0xfd, // First element (-typeTrue)

		// Names:
		0x2, 'a', 0x2, 'b', 0x2, 'c',

		// No values.

		// Second object:
		typeObjectThin,

		// Full object offset:
		0, 0, 0, 0xa,

		// Value offsets:
		0xff, 0xff, 0xff, 0xff, // First element (-typeNil)
		0xff, 0xff, 0xff, 0xfe, // First element (-typeFalse)
		0xff, 0xff, 0xff, 0xfd, // First element (-typeTrue)
	})

	array, ok := j.(Array)
	if !ok {
		t.Errorf("No array loaded")
	}

	if array.Len() != 2 {
		t.Errorf("Elements not loaded")
	}

	elem0 := array.Value(0)
	elem1 := array.Value(1)

	if obj, ok := elem0.(Object); !ok {
		t.Errorf("Element 0 incorrect")
	} else {
		if obj.Len() != 3 {
			t.Errorf("Element 0 incorrect")
		}

		valueA := obj.Value("a")
		if _, ok := valueA.(Null); !ok {
			t.Errorf("Value incorrect")
		}

		valueB := obj.Value("b")
		if b, ok := valueB.(Bool); !ok || b.Value() {
			t.Errorf("Value incorrect")
		}

		valueC := obj.Value("c")
		if b, ok := valueC.(Bool); !ok || !b.Value() {
			t.Errorf("Value incorrect")
		}
	}

	if obj, ok := elem1.(Object); !ok {
		t.Errorf("Element 0 incorrect")
	} else {
		if obj.Len() != 3 {
			t.Errorf("Element 0 incorrect")
		}

		valueA := obj.Value("a")
		if _, ok := valueA.(Null); !ok {
			t.Errorf("Value incorrect")
		}

		valueB := obj.Value("b")
		if b, ok := valueB.(Bool); !ok || b.Value() {
			t.Errorf("Value incorrect")
		}

		valueC := obj.Value("c")
		if b, ok := valueC.(Bool); !ok || !b.Value() {
			t.Errorf("Value incorrect")
		}
	}
}

func TestNewWriteTo(t *testing.T) {
	str := "string"
	var empty interface{}
	var i interface{} = str
	//var bigint *big.Int
	for n, test := range []interface{}{
		// Golang JSON native types
		testBuildJSON(`null`),
		testBuildJSON(`1234`),
		testBuildJSON(`1234.1`),
		testBuildJSON(`"foo"`),
		testBuildJSON(`false`),
		testBuildJSON(`true`),
		testBuildJSON(`[]`),
		testBuildJSON(`["a"]`),
		testBuildJSON(`["a","b"]`),
		testBuildJSON(`{}`),
		testBuildJSON(`{"a":"b"}`),
		testBuildJSON(`{"a":"b","c":"d"}`),
		// Golang primitive types
		empty,
		nil,
		i,
		&str,
		true,
		false,
		str,
		[]string(nil),
		[]string{},
		[]string{"foo", "bar"},
		[2]string{"foo", "bar"},
		[]interface{}(nil),
		[]interface{}{},
		[]interface{}{"foo", "bar"},
		[2]interface{}{"foo", "bar"},
		int(1),
		int16(16),
		int32(32),
		int64(64),
		uint(1),
		uint16(16),
		uint32(32),
		uint64(64),
		float32(32.32),
		float64(64.64),
		map[string]string(nil),
		map[string]string{},
		map[string]string{"foo": "bar"},
		map[string]interface{}(nil),
		map[string]interface{}{},
		map[string]interface{}{"foo": "bar"},
		time.Now(), // time.Time has MarshalJSON interface.
		(*textMarshaler)(nil),
		textMarshaler{},
		textMarshaler2{},
		&textMarshaler2{},
		struct {
			A1    []interface{}
			A2    []interface{}
			A3    []interface{}
			A4    [2]string
			B1    bool `json:"Bool1"`
			B2    bool `json:"Bool2,omitempty"`
			F1    float32
			F2    float64
			I1    interface{}
			I2    interface{}
			I3    *interface{}
			Int   int
			Int16 int16
			Int32 int32
			Int64 int64
			Int8  int8
			M1    map[string]string
			M2    map[string]string
			M3    map[string]string
			M4    map[int]string
			M5    map[uint]string
			//M6     map[*big.Int]string // *big.Int implements MarshalJSON interface.
			M7     map[textMarshaler2]string
			S1     string
			S2     *string
			S3     *string
			T1     *textMarshaler
			T2     textMarshaler
			T3     textMarshaler2
			T4     *textMarshaler2
			UInt   uint
			UInt16 uint16
			UInt32 uint32
			UInt64 uint64
			UInt8  uint8
		}{
			// A1 has null value
			A2: []interface{}{},
			A3: []interface{}{"bar", "foo"},
			A4: [2]string{"foo", "bar"},
			B1: true,
			B2: false,
			F1: 0.5,
			F2: -0.6,
			// I1 has null value
			I2:    "interface",
			I3:    &i,
			Int16: 16,
			Int32: -32,
			Int64: 64,
			Int8:  -8,
			Int:   1,
			// M1 has null value
			M2: map[string]string{},
			M3: map[string]string{"foo": "bar"},
			M4: map[int]string{1: "bar"},
			M5: map[uint]string{1: "bar"},
			//M6: map[*big.Int]string{big.NewInt(1): "bar", bigint: "big"},
			M7: map[textMarshaler2]string{{}: "bar"},
			S1: "string",
			S2: new(string),
			S3: &str,
			// T1 has null value, will marshal to "<nil>", not JSON nil, if TextMarshaler invoked correctly.
			T4:     &textMarshaler2{},
			UInt16: 16,
			UInt32: 32,
			UInt64: 64,
			UInt8:  8,
			UInt:   1,
		},
	} {
		t.Run(fmt.Sprintf("%d", n), func(t *testing.T) {
			j, err := New(test)
			if err != nil {
				t.Fatalf("construct failure: %s", err)
			}

			var buf bytes.Buffer
			encoder := json.NewEncoder(&buf)
			encoder.SetEscapeHTML(false)
			encoder.Encode(test)
			expectedSerialization := buf.Bytes()[0 : len(buf.Bytes())-1] // Strip the newline added by encoder.

			// Test the non-binary serialization to JSON.

			b := new(bytes.Buffer)
			if n, err := j.WriteTo(b); err != nil {
				t.Errorf("write failure: %s", err)
			} else if n != int64(len(expectedSerialization)) {
				t.Errorf("write length mismatch: %s (%d) and %s (%d)", string(expectedSerialization), len(expectedSerialization), b.String(), n)
			} else if !bytes.Equal(b.Bytes(), expectedSerialization) {
				t.Errorf("incorrect serialization: %v vs %v", b.String(), string(expectedSerialization))
			}

			//  Test the binary serialization to JSON.

			b = new(bytes.Buffer)
			content, _ := getContent(t, string(expectedSerialization))
			if n, err := newFile(content, 0).WriteTo(b); err != nil {
				t.Errorf("write failure: %s", err)
			} else if n != int64(len(expectedSerialization)) {
				t.Errorf("write length mismatch: %s (%d) and %s (%d)", string(expectedSerialization), len(expectedSerialization), b.String(), n)
			} else if !bytes.Equal(b.Bytes(), expectedSerialization) {
				t.Errorf("incorrect serialization: %v", test)
			}
		})
	}
}

type textMarshaler struct{}

func (t *textMarshaler) MarshalText() (text []byte, err error) {
	if t == nil {
		return []byte("<nil>"), nil
	}

	return []byte("<non-nil>"), nil
}

type textMarshaler2 struct{}

func (t textMarshaler2) MarshalText() (text []byte, err error) {
	return []byte("<non-nil>"), nil
}

func TestObject(t *testing.T) {
	x := map[string]File{
		"a": NewString("a"),
		"b": NewString("b"),
		"d": NewString("d"),
		"e": NewString("e"),
	}

	// Constructor and names

	o := NewObject(x)
	if names := o.Names(); !reflect.DeepEqual(names, []string{"a", "b", "d", "e"}) {
		t.Errorf("names don't match")
	}

	// Set and Value

	o.Set("c", NewString("c"))

	if names := o.Names(); !reflect.DeepEqual(names, []string{"a", "b", "c", "d", "e"}) {
		t.Errorf("names don't match: %v", names)
	}

	if v := o.Value("b"); v == nil {
		t.Errorf("not found")
	} else if s, ok := v.(String); !ok {
		t.Errorf("not string")
	} else if s != NewString("b") {
		t.Errorf("not b")
	}

	// Len, Iterate, SetIdx and RemoveIdx

	o.SetIdx(4, NewString("f"))
	o.RemoveIdx(3)

	if names := o.Names(); !reflect.DeepEqual(names, []string{"a", "b", "c", "e"}) {
		t.Errorf("names don't match: %v", names)
	}

	var values []Json
	for i := 0; i < o.Len(); i++ {
		values = append(values, o.Iterate(i))
	}

	if !reflect.DeepEqual(values, []Json{NewString("a"), NewString("b"), NewString("c"), NewString("f")}) {
		t.Errorf("values don't match: %v", values)
	}
}

func buildJSON(data interface{}) (Json, error) {
	cache := newEncodingCache()
	buffer := new(bytes.Buffer)

	_, err := serialize(data, cache, buffer, 0)
	if err != nil {
		return nil, err
	}

	v := buffer.Bytes()
	return newFile(newSnapshotReader(utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(v))), 0).(Json), nil
}

func validateSerialization(t *testing.T, j File, valid []byte) {
	buffer := new(bytes.Buffer)

	x := j.Contents()
	_, err := serialize(x, newEncodingCache(), buffer, 0)
	if err != nil {
		t.Errorf("Unable to serialize: %v", err)
	} else if !bytes.Equal(buffer.Bytes(), valid) {
		t.Errorf("\nSerialization:\n%s\ncorrect:\n%s", hex.Dump(buffer.Bytes()), hex.Dump(valid))
	}
}

func TestAST(t *testing.T) {
	tests := []struct {
		note  string
		value interface{}
	}{
		{
			value: nil,
		},
		{
			value: true,
		},
		{
			value: json.Number("1.234"),
		},
		{
			value: "string",
		},
		{
			value: []interface{}{"a", "b"},
		},
		{
			value: map[string]interface{}{"a": "b"},
		},
		{
			value: map[string]interface{}{
				"a": []interface{}{
					map[string]interface{}{
						"foo": "bar",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.note, func(t *testing.T) {
			rtt, err := ast.ValueToInterface(MustNew(test.value).AST(), nil)
			if err != nil {
				t.Errorf("error: %s", err.Error())
			}
			if !reflect.DeepEqual(rtt, test.value) {
				t.Errorf("not equal")
			}
		})
	}
}
