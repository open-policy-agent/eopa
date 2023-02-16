package vm

import (
	"context"
	"encoding/binary"
	"math"
	"reflect"
	"unsafe"

	"github.com/OneOfOne/xxhash"

	fjson "github.com/styrainc/load-private/pkg/json"
)

const (
	typeHashNull   = 0
	typeHashBool   = 1
	typeHashFloat  = 2
	typeHashString = 3
	typeHashArray  = 4
	typeHashObject = 5
	typeHashSet    = 6

	maxInt32 int32 = (1<<31 - 1)
)

// hashable is for testing purposes, to allow hash function customization.
type hashable interface {
	Equal(other hashable) bool
	Hash() uint64
}

func hash(ctx context.Context, value interface{}) (uint64, error) {
	hasher := xxhash.New64()
	err := hashImpl(ctx, value, hasher)
	return hasher.Sum64(), err
}

func hashImpl(ctx context.Context, value interface{}, hasher *xxhash.XXHash64) error {
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

	case *fjson.String:
		hashString(value, hasher)

	case fjson.Array:
		hasher.Write([]byte{typeHashArray})

		n := value.Len()
		var err error
		for i := 0; i < n && err == nil; i++ {
			err = hashImpl(ctx, value.Iterate(i), hasher)
		}
		return err

	case IterableObject:
		// The two object implementation should have equal
		// hash implementation
		hasher.Write([]byte{typeHashObject})

		var m uint64
		var err2 error
		if err := value.Iter(ctx, func(k, v interface{}) bool {
			hasher := xxhash.New64()
			err2 = hashImpl(ctx, k, hasher)
			if err2 == nil {
				err2 = hashImpl(ctx, v, hasher)
			}
			if err2 != nil {
				return true
			}

			m += hasher.Sum64()
			return false
		}); err != nil {
			return err
		} else if err2 != nil {
			return err2
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	case fjson.Object:
		hasher.Write([]byte{typeHashObject})

		var m uint64
		names := value.Names()
		var err error
		for i := 0; i < len(names) && err == nil; i++ {
			hasher := xxhash.New64()
			key := fjson.NewString(names[i])
			hashString(key, hasher)
			err = hashImpl(ctx, value.Iterate(i), hasher)
			m += hasher.Sum64()
		}

		if err != nil {
			return err
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	case *Set:
		hasher.Write([]byte{typeHashSet})

		var m uint64
		var err error
		value.Iter(func(v fjson.Json) bool {
			hasher := xxhash.New64()
			err = hashImpl(ctx, v, hasher)
			if err != nil {
				return true
			}
			m += hasher.Sum64()
			return false
		})
		if err != nil {
			return err
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	case hashable:
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, value.Hash())
		hasher.Write(b)

	default:
		panic("json: unsupported type")
	}

	return nil
}

func hashString(value *fjson.String, hasher *xxhash.XXHash64) {
	hasher.Write([]byte{typeHashString})

	if len(*value) == 0 {
		return
	}

	ss := (*reflect.StringHeader)(unsafe.Pointer(value))
	hasher.Write((*[maxInt32]byte)(unsafe.Pointer(ss.Data))[:len(*value):len(*value)])
}
