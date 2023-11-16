package json

import (
	"encoding/binary"
	"math"

	"github.com/cespare/xxhash/v2"
)

const (
	typeHashNull = iota
	typeHashBool
	typeHashFloat
	typeHashString
	typeHashArray
	typeHashObject
	typeHashSet
)

func hash(value interface{}) uint64 {
	hasher := xxhash.New()
	hashImpl(value, hasher)
	return hasher.Sum64()
}

func hashImpl(value interface{}, hasher *xxhash.Digest) {
	// Note Hasher writer below never returns an error.

	switch value := value.(type) {
	case Null:
		hasher.Write([]byte{typeHashNull})

	case Bool:
		if value.Value() {
			hasher.Write([]byte{typeHashBool, 1})
		} else {
			hasher.Write([]byte{typeHashBool, 0})
		}

	case Float:
		f, err := value.Value().Float64()
		if err != nil {
			panic("invalid float")
		}
		// NOTE(sr): Picked BigEndian here because we've done it below. It shouldn't matter
		// for the hashing here.
		b := make([]byte, 9)
		b[0] = typeHashFloat
		binary.BigEndian.PutUint64(b[1:], math.Float64bits(f))
		hasher.Write(b)

	case *String:
		hashString(value, hasher)

	case Array:
		n := value.Len()
		for i := 0; i < n; i++ {
			hashImpl(value.Iterate(i), hasher)
		}

		hasher.Write([]byte{typeHashArray})

	case Object2:
		// The two object implementation should have equal
		// hash implementation

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, value.Hash())
		b[0] = typeHashObject
		hasher.Write(b)

	case Object:
		var m uint64
		names := value.Names()
		for i := 0; i < len(names); i++ {
			m = objectHashEntry(m, NewString(names[i]), value.Iterate(i))
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		b[0] = typeHashObject
		hasher.Write(b)

	case Set:
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, value.Hash())
		b[0] = typeHashSet
		hasher.Write(b)

	default:
		panic("json: unsupported type")
	}
}

func hashString(value *String, hasher *xxhash.Digest) {
	hasher.Write([]byte{typeHashString})

	if len(*value) == 0 {
		return
	}

	hasher.WriteString(string(*value))
}

// objectHashEntry hashes an object key-value pair. To be used with
// any object implementation, to guarantee identical hashing across
// different object implementations.
func objectHashEntry(h uint64, k, v interface{}) uint64 {
	hasher := xxhash.New()
	hashImpl(k, hasher)
	hashImpl(v, hasher)

	return h + hasher.Sum64()
}
