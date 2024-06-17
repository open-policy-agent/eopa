package json

import (
	"bytes"

	jsoniter "github.com/json-iterator/go"
)

// Unmarshal stores the JSON data in the value pointed to by v.
func Unmarshal(data Json, v interface{}) error {
	var buf bytes.Buffer
	if _, err := data.WriteTo(&buf); err != nil {
		return err
	}

	return jsoniter.ConfigCompatibleWithStandardLibrary.Unmarshal(buf.Bytes(), v)
}

// UnmarshalUseNumber stores the JSON data in the value pointed to by v. Numbers are stored as json.Number
func UnmarshalUseNumber(data Json, v interface{}) error {
	var buf bytes.Buffer
	if _, err := data.WriteTo(&buf); err != nil {
		return err
	}

	dec := jsoniter.ConfigCompatibleWithStandardLibrary.NewDecoder(&buf)
	dec.UseNumber()
	return dec.Decode(v)
}

func (decoder *Decoder) UnmarshalString() (string, error) {
	token := decoder.iter.ReadString()
	if err := decoder.error(); err != nil {
		return "", err
	}

	return token, nil
}

func (decoder *Decoder) UnmarshalObject(f func(property string, decoder *Decoder) error) error {
	var err error
	decoder.iter.ReadMapCB(func(_ *jsoniter.Iterator, field string) bool {
		err = f(field, decoder)
		return err == nil
	})
	if err != nil {
		return err
	}
	return decoder.error()
}

func (decoder *Decoder) UnmarshalArray(f func(decoder *Decoder) error) error {
	var err error
	decoder.iter.ReadArrayCB(func(*jsoniter.Iterator) bool {
		err = f(decoder)
		return err == nil
	})
	if err != nil {
		return err
	}
	return decoder.error()
}
