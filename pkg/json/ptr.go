package json

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	internal "github.com/styrainc/enterprise-opa-private/pkg/json/internal/json"
)

var zeroValue = reflect.Value{}

// NewPointer constructs a JSON pointer from its segments.
func NewPointer(segs []string) string {
	var buffer bytes.Buffer

	if len(segs) == 0 {
		return ""
	}

	for i := range segs {
		buffer.WriteString("/")
		buffer.WriteString(EscapePointerSeg(segs[i]))
	}

	return buffer.String()
}

// ParsePointer parses a JSON pointer, returning its segments.
func ParsePointer(ptr string) ([]string, error) {
	return preparePointer(ptr)
}

// preparePointer parses a pointer string as per RFC 6901.
func preparePointer(ptr string) ([]string, error) {
	if len(ptr) == 0 {
		return nil, nil
	}

	if ptr[0] != '/' {
		return nil, fmt.Errorf("Invalid pointer")
	}

	p := strings.Split(ptr, "/")

	// Resolve the escaped characters
	for i := range p {
		p[i] = UnescapePointerSeg(p[i])
	}

	return p[1:], nil
}

// unescapePointerSeg unescapes a path segment.
func UnescapePointerSeg(ptr string) string {
	ptr = strings.Replace(ptr, "~1", "/", -1)
	return strings.Replace(ptr, "~0", "~", -1)
}

// escapePointerSeg escapes a string to be safe string for a path segment.
func EscapePointerSeg(ptr string) string {
	ptr = strings.Replace(ptr, "~", "~0", -1)
	return strings.Replace(ptr, "/", "~1", -1)
}

// Extract returns a value from an JSON document as per RFC 6901
// pointer string.
//
// The function returns an error if no element found (to separate from
// the case value being a nil). That is, it is up to the caller to
// determine the exact semantics of no element found.
func Extract(json interface{}, ptr string) (interface{}, error) {
	if doc, ok := json.(Json); ok {
		return doc.Extract(ptr)
	}

	result, err := extractImpl(reflect.ValueOf(json), ptr)
	if err != nil {
		return nil, err
	}

	return result.Interface(), nil
}

func extractImpl(json reflect.Value, ptr string) (reflect.Value, error) {
	if ptr == "" {
		return json, nil
	}

	p, err := preparePointer(ptr)
	if err != nil {
		return zeroValue, err
	}

	return extract(json, p)
}

func extract(value reflect.Value, ptr []string) (reflect.Value, error) {
	if kind := value.Kind(); kind == reflect.Interface || kind == reflect.Ptr {
		if len(ptr) == 0 {
			if !value.IsNil() {
				return extract(value.Elem(), ptr)
			}
			return value, nil
		}

		value = value.Elem()
	}

	if len(ptr) == 0 {
		if value.IsValid() {
			return value, nil
		}
	}

	if !value.IsValid() {
		return zeroValue, errors.New("json: path not found")
	}

	switch value.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
		return zeroValue, errors.New("json: path not found")

	case reflect.Interface, reflect.Ptr:
		return extract(value.Elem(), ptr)

	case reflect.Map:
		var v reflect.Value
		switch value.Type().Key().Kind() {
		case reflect.String:
			v = value.MapIndex(reflect.ValueOf(ptr[0]))
		case reflect.Int:
			index, err := strconv.ParseInt(ptr[0], 10, 0)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(int(index)))

		case reflect.Int8:
			index, err := strconv.ParseInt(ptr[0], 10, 8)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(int8(index)))

		case reflect.Int16:
			index, err := strconv.ParseInt(ptr[0], 10, 16)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(int16(index)))

		case reflect.Int32:
			index, err := strconv.ParseInt(ptr[0], 10, 32)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(int32(index)))

		case reflect.Int64:
			index, err := strconv.ParseInt(ptr[0], 10, 64)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(index))

		case reflect.Uint:
			index, err := strconv.ParseUint(ptr[0], 10, 0)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(uint(index)))

		case reflect.Uint8:
			index, err := strconv.ParseUint(ptr[0], 10, 8)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(uint8(index)))

		case reflect.Uint16:
			index, err := strconv.ParseUint(ptr[0], 10, 16)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(uint16(index)))

		case reflect.Uint32:
			index, err := strconv.ParseUint(ptr[0], 10, 32)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(uint32(index)))

		case reflect.Uint64:
			index, err := strconv.ParseUint(ptr[0], 10, 64)
			if err != nil {
				return zeroValue, errors.New("json: path not found")
			}

			v = value.MapIndex(reflect.ValueOf(uint64(index)))

		default:
			if value.Type().Key().Implements(encodingTextMarshalerType) {
				found := false
				iter := value.MapRange()
				for iter.Next() {
					raw, err := iter.Key().Interface().(encoding.TextMarshaler).MarshalText()
					if err != nil {
						return zeroValue, err
					}

					if string(raw) == ptr[0] {
						v = iter.Value()
						found = true
						break
					}
				}

				if !found {
					return zeroValue, errors.New("json: path not found")
				}
			} else {
				return zeroValue, fmt.Errorf("json: unsupported type %v", value.Type())
			}
		}

		return extract(v, ptr[1:])

	case reflect.Array, reflect.Slice:
		index, err := strconv.Atoi(ptr[0])
		if err != nil || index < 0 || index >= value.Len() {
			return zeroValue, errors.New("json: path not found")
		}

		return extract(value.Index(index), ptr[1:])

	case reflect.Struct:
		for _, f := range internal.CachedTypeFields(value.Type()) {
			if f.Name != ptr[0] {
				continue
			}

			fv := fieldByIndex(value, f.Index)
			if !fv.IsValid() || f.OmitEmpty && isEmptyValue(fv) {
				continue
			}

			return extract(fv, ptr[1:])
		}

		return zeroValue, errors.New("json: path not found")

	default:
		return zeroValue, fmt.Errorf("json: path not unsupported type %v", value.Type())
	}
}
