package vm

import (
	"reflect"

	"github.com/mitchellh/mapstructure"
)

func toNative(x any) (any, error) {
	var res any
	switch reflect.TypeOf(x).Kind() {
	case reflect.Map:
		res = map[string]any{}
	case reflect.Slice:
		res = []any{}
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName: "json",
		Result:  &res,
	})
	if err != nil {
		return nil, err
	}

	return res, decoder.Decode(x)
}
