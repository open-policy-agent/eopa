package vm

import (
	"encoding/binary"
	"math"

	"github.com/OneOfOne/xxhash"

	fjson "github.com/styrainc/load/pkg/json"
)

const (
	typeHashNull   = 0
	typeHashBool   = 1
	typeHashFloat  = 2
	typeHashString = 3
	typeHashArray  = 4
	typeHashObject = 5
	typeHashSet    = 6
)

func hash(value interface{}) uint64 {
	hasher := xxhash.New64()
	hashImpl(value, hasher)
	return hasher.Sum64()
}

func hashImpl(value interface{}, hasher *xxhash.XXHash64) {
	// Note Hasher writer below never returns an error.

	switch value := value.(type) {
	case fjson.Null:
		hasher.Write([]byte{typeHashNull})

	case fjson.Bool:
		hasher.Write([]byte{typeHashBool})
		if value.Value() {
			hasher.Write([]byte{1})
		} else {
			hasher.Write([]byte{0})
		}

	case fjson.Float:
		hasher.Write([]byte{typeHashFloat})
		f, err := value.Value().Float64()
		if err != nil {
			panic("invalid float")
		}
		// NOTE(sr): Picked BigEndian here because we've done it below. It shouldn't matter
		// for the hashing here.
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, math.Float64bits(f))
		hasher.Write(b)

	case fjson.String:
		hashString(value, hasher)

	case fjson.Array:
		hasher.Write([]byte{typeHashArray})

		n := value.Len()
		for i := 0; i < n; i++ {
			hashImpl(value.Iterate(i), hasher)
		}

	case *Object:
		// The two object implementation should have equal
		// hash implementation
		hasher.Write([]byte{typeHashObject})

		var m uint64
		value.Iter(func(k, v fjson.Json) bool {
			hasher := xxhash.New64()
			hashImpl(k, hasher)
			hashImpl(v, hasher)
			m += hasher.Sum64()
			return false
		})

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	case fjson.Object:
		hasher.Write([]byte{typeHashObject})

		var m uint64
		names := value.Names()
		for i := 0; i < len(names); i++ {
			hasher := xxhash.New64()
			key := fjson.NewString(names[i])
			hashString(key, hasher)
			hashImpl(value.Iterate(i), hasher)
			m += hasher.Sum64()
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	case *Set:
		hasher.Write([]byte{typeHashSet})

		var m uint64
		value.Iter(func(v fjson.Json) bool {
			hasher := xxhash.New64()
			hashImpl(v, hasher)
			m += hasher.Sum64()
			return false
		})

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	default:
		panic("json: unsupported type")
	}
}

func hashString(value fjson.String, hasher *xxhash.XXHash64) {
	hasher.Write([]byte{typeHashString})
	hasher.Write([]byte(value.Value()))
}
