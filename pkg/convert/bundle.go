package convert

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"

	"github.com/open-policy-agent/opa/v1/bundle"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

const (
	dataFile  = "data.json"
	patchFile = "patch.json" // TODO(sr): bjson this?
)

// BjsonBundle is OPA's Bundle with bjson.Object instead of map[string]interface{}
// and no wasm or plan artifacts; or signatures.
type BjsonBundle struct {
	Manifest bundle.Manifest
	Data     bjson.Object
	Modules  []bundle.ModuleFile
	Patch    bundle.Patch
	Etag     string
}

// BundleFile parses `in` as a bundle and writes a binary bundle to `out`.
func BundleFile(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	path := "/" // TODO(sr): really?
	tl := bundle.NewTarballLoaderWithBaseURL(bytes.NewReader(bs), path)
	br := bundle.NewCustomReader(tl).WithSkipBundleVerification(true)
	b, err := br.Read()
	if err != nil {
		return err
	}
	if b.Signatures.Signatures != nil {
		log.Printf("warn: signatures get dropped in bundle conversion")
	}
	bb := BjsonBundle{
		Manifest: b.Manifest,
		Modules:  b.Modules,
		Patch:    b.Patch, // TODO(sr): Anything to do for these?
	}
	if b.Data != nil {
		bb.Data = bjson.MustNew(b.Data).(bjson.Object)
	} else {
		bb.Data = bjson.NewObject(map[string]bjson.File{})
	}

	outFile, err := os.Create(out)
	if err != nil {
		return err
	}
	defer outFile.Close()
	return write(outFile, bb)
}

// writeFile adds a file header with content to the given tar writer
// From github.com/open-policy-agent/opa/internal/file/archive
func writeFile(tw *tar.Writer, path string, bs []byte) error {
	hdr := &tar.Header{
		Name:     "/" + strings.TrimLeft(path, "/"),
		Mode:     0o600,
		Typeflag: tar.TypeReg,
		Size:     int64(len(bs)),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}

	_, err := tw.Write(bs)
	return err
}

// write writes the bundle to passed io.Writer
func write(w io.Writer, b BjsonBundle) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	if len(b.Patch.Data) == 0 { // snapshot bundle
		buf, err := bjson.Marshal(b.Data)
		if err != nil {
			return err
		}

		if err := writeFile(tw, dataFile, buf); err != nil {
			return err
		}

		for _, module := range b.Modules {
			path := module.URL
			if err := writeFile(tw, path, module.Raw); err != nil {
				return err
			}
		}

	} else {
		if err := writePatch(tw, b); err != nil {
			return err
		}
	}

	if err := writeManifest(tw, b); err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}

	return gw.Close()
}

func writeManifest(tw *tar.Writer, b BjsonBundle) error {
	if b.Manifest.Equal(bundle.Manifest{}) {
		return nil
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(b.Manifest); err != nil {
		return err
	}
	return writeFile(tw, bundle.ManifestExt, buf.Bytes())
}

func writePatch(tw *tar.Writer, b BjsonBundle) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(b.Patch); err != nil {
		return err
	}
	return writeFile(tw, patchFile, buf.Bytes())
}
