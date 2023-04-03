package json

import (
	"reflect"
	"strings"
	"unsafe"
)

// PathSegments returns the path segments of the path.
func PathSegments(path string) []string {
	path = strings.TrimPrefix(path, "/")

	var segs []string
	if path != "" {
		segs = strings.Split(path, "/")
	}

	return segs
}

func bytesToString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func readOnlyStringBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}

func fieldByIndex(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}
