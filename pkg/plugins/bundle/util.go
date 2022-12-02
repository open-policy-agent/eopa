package bundle

import (
	"bytes"

	"github.com/open-policy-agent/opa/util"

	bjson "github.com/styrainc/load/pkg/json"
)

// BjsonFromBinary returns a bjson.Json instance (on success) that was read from
// the passed byte slice. If the byte slice is a vanilla bundle's content, it'll
// be converted.
func BjsonFromBinary(bs []byte) (bjson.Json, error) {
	if bs[0] == byte('{') {
		var v interface{}
		err := util.NewJSONDecoder(bytes.NewReader(bs)).Decode(&v)
		if err != nil {
			return nil, err
		}
		return bjson.New(v)
	}
	return bjson.NewFromBinary(bs)
}
