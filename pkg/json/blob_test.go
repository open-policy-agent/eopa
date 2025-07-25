package json

import (
	"bytes"
	"testing"

	"github.com/open-policy-agent/eopa/pkg/json/internal/utils"
)

func TestBlob(t *testing.T) {
	// Empty blob.

	j, err := buildBinary([]byte{})
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeBinaryFull, 0x0})
	if s, ok := j.(Blob); !ok || len(s.Value()) != 0 {
		t.Errorf("Incorrect value")
	}

	// Non-empty.

	j, err = buildBinary([]byte("foo"))
	if err != nil {
		t.Fatalf("Construction error: %v", err)
	}

	validateSerialization(t, j, []byte{typeBinaryFull, 0x6, 'f', 'o', 'o'})
	if s, ok := j.(Blob); !ok || !bytes.Equal(s.Value(), []byte("foo")) {
		t.Errorf("Incorrect value")
	}
}

func buildBinary(data interface{}) (File, error) {
	cache := newEncodingCache()
	buffer := new(bytes.Buffer)

	_, err := serialize(data, cache, buffer, 0)
	if err != nil {
		return nil, err
	}

	v := buffer.Bytes()
	return newFile(newSnapshotReader(utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(v))), 0), nil
}
