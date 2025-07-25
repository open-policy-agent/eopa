package convert

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"

	bjson "github.com/open-policy-agent/eopa/pkg/json"
)

func TestConvert(t *testing.T) {
	type testcase struct {
		note   string
		bundle bundle.Bundle
	}
	testcases := []testcase{
		{
			note: "normal snapshot bundle",
			bundle: bundle.Bundle{
				Data: map[string]interface{}{
					"foo": map[string]interface{}{
						"bar": []interface{}{json.Number("1"), json.Number("2"), json.Number("3")},
						"baz": true,
						"qux": "hello",
					},
				},
				Modules: []bundle.ModuleFile{
					{
						URL:          "/foo/corge/corge.rego",
						Path:         "/foo/corge/corge.rego",
						RelativePath: "/foo/corge/corge.rego",
						Parsed:       ast.MustParseModule(`package foo.corge`),
						Raw:          []byte("package foo.corge\n"),
					},
				},
				Manifest: bundle.Manifest{
					Roots:    &[]string{""},
					Revision: "quickbrownfaux",
					Metadata: map[string]interface{}{"version": "v1", "hello": "world"},
				},
			},
		},
		{
			note: "delta bundle",
			bundle: bundle.Bundle{
				Data: map[string]interface{}{},
				Patch: bundle.Patch{Data: []bundle.PatchOperation{
					{Op: "upsert", Path: "/a/b/d", Value: "foo"},
					{Op: "remove", Path: "/a/b/c"},
				}},
				Manifest: bundle.Manifest{
					Roots:    &[]string{""},
					Revision: "quickbrownfaux",
					Metadata: map[string]interface{}{"version": "v1", "hello": "world"},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.note, func(t *testing.T) {
			b := tc.bundle
			dir := t.TempDir()

			inFile := dir + "/in.tar.gz"
			outFile := dir + "/out.tar.gz"

			// create source file
			ifile, err := os.Create(inFile)
			if err != nil {
				t.Fatal(err)
			}

			if err := bundle.NewWriter(ifile).Write(b); err != nil {
				t.Fatal(err)
			}
			ifile.Close()

			// invoke the convert function
			if err := BundleFile(inFile, outFile); err != nil {
				t.Fatal(err)
			}

			// validate the result
			ofile, err := os.OpenFile(outFile, os.O_RDONLY, 0o755)
			if err != nil {
				t.Fatal(err)
			}
			defer ofile.Close()

			b2, err := bundle.NewReader(ofile).WithLazyLoadingMode(true).Read()
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(b.Manifest, b2.Manifest); diff != "" {
				t.Errorf("[%s] Diff (-want +got):\n%s", tc.note, diff)
			}

			if diff := cmp.Diff(b.Modules, b2.Modules); diff != "" {
				t.Errorf("[%s] Diff (-want +got):\n%s", tc.note, diff)
			}

			switch b2.Type() {
			case bundle.DeltaBundleType:
				// TODO(philip): For some reason, the b2.Raw field *is not populated* for delta bundle types?
				if len(b2.Patch.Data) == 0 {
					t.Errorf("[%s] Missing patches for delta bundle", tc.note)
				}
				return
			case bundle.SnapshotBundleType:
				for _, v := range b2.Raw {
					if v.Path == "/data.json" {
						if len(v.Value) == 0 {
							t.Errorf("[%s] Missing bjson value", tc.note)
						}
						res, err := bjson.NewFromBinary(v.Value)
						if err != nil {
							t.Errorf("[%s] bjson decode failed: %v, %#v", tc.note, err, v.Value)
						}
						if diff := cmp.Diff(b.Data, res.JSON()); diff != "" {
							t.Errorf("[%s] Diff (-want +got):\n%s", tc.note, diff)
						}
						return
					}
				}
				t.Errorf("[%s] No data.json found: %#v", tc.note, b2)
			default:
				t.Errorf("[%s] Unknown bundle type %#v", tc.note, b2)
			}
		})
	}
}
