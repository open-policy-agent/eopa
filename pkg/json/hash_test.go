package json

import (
	gojson "encoding/json"
	"testing"
)

// Ensure the hash implementation does not panic on out-of-range huge floats.
func TestBigFloatHashing(_ *testing.T) {
	f := Float{value: gojson.Number("23456789012E666")}
	hash(f)
}
