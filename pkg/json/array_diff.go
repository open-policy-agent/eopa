package json

import (
	"fmt"

	"github.com/OneOfOne/xxhash"
)

// TODO: scan the output for gaps (unidentified source values): if the gap between identified values equals to the gap between identified values in source, assume patching.
// else revert to set.

// arrayDiff runs a diff between the two arrays ('ar' and 'br') and returns a new array as indices to either one of the values, effectively trying to reuse elements of 'ar' to the extent
// they can be reused.
func arrayDiff(a contentReader, ar arrayReader, b contentReader, br arrayReader, ac *hashCache, bc *hashCache) ([]arrayIndex, bool, error) {
	al, err := ar.ArrayLen()
	if err != nil {
		return nil, false, err
	}

	bl, err := br.ArrayLen()
	if err != nil {
		return nil, false, err
	}

	av := make([]int64, al)
	bv := make([]int64, bl)

	// Compute hashes for all elements both arrays, gathering all values to be readily available for the cross comparison below.

	ah := make(map[uint64][]int, al)
	bh := make(map[uint64][]int, bl)
	bhl := make([]uint64, bl)

	for i := 0; i < al; i++ {
		off, err := ar.ArrayValueOffset(i)
		if err != nil {
			return nil, false, err
		}

		av[i] = off

		h, err := ac.Hash(av[i])
		if err != nil {
			return nil, false, err
		}

		ah[h] = append(ah[h], i)
	}

	for i := 0; i < bl; i++ {
		off, err := br.ArrayValueOffset(i)
		if err != nil {
			return nil, false, err
		}

		bv[i] = off

		h, err := bc.Hash(bv[i])
		if err != nil {
			return nil, false, err
		}

		bh[h] = append(bh[h], i)
		bhl[i] = h
	}

	// Identify the old elements that can be used with the new array.

	result := make([]arrayIndex, bl)

	for i, h := range bhl {
		result[i] = arrayIndex{b: true, offset: bv[i]}

		for _, j := range ah[h] {
			// Hash was identical, compare for equality.
			eq, err := elementEqual(a, av[j], b, bv[i])
			if err != nil {
				return nil, false, err
			}

			if eq {
				result[i] = arrayIndex{b: false, offset: av[j]}
				break
			}
		}
	}

	// Compare the resulting array to the 'a', to see if it's different or not. It's different if the length is different, any values were obtained from 'b' or if the value offsets are
	// not identical for a given array index.

	if al != bl {
		return result, true, nil
	}

	for i, ai := range result {
		if ai.b || ai.offset != av[i] {
			return result, true, nil
		}
	}

	// Resulting array is exactly the same as 'a', as long as it's length remained the same.

	return nil, false, nil
}

// arrayIndex holds a reference to an element in one of two arrays ('a' or 'b'). If 'b' is true, the offset is an offset to an element in 'b', otherwise 'a'.
type arrayIndex struct {
	b      bool
	offset int64
}

func resolveType(content contentReader, offset int64) (int, error) {
	if offset < 0 {
		switch t := -offset; t {
		case typeNil, typeFalse, typeTrue:
			return int(t), nil
		default:
			return 0, fmt.Errorf("json: corrupted file (invalid type = %d)", t)
		}
	}

	return content.ReadType(offset)
}

