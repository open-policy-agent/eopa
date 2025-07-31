// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/opa/v1/loader/extension"

	bjson "github.com/open-policy-agent/eopa/pkg/json"
)

func init() {
	// file system json loader (load json or bjson)
	extension.RegisterExtension(".json", loadJSON)

}

func loadJSON(bs []byte, v any) error {
	r, err := BjsonFromBinary(bs)
	if err != nil {
		return err
	}
	switch v := v.(type) {
	case *any:
		*v = r.JSON()
	case *map[string]json.RawMessage:
		if *v == nil {
			*v = map[string]json.RawMessage{}
		}
		o, ok := r.(bjson.Object)
		if !ok {
			return fmt.Errorf("unsupported JSON type %T (target type %T)", r, v)
		}
		for _, n := range o.Names() {
			var err error
			(*v)[n], err = bjson.Marshal(o.Value(n))
			return err
		}
	default:
		return fmt.Errorf("unsupported target type %T", v)
	}
	return nil
}
