package bundle

import (
	"bytes"
	"fmt"
	"io"

	"sigs.k8s.io/yaml"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
)

// BjsonFromBinary returns a bjson.Json instance (on success) that was read from
// the passed byte slice. If the byte slice is a vanilla bundle's content, it'll
// be converted.
func BjsonFromBinary(bs []byte) (b bjson.Json, err error) {
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok {
				err = fmt.Errorf(s)
			} else {
				err = fmt.Errorf("bjson decoding error")
			}
		}
	}()

	if len(bs) == 0 {
		return nil, io.EOF
	}

	if bjson.IsBJson(bs) { // tab (bjson.typeObjectThin)
		return bjson.NewFromBinary(bs)
	}

	if bjson.IsJSON(bs) {
		return bjson.NewDecoder(bytes.NewReader(bs)).Decode()
	}

	// lastly, try yaml
	nbs, err := yaml.YAMLToJSON(bs)
	if err != nil {
		return nil, err
	}
	return bjson.NewDecoder(bytes.NewReader(nbs)).Decode()
}
