// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
)

func TestArrayStringsSizes(t *testing.T) {
	tests := []struct {
		json string
	}{
		{`[]`},
		{`["0"]`},
		{`["0","1"]`},
		{`["0","1","2"]`},
		{`["0","1","2","3"]`},
		{`["0","1","2","3","4"]`},
		{`["0","1","2","3","4","5"]`},
		{`["0","1","2","3","4","5","6"]`},
		{`["0","1","2","3","4","5","6","7"]`},
		{`["0","1","2","3","4","5","6","7","8"]`},
		{`["0","1","2","3","4","5","6","7","8","9"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24","25"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24","25","26"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24","25","26","27"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24","25","26","27","28"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24","25","26","27","28","29"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24","25","26","27","28","29","30"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24","25","26","27","28","29","30","31"]`},
		{`["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20","21","22","23","24","25","26","27","28","29","30","31","32"]`},
	}

	if len(tests) <= maxCompactArray+1 {
		t.Fatal("non-compact array not tested")
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			var native any
			if err := json.NewDecoder(bytes.NewBufferString(test.json)).Decode(&native); err != nil {
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

			var nativeArr = native.([]any)
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
				if !reflect.DeepEqual(NewString(fmt.Sprintf("%d", i)), arr.Iterate(i)) {
					t.Error("broken iteration")
				}

				if !reflect.DeepEqual(NewString(fmt.Sprintf("%d", i)), arr.Value(i)) {
					t.Error("broken value")
				}

				e, err := arr.Extract(fmt.Sprintf("/%d", i))
				if err != nil || !reflect.DeepEqual(NewString(fmt.Sprintf("%d", i)), e) {
					t.Error("broken extract")
				}

			}

			// Append, AppendSingle

			arr = arr.Append(NewString("a"), NewString("b"))
			if a, ok := arr.AppendSingle(NewString("c")); ok {
				arr = a
			}

			if !reflect.DeepEqual(arr.JSON(), append(nativeArr, []any{"a", "b", "c"}...)) {
				t.Error("broken append or append single")
			}

			// SetIdx, RemoveIdx

			arr = arr.SetIdx(arr.Len()-1, NewString("c updated")).(Array)
			if !reflect.DeepEqual(arr.JSON(), append(nativeArr, []any{"a", "b", "c updated"}...)) {
				t.Error("broken set idx")
			}

			arr = arr.RemoveIdx(arr.Len() - 1).(Array)
			arr = arr.RemoveIdx(arr.Len() - 1).(Array)
			arr = arr.RemoveIdx(arr.Len() - 1).(Array)
			if !reflect.DeepEqual(arr.JSON(), nativeArr) {
				t.Error("broken remove idx")
			}
		})
	}
}

// TestArrayStringsAppend stresses the append implementation.
func TestArrayStringsAppend(t *testing.T) {
	arr := newArrayCompactStrings[[1]*String](nil)
	expected := make([]any, 0, 64*2)

	for i := range int64(64) {
		arr = arr.Append(NewString(fmt.Sprintf("%d", i)), NewString(fmt.Sprintf("%d", i*2)))
		expected = append(expected, fmt.Sprintf("%d", i))
		expected = append(expected, fmt.Sprintf("%d", i*2))

		if !reflect.DeepEqual(arr.JSON(), expected) {
			t.Error("broken array append")
		}
	}
}
