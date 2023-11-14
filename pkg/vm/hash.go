package vm

import (
	"context"
	"encoding/binary"
	"math"

	"github.com/cespare/xxhash/v2"

	fjson "github.com/styrainc/enterprise-opa-private/pkg/json"
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

func hash(ctx context.Context, value interface{}) (uint64, error) {
	hasher := xxhash.New()
	err := hashImpl(ctx, value, hasher)
	return hasher.Sum64(), err
}

func hashImpl(ctx context.Context, value interface{}, hasher *xxhash.Digest) error {
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

		h, err := value.Hash(ctx)
		if err != nil {
			return err
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, h)
		hasher.Write(b)

	case fjson.Object:
		hasher.Write([]byte{typeHashObject})

		var m uint64
		names := value.Names()
		var err error
		for i := 0; i < len(names) && err == nil; i++ {
			m, err = ObjectHashEntry(ctx, m, fjson.NewString(names[i]), value.Iterate(i))
		}

		if err != nil {
			return err
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	case Set:
		hasher.Write([]byte{typeHashSet})

		m, err := value.Hash(ctx)
		if err != nil {
			return err
		}

		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, m)
		hasher.Write(b)

	default:
		panic("json: unsupported type")
	}

	return nil
}

func hashString(value *fjson.String, hasher *xxhash.Digest) {
	hasher.Write([]byte{typeHashString})

	if len(*value) == 0 {
		return
	}

	hasher.WriteString(string(*value))
}

// ObjectHashEntry hashes an object key-value pair. To be used with
// any object implementation, to guarantee identical hashing across
// different object implementations.
func ObjectHashEntry(ctx context.Context, h uint64, k, v interface{}) (uint64, error) {
	hasher := xxhash.New()
	err := hashImpl(ctx, k, hasher)
	if err == nil {
		err = hashImpl(ctx, v, hasher)
	}

	return h + hasher.Sum64(), err
}
