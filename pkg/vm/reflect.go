package vm

import (
	"reflect"

	"github.com/go-viper/mapstructure/v2"
)

// toNative is called via FromInterface's default case: it's a bit of a
// last resort. For example, FromInterface knows how to deal with []any,
// but not how to deal with...
func toNative(f any) (any, error) {
	// NOTE(sr): mapstructure needs to know what to convert to: we tell
	// it by creating `res` (any) of the target type.
	res := valueFromKind(f)

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName:              "json",
		IgnoreUntaggedFields: true,
		Result:               &res,
	})
	if err != nil {
		return nil, err
	}

	return res, decoder.Decode(f)
}

func valueFromKind(f any) (res any) {
	switch reflect.TypeOf(f).Kind() {
	case reflect.Map, reflect.Struct:
		res = map[string]any{}
	case reflect.String: // custom types of string (e.g. ast.Var)
		res = ""
	case reflect.Slice, reflect.Array:
		res = []any{}
	case reflect.Pointer: // pointers to structs, slices, arrays, maps
		res = valueFromKind(reflect.ValueOf(f).Elem())
	}
	return
}
