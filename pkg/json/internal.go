package json

import (
	"encoding/binary"
	"io"

	"github.com/OneOfOne/xxhash"
)

const (
	typeInvalid = iota //nolint // required
	typeNil
	typeFalse
	typeTrue
	typeString
	typeStringInt
	typeNumber
	typeArray
	typeObjectFull
	typeObjectThin
	typeObjectPatch
	typeBinaryFull
)

var order = binary.BigEndian

// contentReader is the interface binary content provider implements. JSON elements constructed use this to access the binary representation below; the interface hides the
// differences between snapshots and deltas.
type contentReader interface {
	// ReadType returns the element type at offset.
	ReadType(offset int64) (int, error)

	// ReadFloat returns a float at offset.
	ReadFloat(offset int64) (float64, error)

	// ReadVarInt returns a variable length integer at offset.
	ReadVarInt(offset int64) (int64, error)

	// ReadString reads a variable length byte array at offset.
	ReadBytes(offset int64) ([]byte, error)

	// ReadString reads a variable length UTF-8 encoded string at offset.
	ReadString(offset int64) (string, error)

	// ReadArray returns a reader to read array at offset.
	ReadArray(offset int64) (arrayReader, error)

	// ReadObject returns a reader to read object at offset.
	ReadObject(offset int64) (objectReader, error)

	io.WriterTo
}

// objectReader provides a convenient access to an object at offset. The rest of the content can be accessed through contentReader.
type objectReader interface {
	contentReader

	// ObjectLen returns the length (number of properties).
	ObjectLen() int

	// ObjectNames returns the property names. The names are returned in lexical order.
	ObjectNames() ([]string, error)

	// ObjectNamesIndex returns the property names at 'i'.
	ObjectNamesIndex(i int) (string, error)

	// ObjectName returns the name offset to the name.
	ObjectNameOffset(name string) (int64, bool, error)

	// ObjectValueOffset returns the value offset of the property. It returns false if the value is not found.
	ObjectValueOffset(name string) (int64, bool, error)

	// objectNameValueOffsets returns the property name and the offset to value. The property has to exist.
	objectNameValueOffsets() ([]objectEntry, []int64, error)

	// objectNameOffsetsValueOffsets returns the property name and the offsets to key and value. The property has to exist.
	objectNameOffsetsValueOffsets() ([]objectEntry, []int64, []int64, error)
}

// objectReader provides a convenient access to an array at offset. The rest of the content can be accessed through contentReader.
type arrayReader interface {
	contentReader

	// ArrayLen returns the length of the array.
	ArrayLen() (int, error)

	// ArrayValueOffset returns the offset of the i-th value.
	ArrayValueOffset(i int) (int64, error)
}

// encodingCache holds the type descriptions of JSON objects. It allows object serialization *not* to store key names per instance but mere values.
// All of similar object instances (with exactly the same key names present) will maintain their values in key sorted order within the object.
// Then by having access to a type descriptor, one can effectively use full key names to access these values as the descriptor binds
// key names to indices. This builds on the assumption a typical JSON collection holds a small number of different kinds of objects (in terms
// of property names inside).
type encodingCache struct {
	objectTypes map[uint32][]encodingCacheObjectType
	strings     map[uint32][]encodingCacheStringType
	numbers     map[string]int32
}

type encodingCacheObjectType struct {
	properties []string
	offset     int32
}

type encodingCacheStringType struct {
	s      string
	offset int32
}

func newEncodingCache() *encodingCache {
	return &encodingCache{objectTypes: make(map[uint32][]encodingCacheObjectType), strings: make(map[uint32][]encodingCacheStringType), numbers: make(map[string]int32)}
}

func (c *encodingCache) CacheObjectType(typeName []objectEntry, offset int32) int32 {
	h := uint32(0)
	for i := 0; i < len(typeName); i++ {
		h += xxhash.ChecksumString32(typeName[i].name)
	}

	objects, ok := c.objectTypes[h]
	if ok {
	next:
		for i := 0; i < len(objects); i++ {
			if len(typeName) != len(objects[i].properties) {
				continue
			}

			for j := 0; j < len(objects[i].properties); j++ {
				if typeName[j].name != objects[i].properties[j] {
					continue next
				}
			}

			return objects[i].offset
		}
	}

	t := make([]string, len(typeName))
	for i := range typeName {
		t[i] = typeName[i].name
	}

	c.objectTypes[h] = append(objects, encodingCacheObjectType{t, offset})
	return offset
}

func (c *encodingCache) CacheString(str string, offset int32) int32 {
	h := xxhash.ChecksumString32(str)

	strings, ok := c.strings[h]
	if ok {
		for i := 0; i < len(strings); i++ {
			if strings[i].s == str {
				return strings[i].offset
			}
		}
	}

	c.strings[h] = append(strings, encodingCacheStringType{str, offset})
	return offset
}

func (c *encodingCache) CacheNumber(n string, offset int32) int32 {
	if o, ok := c.numbers[n]; ok {
		return o
	}
	c.numbers[n] = offset
	return offset
}
