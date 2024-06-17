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

func IsJSON(bs []byte) bool {
	return config.Valid(bs)
}

// Decoder mimics the golang JSON decoder: reading a JSON object out of a byte stream. For simplicity, the Decode() implementation returns the constructed object, instead of taking a pointer as a parameter as the
// standard package.
type Decoder struct {
	strings map[string]*String // for string interning.
	keys    map[interface{}]*[]string
	iter    *jsoniter.Iterator
}

func newDecoder(iter *jsoniter.Iterator) *Decoder {
	return &Decoder{
		strings: make(map[string]*String),
		keys:    make(map[interface{}]*[]string),
		iter:    iter,
	}
}

func NewDecoder(r io.Reader) *Decoder {
	return newDecoder(jsoniter.Parse(config, r, 512))
}

func NewStringDecoder(s string) *Decoder {
	return newDecoder(jsoniter.ParseString(config, s))
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

		return d.intern(v), nil

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
		d.iter.ReadArrayCB(func(*jsoniter.Iterator) bool {
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

		if len(arr) <= maxCompactArray {
			return NewArray(arr, len(arr)), nil
		}

		trimmed := make([]File, len(arr))
		copy(trimmed, arr)
		return NewArray(trimmed, len(trimmed)), nil

	case jsoniter.ObjectValue:
		properties := make(map[string]File)
		var err error
		d.iter.ReadMapCB(func(_ *jsoniter.Iterator, field string) bool {
			v, e := d.Decode()
			if e != nil {
				err = e
				return false
			}

			properties[d.intern(field).Value()] = v
			return true
		})
		if err != nil {
			return nil, err
		}
		if err := d.error(); err != nil {
			return nil, err
		}

		return NewObjectMapCompact(properties, d.keys), nil
	}

	return nil, fmt.Errorf("unexpected value type: %v", valueType)
}

func (d *Decoder) intern(v string) *String {
	return internString(v, d.strings)
}

func internString(v string, strings map[string]*String) *String {
	if s, ok := strings[v]; ok {
		return s
	}

	s := String(v)
	strings[v] = &s

	return &s
}
