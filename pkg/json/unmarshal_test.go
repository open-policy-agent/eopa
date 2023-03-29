package json

import (
	"bytes"
	"testing"
)

func TestUnmarshalString(t *testing.T) {
	decoder := NewDecoder(bytes.NewBuffer([]byte(`"a"`)))
	if x, err := decoder.UnmarshalString(); x != "a" || err != nil {
		t.Fatalf("parsing failure: %s", err)
	}
}

func TestUnmarshalObject(t *testing.T) {
	decoder := NewDecoder(bytes.NewBuffer([]byte(`{"a": "x", "b": "y"}[`)))
	parsed := NewObject(nil)
	if err := decoder.UnmarshalObject(func(property string, decoder *Decoder) error {
		v, err := decoder.Decode()
		if err != nil {
			return err
		}
		parsed.Set(property, v)
		return nil
	}); err != nil {
		t.Errorf("parsing failure: %s", err)
	}

	correct, _ := New(map[string]interface{}{"a": "x", "b": "y"})
	if correct.Compare(parsed) != 0 {
		t.Error("parsing failure: not equal")
	}
}

func TestUnmarshalArray(t *testing.T) {
	decoder := NewDecoder(bytes.NewBuffer([]byte(`["a", "b"]{`)))
	parsed := NewArray()
	if err := decoder.UnmarshalArray(func(decoder *Decoder) error {
		v, err := decoder.Decode()
		if err != nil {
			return err
		}
		parsed = parsed.Append(v)
		return nil
	}); err != nil {
		t.Errorf("parsing failure: %s", err)
	}

	correct, _ := New([]interface{}{"a", "b"})
	if correct.Compare(parsed) != 0 {
		t.Error("parsing failure: not equal")
	}
}
