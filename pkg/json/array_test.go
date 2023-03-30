package json

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/util"
)

func TestArraySizes(t *testing.T) {
	tests := []struct {
		json string
	}{
		{`[]`},
		{`[0]`},
		{`[0,1]`},
		{`[0,1,2]`},
		{`[0,1,2,3]`},
		{`[0,1,2,3,4]`},
		{`[0,1,2,3,4,5]`},
		{`[0,1,2,3,4,5,6]`},
		{`[0,1,2,3,4,5,6,7]`},
		{`[0,1,2,3,4,5,6,7,8]`},
		{`[0,1,2,3,4,5,6,7,8,9]`},
		{`[0,1,2,3,4,5,6,7,8,9,10]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31]`},
		{`[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32]`},
	}

	if len(tests) <= maxCompactArray+1 {
		t.Fatal("non-compact array not tested")
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			var native interface{}
			if err := util.NewJSONDecoder(bytes.NewBufferString(test.json)).Decode(&native); err != nil {
				t.Fatal(err)
			}

			// Decoding, Clone

			doc, err := NewDecoder(bytes.NewBufferString(test.json)).Decode()
			if err != nil {
				t.Fatal(err)
			}

			if doc.String() != test.json {
				t.Errorf("incorrect json marshaling: %s", doc.String())
			}

			var nativeArr = native.([]interface{})
			var arr = doc.(Array)

			arr = arr.Clone(true).(Array)
			if !reflect.DeepEqual(arr.JSON(), nativeArr) {
				t.Error("broken decoder or clone")
			}

			// Compare, Len, AST

			if arr.Compare(MustNew(nativeArr)) != 0 {
				t.Error("broken compare")
			}

			if len(nativeArr) != arr.Len() {
				t.Error("broken len")
			}

			if ast.MustInterfaceToValue(nativeArr).Compare(arr.AST()) != 0 {
				t.Error("broken ast conversion")
			}

			// Len, Iterate, Value, Extract

			for i := 0; i < arr.Len(); i++ {
				if !reflect.DeepEqual(NewFloatInt(int64(i)), arr.Iterate(i)) {
					t.Error("broken iteration")
				}

				if !reflect.DeepEqual(NewFloatInt(int64(i)), arr.Value(i)) {
					t.Error("broken value")
				}

				e, err := arr.Extract(fmt.Sprintf("/%d", i))
				if err != nil || !reflect.DeepEqual(NewFloatInt(int64(i)), e) {
					t.Error("broken extract")
				}

			}

			// Find

			var found Json
			p, _ := ParsePath("$")
			arr.Find(p, func(v Json) {
				found = v
			})
			if found.Compare(arr) != 0 {
				t.Error("broken find")
			}

			// Append, AppendSingle

			arr = arr.Append(NewString("a"), NewString("b"))
			if a, ok := arr.AppendSingle(NewString("c")); ok {
				arr = a
			}

			if !reflect.DeepEqual(arr.JSON(), append(nativeArr, []interface{}{"a", "b", "c"}...)) {
				t.Error("broken append or append single")
			}

			// SetIdx, RemoveIdx

			arr = arr.SetIdx(arr.Len()-1, NewString("c updated")).(Array)
			if !reflect.DeepEqual(arr.JSON(), append(nativeArr, []interface{}{"a", "b", "c updated"}...)) {
				t.Error("broken set idx")
			}

			arr = arr.RemoveIdx(arr.Len() - 1).(Array)
			arr = arr.RemoveIdx(arr.Len() - 1).(Array)
			arr = arr.RemoveIdx(arr.Len() - 1).(Array)
			if !reflect.DeepEqual(arr.JSON(), nativeArr) {
				t.Error("broken remove idx")
			}

			// Walk

			var walker testArrayWalker
			arr.Walk(NewDecodingState(), &walker)
			if walker.decoded != test.json {
				t.Error("broken walk")
			}
		})
	}
}

type testArrayWalker struct {
	Walker
	decoded string
}

func (t *testArrayWalker) StartArray(*DecodingState)      { t.decoded += "[" }
func (t *testArrayWalker) EndArray(*DecodingState, Array) { t.decoded += "]" }

func (t *testArrayWalker) Number(_ *DecodingState, f Float) {
	if len(t.decoded) > 1 {
		t.decoded += ","
	}
	t.decoded += f.String()
}

func (t *testArrayWalker) String(_ *DecodingState, s String) {
	if len(t.decoded) > 1 {
		t.decoded += ","
	}
	t.decoded += strconv.Quote(string(s))
}
