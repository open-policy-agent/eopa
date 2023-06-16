package bundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLoaderErrors(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		err   error
		msg   string
	}{
		{"empty string", []byte(""), io.EOF, "expecting io.EOF"},
		{"bad bjson", []byte{0x08, 0x09, 0x09, 0x09, 0x09, 0x09}, nil, "corrupted binary"},
		{"bad json", []byte("{"), nil, "expect \" after {"},
		{"bad number", []byte("{10}"), nil, "but found 1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var x any
			err := loadJSON(tc.input, &x)
			if err == nil {
				t.Fatalf("expected %v", tc.msg)
			}
			if tc.err == nil {
				if !strings.Contains(err.Error(), tc.msg) {
					t.Fatalf("expected %v, got %v", tc.msg, err)
				}
			} else {
				if !errors.Is(err, tc.err) {
					t.Fatal(tc.msg)
				}
			}
		})
	}
}

func TestLoader(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{"json", []byte(`{"a":"b"}`), []byte(`{"a":"b"}`)},
		{"tab", []byte("\t" + `{"a":"b"}`), []byte(`{"a":"b"}`)},
		{"return", []byte("\rtrue"), []byte(`true`)},
		{"newline", []byte("\n" + `"abc"`), []byte(`"abc"`)},
		{"bjson", []byte{0x08, 0x02, 0x00, 0x00, 0x00, 0x0a, 0x00, 0x00, 0x00, 0x0f, 0x08, 0x75, 0x73, 0x65, 0x72, 0x04, 0x0a, 0x61, 0x6c, 0x69, 0x63, 0x65}, []byte(`{"user":"alice"}`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var intf any
			err := loadJSON(tc.input, &intf)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%#v", intf)
			r, err := json.Marshal(intf)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(r, tc.expected) {
				t.Fatalf("got %v, expected %v", r, tc.expected)
			}
		})
	}
}
