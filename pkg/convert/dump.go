package convert

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"

	bjson "github.com/styrainc/load-private/pkg/json"
)

func DumpData(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	tb := tar.NewReader(gz)
	for {
		header, err := tb.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if header.Typeflag == tar.TypeReg &&
			header.Name == "/data.json" || header.Name == "/data.bjson" {
			if _, err := io.Copy(&buf, tb); err != nil {
				return err
			}
		}
	}

	data, err := bjson.NewFromBinary(buf.Bytes())
	if err != nil {
		return err
	}
	fmt.Print(data)
	return nil
}
