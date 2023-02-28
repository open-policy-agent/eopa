package utils

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestInsertFile(t *testing.T) {
	for _, tt := range []struct {
		name          string
		files         map[string]any
		path          []string
		document      any
		expectedFiles map[string]any
	}{
		{
			name:     "empty files with root document",
			files:    map[string]any{},
			path:     []string{"file.txt"},
			document: "foo",
			expectedFiles: map[string]any{
				"file.txt": "foo",
			},
		},
		{
			name: "override",
			files: map[string]any{
				"file.txt": "foo",
			},
			path:     []string{"file.txt"},
			document: "bar",
			expectedFiles: map[string]any{
				"file.txt": "bar",
			},
		},
		{
			name: "nested",
			files: map[string]any{
				"file.txt": "foo",
			},
			path:     []string{"foo", "bar", "file.txt"},
			document: "bar",
			expectedFiles: map[string]any{
				"file.txt": "foo",
				"foo": map[string]any{
					"bar": map[string]any{
						"file.txt": "bar",
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			InsertFile(tt.files, tt.path, tt.document)
			if diff := cmp.Diff(tt.expectedFiles, tt.files); diff != "" {
				t.Fatalf("unexpected diff:\n%s", diff)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
		data     string
		expected any
		err      string
	}{
		{
			name:     "json",
			filename: "file.json",
			data:     `{"foo": "bar"}`,
			expected: map[string]any{
				"foo": "bar",
			},
		},
		{
			name:     "yaml",
			filename: "file.yaml",
			data:     `foo: "bar"`,
			expected: map[string]any{
				"foo": "bar",
			},
		},
		{
			name:     "yml",
			filename: "file.yml",
			data:     `foo: "bar"`,
			expected: map[string]any{
				"foo": "bar",
			},
		},
		{
			name:     "xml",
			filename: "file.xml",
			data:     `<foo>bar</foo>`,
			expected: map[string]any{
				"foo": "bar",
			},
		},
		{
			name:     "xml with attribute",
			filename: "file.xml",
			data:     `<foo number="42">bar</foo>`,
			expected: map[string]any{
				"foo": map[string]any{
					"#text":   "bar",
					"@number": "42",
				},
			},
		},
		{
			name:     "bad xml",
			filename: "file.xml",
			data:     `<foo 42>bar</foo>`,
			err:      "invalid XML name: 42",
		},
		{
			name:     "bad json",
			filename: "file.json",
			data:     `{"foo": "bar"`,
			err:      "did not find expected ',' or '}'",
		},
		{
			name:     "bad yaml",
			filename: "file.yaml",
			data: `
- foo:
  - bar
  xyz
`,
			err: "could not find expected ':'",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.data)
			actual, err := ParseFile(tt.filename, r)
			if tt.err != "" {
				if err == nil {
					t.Fatalf("expected error %q, but got nil with result: %#v", tt.err, actual)
				}
				if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("error %q does not contain %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %q", err)
			}
			if diff := cmp.Diff(tt.expected, actual); diff != "" {
				t.Fatalf("unexpected diff:\n%s", diff)
			}
		})
	}
}
