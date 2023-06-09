package bundle

import (
	"github.com/open-policy-agent/opa/loader/extension"
)

func init() {
	// file system json loader (load json or bjson)
	extension.RegisterExtension(".json", loadJSON)
}

func loadJSON(bs []byte) (interface{}, error) {
	r, err := BjsonFromBinary(bs)
	if err != nil {
		return nil, err
	}
	return r.JSON(), nil
}
