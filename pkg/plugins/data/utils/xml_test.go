package utils

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseXML(t *testing.T) {
	for _, tt := range []struct {
		name     string
		data     string
		expected any
		err      string
	}{
		{
			name: "valid xml",
			data: `<foo data="42">bar</foo>`,
			expected: map[string]any{
				"foo": map[string]any{
					"#text": "bar",
					"@data": "42",
				},
			},
		},
		{
			name: "json should fail",
			data: `{"foo": "bar"}`,
			err:  "data at the root level is invalid",
		},
		{
			name: "yaml should fail",
			data: `foo: bar`,
			err:  "data at the root level is invalid",
		},
		{
			name: "quoted string",
			data: `"foo"`,
			err:  "data at the root level is invalid",
		},
		{
			name: "number",
			data: `42`,
			err:  "data at the root level is invalid",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ParseXML(strings.NewReader(tt.data))
			t.Logf("actual: %+v", actual)
			if tt.err != "" {
				if err == nil {
					t.Fatalf("expected error %q, but got nil", tt.err)
				}
				if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.err)
				}
			}
			if !reflect.DeepEqual(tt.expected, actual) {
				t.Fatalf("expected: %+v\n actual: %+v", tt.expected, actual)
			}
		})
	}
}
