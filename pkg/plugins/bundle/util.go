package bundle

import (
	"bytes"

	bjson "github.com/styrainc/load-private/pkg/json"
)

// BjsonFromBinary returns a bjson.Json instance (on success) that was read from
// the passed byte slice. If the byte slice is a vanilla bundle's content, it'll
// be converted.
func BjsonFromBinary(bs []byte) (bjson.Json, error) {
	if bs[0] < 32 { // non-printable (BJSON)
		return bjson.NewFromBinary(bs)
	}

	// JSON (ascii)
	return bjson.NewDecoder(bytes.NewReader(bs)).Decode()
}
