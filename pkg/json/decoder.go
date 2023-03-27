package json

import (
	"errors"
	"fmt"
	"io"

	jsoniter "github.com/json-iterator/go"
)

var config = jsoniter.Config{
	EscapeHTML:             true,
	SortMapKeys:            true,
	ValidateJsonRawMessage: true,
	UseNumber:              true,
}.Froze()

// Decoder mimics the golang JSON decoder: reading a JSON object out of a byte stream. For simplicity, the Decode() implementation returns the constructed object, instead of taking a pointer as a parameter as the
// standard package.
type Decoder struct {
	strings map[string]string // for string interning.
	keys    map[interface{}]*[]string
	iter    *jsoniter.Iterator
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{strings: make(map[string]string), keys: make(map[interface{}]*[]string), iter: jsoniter.Parse(config, r, 512)}
}

func (d *Decoder) error() error {
	if err := d.iter.Error; err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	return nil
}

func (d *Decoder) Decode() (Json, error) {
	valueType := d.iter.WhatIsNext()
	if err := d.iter.Error; err != nil {
		return nil, err
	}

	switch valueType {
	case jsoniter.StringValue:
		v := d.iter.ReadString()
		if err := d.error(); err != nil {
			return nil, err
		}

		return NewString(d.intern(v)), nil

	case jsoniter.NumberValue:
		v := d.iter.ReadNumber()
		if err := d.error(); err != nil {
			return nil, err
		}

		return NewFloat(v), nil

	case jsoniter.NilValue:
		d.iter.Read()
		if err := d.error(); err != nil {
			return nil, err
		}

		return NewNull(), nil

	case jsoniter.BoolValue:
		v := d.iter.ReadBool()
		if err := d.error(); err != nil {
			return nil, err
		}

		return NewBool(v), nil

	case jsoniter.ArrayValue:
		var err error
		var arr []File
		d.iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
			v, e := d.Decode()
			if e != nil {
				err = e
				return false
			}

			arr = append(arr, v)
			return true
		})
		if err != nil {
			return nil, err
		}
		if err := d.error(); err != nil {
			return nil, err
		}

		trimmed := make([]File, len(arr))
		copy(trimmed, arr)
		return NewArray2(trimmed), nil

	case jsoniter.ObjectValue:
		properties := make(map[string]File)
		var err error
		d.iter.ReadMapCB(func(iter *jsoniter.Iterator, field string) bool {
			v, e := d.Decode()
			if e != nil {
				err = e
				return false
			}

			properties[d.intern(field)] = v
			return true
		})
		if err != nil {
			return nil, err
		}
		if err := d.error(); err != nil {
			return nil, err
		}

		return NewObject2(properties, d.keys), nil
	}

	return nil, fmt.Errorf("unexpected value type: %v", valueType)
}

func (d *Decoder) intern(v string) string {
	if s, ok := d.strings[v]; ok {
		v = s
	} else {
		d.strings[v] = v
	}

	return v
}
