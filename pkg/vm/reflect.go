package vm

import (
	"reflect"
	"time"

	"github.com/mitchellh/mapstructure"
)

func toNative(f any) (any, error) {
	var res any
	switch reflect.TypeOf(f).Kind() {
	case reflect.Map:
		res = map[string]any{}
	case reflect.Slice:
		res = []any{}
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName: "json",
		Result:  &res,
		DecodeHook: mapstructure.DecodeHookFunc(func(f reflect.Type, t reflect.Type, data any) (any, error) {
			// if we encounter a `time.Time` struct anywhere, it
			// is converted to a string timestamp
			if f != reflect.TypeOf(time.Time{}) {
				return data, nil
			}
			bs, err := data.(time.Time).MarshalText()
			return string(bs), err
		}),
	})
	if err != nil {
		return nil, err
	}

	return res, decoder.Decode(f)
}
