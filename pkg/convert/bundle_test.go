package convert

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

func TestConvert(t *testing.T) {
	b := bundle.Bundle{
		Data: map[string]interface{}{
			"foo": map[string]interface{}{
				"bar": []interface{}{json.Number("1"), json.Number("2"), json.Number("3")},
				"baz": true,
				"qux": "hello",
			},
		},
		Modules: []bundle.ModuleFile{
			{
				URL:    "/foo/corge/corge.rego",
				Path:   "/foo/corge/corge.rego",
				Parsed: ast.MustParseModule(`package foo.corge`),
				Raw:    []byte("package foo.corge\n"),
			},
		},
		Manifest: bundle.Manifest{
			Roots:    &[]string{""},
			Revision: "quickbrownfaux",
			Metadata: map[string]interface{}{"version": "v1", "hello": "world"},
		},
	}

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
	ofile, err := os.OpenFile(outFile, os.O_RDONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	defer ofile.Close()

	b2, err := bundle.NewReader(ofile).WithLazyLoadingMode(true).Read()
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(b.Manifest, b2.Manifest); diff != "" {
		t.Errorf("Diff (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(b.Modules, b2.Modules); diff != "" {
		t.Errorf("Diff (-want +got):\n%s", diff)
	}

	for _, v := range b2.Raw {
		if v.Path == "/data.json" {
			if len(v.Value) == 0 {
				t.Error("Missing bjson value")
			}
			res, err := bjson.NewFromBinary(v.Value)
			if err != nil {
				t.Errorf("bjson decode failed: %v, %#v", err, v.Value)
			}
			if diff := cmp.Diff(b.Data, res.JSON()); diff != "" {
				t.Errorf("Diff (-want +got):\n%s", diff)
			}
			return
		}
	}
	t.Errorf("No data.json found: %#v", b2)
}
