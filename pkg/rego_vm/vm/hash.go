package vm

import (
	"encoding/binary"

	"github.com/OneOfOne/xxhash"

	fjson "github.com/StyraInc/load/pkg/json"
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
		hasher.Write([]byte{0})

	case fjson.Bool:
		hasher.Write([]byte{1})
		if value.Value() {
			hasher.Write([]byte{1})
		} else {
			hasher.Write([]byte{0})
		}

	case fjson.Float:
		hasher.Write([]byte{2})
		hasher.Write([]byte(value.Value()))

	case fjson.String:
		hasher.Write([]byte{3})
		hasher.Write([]byte(value.Value()))

	case fjson.Array:
		hasher.Write([]byte{4})

		n := value.Len()
		for i := 0; i < n; i++ {
			hashImpl(value.Iterate(i), hasher)
		}

	case *Object:
		// The two object implementation should have equal
		// hash implementation
		hasher.Write([]byte{5})

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
		hasher.Write([]byte{5})

		var m uint64
		names := value.Names()
		for i := 0; i < len(names); i++ {
			hasher := xxhash.New64()
			hashImpl(fjson.NewString(names[i]), hasher)
			hashImpl(value.Iterate(i), hasher)
			m += hasher.Sum64()
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	case *Set:
		hasher.Write([]byte{6})

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