func elementEqual(a contentReader, aoff int64, b contentReader, boff int64) (bool, error) {
	ta, err := resolveType(a, aoff)
	if err != nil {
		return false, err
	}

	tb, err := resolveType(b, boff)
	if err != nil {
		return false, err
	}

	if ta != tb {
		return false, nil
	}

	switch ta {
	case typeNil, typeFalse, typeTrue:
		return true, nil

	case typeString, typeNumber:
		sa, err := a.ReadString(aoff)
		if err != nil {
			return false, err
		}

		sb, err := b.ReadString(boff)
		if err != nil {
			return false, err
		}

		return sa == sb, nil

	case typeStringInt:
		va, err := a.ReadVarInt(aoff)
		if err != nil {
			return false, err
		}

		vb, err := b.ReadVarInt(boff)
		if err != nil {
			return false, err
		}

		return va == vb, nil

	case typeArray:
		aa, err := a.ReadArray(aoff)
		if err != nil {
			return false, err
		}
		ab, err := b.ReadArray(boff)
		if err != nil {
			return false, err
		}

		al, err := aa.ArrayLen()
		if err != nil {
			return false, err
		}

		bl, err := ab.ArrayLen()
		if err != nil {
			return false, err
		}

		if al != bl {
			return false, nil
		}

		for i := 0; i < al; i++ {
			aoff, err := aa.ArrayValueOffset(i)
			if err != nil {
				return false, err
			}

			boff, err := ab.ArrayValueOffset(i)
			if err != nil {
				return false, err
			}

			eq, err := elementEqual(a, aoff, b, boff)
			if err != nil || !eq {
				return false, err
			}
		}

		return true, nil

	case typeObjectFull, typeObjectThin, typeObjectPatch:
		oa, err := a.ReadObject(aoff)
		if err != nil {
			return false, err
		}
		ob, err := b.ReadObject(boff)
		if err != nil {
			return false, err
		}

		if oa.ObjectLen() != ob.ObjectLen() {
			return false, nil
		}

		aentries, aoffsets, err := oa.objectNameValueOffsets()
		if err != nil {
			return false, err
		}

		bentries, boffsets, err := ob.objectNameValueOffsets()
		if err != nil {
			return false, err
		}

		// Compare the property names and values.

		for i := range aentries {
			if aentries[i].name != bentries[i].name {
				return false, nil
			}

			eq, err := elementEqual(a, aoffsets[i], b, boffsets[i])
			if err != nil || !eq {
				return false, err
			}
		}

		return true, nil

	default:
		panic("json: unsupported type")
	}
}

type hashCache struct {
	hashes  map[int64]uint64
	content contentReader
}

func newHashCache(content contentReader) *hashCache {
	return &hashCache{hashes: make(map[int64]uint64), content: content}
}

func (h *hashCache) Hash(offset int64) (uint64, error) {
	if hash, ok := h.hashes[offset]; ok {
		return hash, nil
	}

	hasher := xxhash.New64()

	err := h.hash(offset, hasher)
	if err != nil {
		return 0, err
	}

	h.hashes[offset] = hasher.Sum64()

	return hasher.Sum64(), nil
}

func (h *hashCache) hash(offset int64, hasher *xxhash.XXHash64) error {
	t, err := resolveType(h.content, offset)
	if err != nil {
		return err
	}

	// Note Hasher writer below never returns an error.

	switch t {
	case typeNil, typeFalse, typeTrue:
		hasher.Write([]byte{byte(t)})
		return nil

	case typeString, typeNumber:
		b, err := h.content.ReadBytes(offset)
		if err != nil {
			return err
		}

		hasher.Write([]byte{byte(t)})
		hasher.Write(b)
		return nil

	case typeStringInt:
		v, err := h.content.ReadVarInt(offset)
		if err != nil {
			return err
		}

		hasher.Write([]byte{byte(t)})

		b := make([]byte, 8)
		order.PutUint64(b, uint64(v))
		hasher.Write(b)

		return nil

	case typeArray:
		a, err := h.content.ReadArray(offset)
		if err != nil {
			return err
		}

		al, err := a.ArrayLen()
		if err != nil {
			return err
		}

		hasher.Write([]byte{byte(t)})

		for i := 0; i < al && err == nil; i++ {
			offset, err = a.ArrayValueOffset(i)
			if err == nil {
				err = h.hash(offset, hasher)
			}
		}

		return err

	case typeObjectFull, typeObjectThin, typeObjectPatch:
		o, err := h.content.ReadObject(offset)
		if err != nil {
			return err
		}

		hasher.Write([]byte{byte(typeObjectFull)}) // Internal representation should not affect the hash.

		entries, offsets, err := o.objectNameValueOffsets()
		for i := 0; i < len(entries) && err == nil; i++ {
			hasher.Write([]byte(entries[i].name))
			err = h.hash(offsets[i], hasher)
		}

		return err

	default:
		panic("json: unsupported type")
	}
}
