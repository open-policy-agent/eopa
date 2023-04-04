package json

import (
	"testing"
)

func TestEncodingCache(t *testing.T) {
	c := newEncodingCache()

	// Object type

	a := []objectEntry{{name: "a"}}
	b := []objectEntry{{name: "a"}, {name: "b"}}

	if offset := c.CacheObjectType(a, 1); offset != 1 {
		t.Errorf("error in inserting the first object type")
	}

	if offset := c.CacheObjectType(a, 2); offset != 1 {
		t.Errorf("error in inserting the second object type")
	}

	if offset := c.CacheObjectType(b, 3); offset != 3 {
		t.Errorf("error in inserting the first object type again")
	}

	// String type

	if offset := c.CacheString("a", 1); offset != 1 {
		t.Errorf("error in inserting the first string")
	}

	if offset := c.CacheString("a", 2); offset != 1 {
		t.Errorf("error in inserting the second string")
	}

	// Number type

	if offset := c.CacheNumber("1", 1); offset != 1 {
		t.Errorf("error in inserting the first number")
	}

	if offset := c.CacheNumber("1", 2); offset != 1 {
		t.Errorf("error in inserting the second number")
	}
}
