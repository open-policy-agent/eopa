// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package merge

import (
	"strings"
	"testing"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

func TestMergeDocs(t *testing.T) {

	tests := []struct {
		a  string
		b  string
		c  string
		ok bool
	}{
		{`{"x": 1, "y": 2}`, `{"z": 3}`, `{"x": 1, "y": 2, "z": 3}`, true},
		{`{"x": {"y": 2}}`, `{"z": 3, "x": {"q": 4}}`, `{"x": {"y": 2, "q": 4}, "z": 3}`, true},
		{`{"x": 1}`, `{"x": 1}`, "", false},
		{`{"x": {"y": [{"z": 2}]}}`, `{"x": {"y": [{"z": 3}]}}`, "", false},
	}

	for _, tc := range tests {

		aJson, err := bjson.NewDecoder(strings.NewReader(tc.a)).Decode()
		if err != nil {
			panic(err)
		}

		bJson, err := bjson.NewDecoder(strings.NewReader(tc.b)).Decode()
		if err != nil {
			panic(err)
		}

		a := aJson.(bjson.Object)
		b := bJson.(bjson.Object)

		if len(tc.c) == 0 {

			c, ok := InterfaceMaps(a, b)
			if ok {
				t.Errorf("Expected merge(%v,%v) == false but got: %v", a, b, c)
			}

		} else {

			expJson, err := bjson.NewDecoder(strings.NewReader(tc.c)).Decode()
			if err != nil {
				panic(err)
			}
			expected := expJson.(bjson.Object)

			c, ok := InterfaceMaps(a, b)
			if !ok || expected.Compare(c) != 0 {
				t.Errorf("Expected merge(%v, %v) == %v but got: %v (ok: %v)", a, b, expected, c, ok)
			}
		}
	}
}
