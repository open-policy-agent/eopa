package json

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/styrainc/load-private/pkg/json/internal/utils"
)

// testTime is for unit tests to control the time.
var testTime time.Time

func Debug(c Collections) Object {
	return c.(*snapshot).ObjectBinary
}

// snapshot implements the Collections interface. It provides an interface to access the snapshots of collections stored within.
// Internally, it is a hierarchical namespace of resources, organized around nested maps. A leaf map represents a resource and holds the meta data for the particular resource.
type snapshot struct {
	ObjectBinary
	slen    int64
	blen    int64         // Length in bytes for the entire snapshot.
	objects []interface{} // Storage objects used to construct the collection, if any.
}

func NewCollectionsFromReaders(snapshotReader *utils.MultiReader, slen int64, dr *utils.MultiReader, dlen int64, objects ...interface{}) (collections Collections, err error) {
	if dr != nil {
		var impl *deltaReader
		impl, err = newDeltaReader(snapshotReader, slen, dr)
		if err == nil {
			collections = &snapshot{ObjectBinary: newObject(impl, 0), slen: slen, blen: slen + dlen, objects: objects}
		}
	} else {
		collections = &snapshot{ObjectBinary: newObject(newSnapshotReader(snapshotReader), 0), slen: slen, blen: slen, objects: objects}
	}

	return collections, err
}

func (s snapshot) Resource(name string) Resource {
	return findImpl2(s.ObjectBinary, PathSegments(name), 0)
}

func (s snapshot) Collections() []string {
	collections := make([]string, 0)

	s.Walk(func(resource Resource) bool {
		if resource.Kind() == JSON {
			collections = append(collections, resource.Name())
		}

		return true
	})

	sort.Strings(collections)
	return collections
}

func (s snapshot) Walk(callback func(resource Resource) bool) {
	s.find("").Walk(callback)
}

func (s snapshot) WriteTo(w io.Writer) (int64, error) {
	return s.content.WriteTo(w)
}

func (s snapshot) Diff(other Collections) (*utils.BytesReader, int64, bool, error) {
	return diff(s.ObjectBinary.content, s.Len(), other.(*snapshot).ObjectBinary.content)
}

func (s snapshot) Writable() WritableCollections {
	return &writableSnapshot{data: s.ObjectBinary.Clone(true).(Object)}
}

func (s snapshot) Reader() *utils.MultiReader {
	if reader, ok := s.ObjectBinary.content.(*snapshotObjectReader); ok {
		return reader.content
	}

	panic("not reached")
}

func (s snapshot) DeltaReader() *utils.MultiReader {
	if reader, ok := s.ObjectBinary.content.(*deltaObjectReader); ok {
		return reader.delta
	}

	if reader, ok := s.ObjectBinary.content.(*deltaPatchObjectReader); ok {
		delta, err := reader.deltaPatch.serialize()
		checkError(err)

		return utils.NewMultiReaderFromBytesReader(delta)
	}

	panic("not reached")
}

func (s snapshot) Len() int64 {
	return s.blen
}

func (s snapshot) Objects() []interface{} {
	return s.objects
}

func (s *snapshot) WriteBlob(name string, blob Blob) {
	*s = *s.newDeltaPatch().WriteBlob(name, blob)
}

func (s *snapshot) WriteJSON(name string, j Json) {
	*s = *s.newDeltaPatch().WriteJSON(name, j)
}

func (s *snapshot) PatchJSON(name string, patch Patch) (bool, error) {
	patched, collection, err := s.newDeltaPatch().PatchJSON(name, patch)
	if collection != nil {
		*s = *collection
	}
	return patched, err
}

func (s *snapshot) WriteDirectory(name string) {
	*s = *s.newDeltaPatch().WriteDirectory(name)
}

func (s *snapshot) Remove(name string) bool {
	removed, collection := s.newDeltaPatch().Remove(name)
	if collection != nil {
		*s = *collection
	}
	return removed
}

func (s *snapshot) WriteMeta(name string, key string, value string) bool {
	written, collection := s.newDeltaPatch().WriteMeta(name, key, value)
	if written {
		*s = *collection
	}
	return written
}

func (s snapshot) newDeltaPatch() *deltaPatch {
	if reader, ok := s.ObjectBinary.content.(*snapshotObjectReader); ok {
		return newDeltaPatch(reader.content, s.slen, nil, s.objects)
	}

	if reader, ok := s.ObjectBinary.content.(*deltaPatchObjectReader); ok {
		// Keep extending the patch with new writes instead of creating new.
		return reader.deltaPatch
	}

	reader := s.ObjectBinary.content.(*deltaObjectReader)
	return newDeltaPatch(reader.snapshot, s.slen, reader.delta, s.objects)
}

func (s snapshot) find(name string) Resource {
	return findImpl(s.ObjectBinary, name)
}

type resourceImpl struct {
	name string
	obj  Object
}

func findImpl(obj Object, name string) Resource {
	return findImpl2(obj, PathSegments(name), 0)
}

func findImpl2(obj Object, segs []string, i int) Resource {
	if len(segs) == i {
		return &resourceImpl{strings.Join(segs, "/"), obj}
	}

	if kindImpl(obj) != Directory {
		return nil
	}

	child := obj.Value("data:" + segs[i])
	if child == nil {
		return nil
	}

	cobj, ok := child.(Object)
	if !ok {
		return &resourceImpl{strings.Join(segs[:i+1], "/"), obj}
	}
	return findImpl2(cobj, segs, i+1)
}

func kindImpl(obj Object) Kind {
	kind := obj.Value("kind")

	if kind == nil {
		return Directory
	}

	if _, ok := kind.(String); !ok {
		corrupted(nil)
		return Invalid
	}

	var k int
	fmt.Sscanf(kind.(String).Value(), "%d", &k)

	switch Kind(k) {
	case Unstructured:
		return Unstructured
	case JSON:
		return JSON
	default:
		return Invalid
	}
}

// createImpl prepares the hierarchy starting at Object for modifications.
func createImpl(j Object, name string) (Object, *resourceImpl) {
	return createImpl2(j, PathSegments(name), 0)
}

func createImpl2(obj Object, segs []string, i int) (Object, *resourceImpl) {
	prefix := "data:"

	if len(segs) == i {
		if o, ok := obj.(ObjectBinary); ok {
			obj = o.clone()
		}

		return obj, &resourceImpl{strings.Join(segs, "/"), obj}
	}

	// Remove any non-directory contents.

	if kindImpl(obj) != Directory {
		for _, name := range obj.Names() {
			obj = obj.Remove(name)
		}
	}

	child := obj.Value(prefix + segs[i])
	if c, ok := child.(Object); child != nil && ok {
		c, r := createImpl2(c, segs, i+1)
		obj, _ = obj.Set(prefix+segs[i], c)
		return obj, r
	}

	childo, r := createImpl2(NewObject(nil), segs, i+1)
	obj, _ = obj.Set(prefix+segs[i], childo)
	return obj, r

}

func (r *resourceImpl) Name() string {
	return r.name
}

func (r *resourceImpl) Kind() Kind {
	if r == nil {
		return Invalid
	}

	return kindImpl(r.obj)
}

// setKind expect the resource to be prepared for modification.
func (r *resourceImpl) setKind(kind Kind) {
	if kind != Directory {
		if _, ok := r.obj.Set("kind", NewString(fmt.Sprintf("%d", kind))); ok {
			panic("not reached")
		}
	}
}

func (r *resourceImpl) Resource(name string) Resource {
	segs := PathSegments(r.name)
	i := len(segs)
	segs = append(segs, PathSegments(name)...)
	return findImpl2(r.obj, segs, i)
}

func (r *resourceImpl) Resources() []Resource {
	resources := make([]Resource, 0)
	prefix := "data:"

	if r.Kind() != Directory {
		panic("json: not a directory")
	}

	segs := PathSegments(r.Name())

	for _, name := range r.obj.Names() {
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		name = name[len(prefix):]

		if resource := findImpl2(r.obj, append(segs, name), len(segs)); resource != nil {
			resources = append(resources, resource)
		}
	}

	return resources
}

func (r *resourceImpl) File() File {
	return r.obj.valueImpl("data")
}

func (r *resourceImpl) Blob() Blob {
	if r.Kind() != Unstructured {
		panic("json: not a blob")
	}

	value := r.obj.valueImpl("data")
	if value == nil {
		return nil
	}

	if blob, ok := value.(Blob); ok {
		return blob
	}
	return nil
}

func (r *resourceImpl) setBlob(blob Blob) {
	if r.Kind() != Unstructured {
		panic("json: not a blob")
	}

	if _, ok := r.obj.setImpl("data", blob); ok {
		panic("not reached")
	}

	// Remove any subdirectories, if this was a directory before.
	for _, name := range r.obj.Names() {
		if strings.HasPrefix(name, "data:") {
			if r.obj.Remove(name) != r.obj {
				panic("not reached")
			}
		}
	}
}

func (r *resourceImpl) JSON() Json {
	if r.Kind() != JSON {
		panic("json: not a json")
	}

	return r.obj.Value("data")
}

// setJSON expect the resource to be prepared for modification.
func (r *resourceImpl) setJSON(j Json) {
	if r.Kind() != JSON {
		panic("json: not a json")
	}

	if _, ok := r.obj.Set("data", j); ok {
		panic("not reached")
	}

	// Remove any subdirectories, if this was a directory before.
	for _, name := range r.obj.Names() {
		if strings.HasPrefix(name, "data:") {
			if r.obj.Remove(name) != r.obj {
				panic("not reached")
			}
		}
	}
}

func (r *resourceImpl) Meta(key string) (string, bool) {
	if v := r.obj.Value(fmt.Sprintf("meta:%s", key)); v == nil {
		return "", false
	} else if s, ok := v.(String); ok {
		return s.Value(), true
	} else {
		return "", false
	}
}

// setMeta expect the resource to be prepared for modification.
func (r *resourceImpl) setMeta(key string, value string) {
	if _, ok := r.obj.setImpl(fmt.Sprintf("meta:%s", key), NewString(value)); ok {
		panic("not reached")
	}
}

func (r *resourceImpl) Walk(callback func(resource Resource) bool) {
	if !callback(r) {
		return
	}

	if r.Kind() == Directory {
		for _, r := range r.Resources() {
			r.Walk(callback)
		}
	}
}

type writableSnapshot struct {
	data Object
}

// NewCollections translates a Go native collection of collections to the binary snapshot.
func NewCollections() WritableCollections {
	return &writableSnapshot{data: NewObject(nil)}
}

func (s *writableSnapshot) Resource(name string) Resource {
	return findImpl2(s.data, PathSegments(name), 0)
}

func (s *writableSnapshot) WriteBlob(name string, blob Blob) {
	r := s.create(name)
	r.setKind(Unstructured)
	r.setBlob(blob)
}

func (s *writableSnapshot) WriteJSON(name string, j Json) {
	r := s.create(name)
	r.setKind(JSON)
	r.setJSON(j)
}

func (s *writableSnapshot) PatchJSON(name string, patch Patch) (bool, error) {
	r := s.Resource(name)
	if r.Kind() != JSON {
		return false, fmt.Errorf("not JSON")
	}

	patched, err := patch.ApplyTo(r.JSON())
	if err != nil {
		return false, err
	}

	s.WriteJSON(name, patched)
	return true, nil
}

func (s *writableSnapshot) WriteDirectory(name string) {
	r := s.create(name)
	r.setKind(Directory)
}

func (s *writableSnapshot) WriteMeta(name string, key string, value string) bool {
	r := s.find(name)
	if r == nil {
		return false
	}
	r.(*resourceImpl).setMeta(key, value)
	return true
}

func (s *writableSnapshot) Prepare(timestamp time.Time) Collections {
	s.setMetaRecursively(s.Resource(""), "timestamp", fmt.Sprintf("%d", timestamp.UnixNano()))

	content, slen, err := translate(s.data)
	if err != nil {
		corrupted(err)
	}

	return &snapshot{ObjectBinary: newObject(content, 0), blen: slen, slen: slen, objects: []interface{}{nil}}
}

func (s *writableSnapshot) setMetaRecursively(r Resource, key string, value string) {
	if _, ok := r.Meta(key); !ok {
		s.WriteMeta(r.Name(), key, value)
	}

	value, _ = r.Meta(key)

	if r.Kind() == Directory {
		for _, child := range r.Resources() {
			s.setMetaRecursively(child, key, value)
		}
	}
}

func (s *writableSnapshot) find(name string) Resource {
	return findImpl(s.data, name)
}

func (s *writableSnapshot) create(name string) *resourceImpl {
	var r *resourceImpl
	s.data, r = createImpl(s.data, name)
	return r
}

func translate(data interface{}) (*snapshotReader, int64, error) {
	cache := newEncodingCache()
	buffer := new(bytes.Buffer)

	_, err := serialize(data, cache, buffer, 0)
	if err != nil {
		return nil, 0, err
	}

	v := buffer.Bytes()
	content := utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(v))
	return newSnapshotReader(content), int64(len(v)), nil
}

// serialize transforms the provided native representation to the storage byte format.
func serialize(data interface{}, cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	// Note: below Write and WriteByte to buffer never return an error even if their function signature would allow so.
	offset := base + int32(buffer.Len())

	if _, ok := data.(Null); data == nil || ok {
		// If this is not the first JSON element of the document, return an offset embedding the element.
		if offset > base {
			return -typeNil, nil
		}

		buffer.WriteByte(typeNil)
		return offset, nil
	}

	switch v := data.(type) {
	case string:
		return serializeString(v, cache, buffer, base), nil

	case String:
		return serializeString(v.Value(), cache, buffer, base), nil

	case bool:
		return serializeBool(v, cache, buffer, base), nil

	case Bool:
		return serializeBool(v.Value(), cache, buffer, base), nil

	case json.Number:
		return serializeNumber(v, cache, buffer, base), nil

	case Float:
		return serializeNumber(v.Value(), cache, buffer, base), nil

	case int, int32, int64:
		// Check for an earlier serialization of the number.
		n := fmt.Sprintf("%d", reflect.ValueOf(v).Int())
		if existing := cache.CacheNumber(n, offset); existing != offset {
			return existing, nil
		}

		// A new, unique number found. Write type and string encoded number.
		buffer.WriteByte(typeNumber)
		writeString(n, buffer)

	case uint, uint32, uint64:
		// Check for an earlier serialization of the number.
		n := fmt.Sprintf("%d", reflect.ValueOf(v).Uint())
		if existing := cache.CacheNumber(n, offset); existing != offset {
			return existing, nil
		}

		// A new, unique number found. Write type and string encoded number.
		buffer.WriteByte(typeNumber)
		writeString(n, buffer)

	case float32, float64:
		// Check for an earlier serialization of the float.
		n := fmt.Sprintf("%g", reflect.ValueOf(v).Float())
		if existing := cache.CacheNumber(n, offset); existing != offset {
			return existing, nil
		}

		// A new, unique number found. Write type and string encoded number.
		buffer.WriteByte(typeNumber)
		writeString(n, buffer)

	case []interface{}:
		return serializeArray(v, cache, buffer, base)

	case Array:
		array := make([]interface{}, 0, v.Len())

		for i := 0; i < v.Len(); i++ {
			array = append(array, v.Value(i))
		}

		return serializeArray(array, cache, buffer, base)

	case map[string]interface{}:
		properties := make([]objectEntry, 0, len(v))
		for name, value := range v {
			properties = append(properties, objectEntry{name, value})
		}

		sort.Slice(properties, func(i, j int) bool { return properties[i].name < properties[j].name })

		return serializeObject(properties, cache, buffer, base)

	case Object:
		return v.Serialize(cache, buffer, base)

	case []byte:
		buffer.WriteByte(typeBinaryFull)
		writeBytes(v, buffer)

	case Blob:
		buffer.WriteByte(typeBinaryFull)
		writeBytes(v.Value(), buffer)

	default:
		return offset, fmt.Errorf("json: unsupported data type %T", v)
	}

	return offset, nil
}

func serializeString(v string, cache *encodingCache, buffer *bytes.Buffer, base int32) int32 {
	offset := base + int32(buffer.Len())

	// Perform string interning, returning the offset an existing identical string if any found.
	if existing := cache.CacheString(v, offset); existing != offset {
		return existing
	}

	// Check for integers represented as strings that have more compact binary encoding.
	if i, err := strconv.Atoi(v); err == nil && strconv.Itoa(i) == v {
		buffer.WriteByte(typeStringInt)
		writeVarInt(int64(i), buffer)
		return offset
	}

	// A new, unique string found. Write type, length and UTF-8 encoded string.
	buffer.WriteByte(typeString)
	writeString(v, buffer)
	return offset
}

func serializeBool(v bool, _ *encodingCache, buffer *bytes.Buffer, base int32) int32 {
	offset := base + int32(buffer.Len())

	t := int32(typeFalse)
	if v {
		t = typeTrue
	}

	// If this is not the first JSON element of the document, return an offset embedding the element.
	if offset > base {
		return -t
	}

	buffer.WriteByte(byte(t))
	return offset
}

func serializeNumber(v json.Number, cache *encodingCache, buffer *bytes.Buffer, base int32) int32 {
	offset := base + int32(buffer.Len())

	// Check for an earlier serialization of the number.
	if existing := cache.CacheNumber(v.String(), offset); existing != offset {
		return existing
	}

	// A new, unique number found. Write type and string encoded number.
	buffer.WriteByte(typeNumber)
	writeString(v.String(), buffer)
	return offset
}

func serializeArray(v []interface{}, cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	offset := base + int32(buffer.Len())

	// Write type, # of array elements.
	buffer.WriteByte(typeArray)

	l := make([]byte, binary.MaxVarintLen64)
	ll := binary.PutVarint(l, int64(len(v)))
	buffer.Write(l[0:ll])

	// Write offset placeholders.
	offsets := buffer.Len()
	buffer.Write(make([]byte, 4*len(v)))

	// Write array elements, updating the offset placeholders while progress is made.
	for _, element := range v {
		elemOffset, err := serialize(element, cache, buffer, base)
		if err != nil {
			return offset, err
		}

		order.PutUint32(buffer.Bytes()[offsets:offsets+4], uint32(elemOffset))
		offsets += 4
	}

	return offset, nil
}

func serializeObject(v []objectEntry, cache *encodingCache, buffer *bytes.Buffer, base int32) (int32, error) {
	offset := base + int32(buffer.Len())

	// Check for an earlier use of similar object type.
	if toffset := cache.CacheObjectType(v, offset); toffset == offset {
		// First use of object type. Write the full object description. Start with the type.
		buffer.WriteByte(typeObjectFull)

		// Write # of properties.
		writeVarInt(int64(len(v)), buffer)

		// Write property offset placeholders.
		keyOffsets := buffer.Len()
		buffer.Write(make([]byte, 4*len(v)))

		// Write value offset placeholders.
		valueOffsets := buffer.Len()
		buffer.Write(make([]byte, 4*len(v)))

		// Write property names, updating the offset placeholders while doing so.
		for i := range v {
			nameOffset := base + int32(buffer.Len())
			writeString(v[i].name, buffer)
			order.PutUint32(buffer.Bytes()[keyOffsets:keyOffsets+4], uint32(nameOffset))
			keyOffsets += 4
		}

		// Write the property values, updating the offset placeholders after each.
		for i := range v {
			element := v[i].value

			valueOffset, err := serialize(element, cache, buffer, base)
			if err != nil {
				return offset, err
			}

			order.PutUint32(buffer.Bytes()[valueOffsets:valueOffsets+4], uint32(valueOffset))
			valueOffsets += 4
		}
	} else {
		// Object type used earlier. Write a thin object description. Start with the type.
		buffer.WriteByte(typeObjectThin)

		// Write offset to the full object type.
		x := make([]byte, 4)
		order.PutUint32(x, uint32(toffset))
		buffer.Write(x)

		// Write value offset placeholders.
		valueOffsets := buffer.Len()
		buffer.Write(make([]byte, 4*len(v)))

		// Write property values, updating the value offsets after each.
		for i := range v {
			element := v[i].value
			valueOffset, err := serialize(element, cache, buffer, base)
			if err != nil {
				return offset, err
			}

			order.PutUint32(buffer.Bytes()[valueOffsets:valueOffsets+4], uint32(valueOffset))
			valueOffsets += 4
		}
	}

	return offset, nil
}

func readByte(content *utils.MultiReader, offset int64) (byte, error) {
	p, err := content.Bytes(offset, 1)
	if len(p) < 1 {
		return 0, err
	}

	return p[0], nil
}

// snapshotReader implements contentReader for the binary encoded JSON snapshot.
type snapshotReader struct {
	content *utils.MultiReader
}

func newSnapshotReader(content *utils.MultiReader) *snapshotReader {
	return &snapshotReader{content: content}
}

func readType(content *utils.MultiReader, offset int64) (int, error) {
	t, err := content.Bytes(offset, 1)
	if len(t) < 1 {
		return 0, err
	}

	return int(t[0]), nil
}

func (s *snapshotReader) ReadType(offset int64) (int, error) {
	return readType(s.content, offset)
}

func readVarInt(content *utils.MultiReader, offset int64) (int64, error) {
	return newBinaryReader(content, offset).ReadVarint()
}

func (s *snapshotReader) ReadVarInt(offset int64) (int64, error) {
	return readVarInt(s.content, offset+1)
}

func readFloat(content *utils.MultiReader, offset int64) (float64, error) {
	f := make([]byte, 8)
	n, err := content.ReadAt(f, offset)
	if n < len(f) {
		return 0, err
	}
	return math.Float64frombits(order.Uint64(f)), nil
}

func (s *snapshotReader) ReadFloat(offset int64) (float64, error) {
	return readFloat(s.content, offset+1)
}

func readBytes(content *utils.MultiReader, offset int64) ([]byte, error) {
	reader := newBinaryReader(content, offset)
	n, err := reader.ReadVarint()
	if err != nil {
		return nil, err
	}

	if n == 0 {
		return nil, nil
	}

	if n < 0 {
		return nil, fmt.Errorf("byte array length invalid")
	}

	p, err := content.Bytes(reader.Offset(), int(n))
	if len(p) < int(n) {
		return nil, fmt.Errorf("byte array not read: %w", err)
	}

	return p, nil
}

func compareBytes(content *utils.MultiReader, offset int64, s []byte) (int, error) {
	reader := newBinaryReader(content, offset)
	n, err := reader.ReadVarint()
	if err != nil {
		return 0, err
	}

	if n < 0 {
		return 0, fmt.Errorf("byte array length invalid")
	}

	if n == 0 {
		return bytes.Compare([]byte{}, s), nil
	}

	p, err := content.Bytes(reader.Offset(), int(n))
	if len(p) < int(n) {
		return 0, fmt.Errorf("byte array not read: %w", err)
	}

	return bytes.Compare(p, s), nil
}

func readString(content *utils.MultiReader, offset int64) (string, error) {
	reader := newBinaryReader(content, offset)
	n, err := reader.ReadVarint()
	if err != nil {
		return "", err
	}

	if n == 0 {
		return "", nil
	}

	if n < 0 {
		return "", fmt.Errorf("string length invalid")
	}

	p, err := content.Bytes(reader.Offset(), int(n))
	if len(p) < int(n) {
		return "", fmt.Errorf("string not read: %w", err)
	}

	return string(p), nil
}

func (s *snapshotReader) ReadBytes(offset int64) ([]byte, error) {
	return readBytes(s.content, offset+1)
}

func (s *snapshotReader) ReadString(offset int64) (string, error) {
	return readString(s.content, offset+1)
}

func (s *snapshotReader) ReadArray(offset int64) (arrayReader, error) {
	return readArray(s.content, offset)
}

func readArray(content *utils.MultiReader, offset int64) (arrayReader, error) {
	return newSnapshotArrayReader(content, offset)
}

func (s *snapshotReader) ReadObject(offset int64) (objectReader, error) {
	return readObject(s.content, offset)
}

func (s *snapshotReader) Reader() *utils.MultiReader {
	return s.content
}

func (s snapshotReader) ReadAt(p []byte, off int64) (n int, err error) {
	return s.content.ReadAt(p, off)
}

func (s *snapshotReader) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, newBinaryReader(s.content, 0))
}

func readObject(content *utils.MultiReader, offset int64) (objectReader, error) {
	return newSnapshotObjectReader(content, offset)
}

// snapshotArrayReader implements contentArrayReader for the binary encoded JSON snapshot.
type snapshotArrayReader struct {
	content *utils.MultiReader
	n       int
	offsets int64 // The offset to the beginning of the value offsets.
}

func newSnapshotArrayReader(content *utils.MultiReader, offset int64) (arrayReader, error) {
	reader := newBinaryReader(content, offset+1)
	n, err := reader.ReadVarint()
	if err != nil {
		return nil, err
	}

	if n < 0 {
		return nil, fmt.Errorf("array length invalid")
	}

	return &snapshotArrayReader{content: content, n: int(n), offsets: reader.Offset()}, nil
}

func (s *snapshotArrayReader) ReadType(offset int64) (int, error) {
	return readType(s.content, offset)
}

func (s *snapshotArrayReader) ReadVarInt(offset int64) (int64, error) {
	return readVarInt(s.content, offset+1)
}

func (s *snapshotArrayReader) ReadFloat(offset int64) (float64, error) {
	return readFloat(s.content, offset+1)
}

func (s *snapshotArrayReader) ReadBytes(offset int64) ([]byte, error) {
	return readBytes(s.content, offset+1)
}

func (s *snapshotArrayReader) ReadString(offset int64) (string, error) {
	return readString(s.content, offset+1)
}

func (s *snapshotArrayReader) ReadArray(offset int64) (arrayReader, error) {
	return newSnapshotArrayReader(s.content, offset)
}

func (s *snapshotArrayReader) ReadObject(offset int64) (objectReader, error) {
	return newSnapshotObjectReader(s.content, offset)
}

func (s *snapshotArrayReader) ArrayLen() (int, error) {
	return s.n, nil
}

func (s *snapshotArrayReader) ArrayValueOffset(i int) (int64, error) {
	boffset, err := s.content.Bytes(s.offsets+int64(i*4), 4)
	if len(boffset) < 4 {
		return 0, fmt.Errorf("array offset not read: %w", err)
	}

	offset := int64(int32(order.Uint32(boffset)))
	return offset, nil
}

func (s *snapshotArrayReader) Reader() *utils.MultiReader {
	return s.content
}

func (s *snapshotArrayReader) ReadAt(p []byte, off int64) (n int, err error) {
	return s.content.ReadAt(p, off)
}

func (s *snapshotArrayReader) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, newBinaryReader(s.content, 0))
}

// snapshotObjectReader implements contentObjectReader for the binary encoded JSON snapshot.
type snapshotObjectReader struct {
	content  *utils.MultiReader
	n        int   // # of properties.
	noffsets int64 // Offset to the beginning of the name offsets.
	voffsets int64 // Offset to the beginning of the value offsets.
}

func newSnapshotObjectReader(content *utils.MultiReader, offset int64) (objectReader, error) {
	t, err := readType(content, offset)
	if err != nil {
		return nil, err
	}

	if t == typeObjectFull {
		reader := newBinaryReader(content, offset+1)
		n, err := reader.ReadVarint()
		if err != nil {
			return nil, err
		}

		if n < 0 {
			return nil, fmt.Errorf("object (full) length invalid")
		}

		noffsets := reader.Offset()
		voffsets := noffsets + 4*n

		return &snapshotObjectReader{content: content, n: int(n), noffsets: noffsets, voffsets: voffsets}, nil
	} else if t == typeObjectThin {
		p, err := content.Bytes(offset+1, 4)
		if len(p) < 4 {
			return nil, fmt.Errorf("object (thin) full offset not read: %w", err)
		}

		reader := newBinaryReader(content, offset+1+int64(len(p)))

		fullOffset := int32(order.Uint32(p))
		freader := newBinaryReader(content, int64(fullOffset)+1)
		n, err := freader.ReadVarint()
		if err != nil {
			return nil, err
		}

		if n < 0 {
			return nil, fmt.Errorf("object (thin) length invalid")
		}

		noffsets := freader.Offset()
		voffsets := reader.Offset()

		return &snapshotObjectReader{content: content, n: int(n), noffsets: noffsets, voffsets: voffsets}, nil
	} else {
		return nil, fmt.Errorf("unknown object type: %d", t)
	}
}

func (s *snapshotObjectReader) ReadType(offset int64) (int, error) {
	return readType(s.content, offset)
}

func (s *snapshotObjectReader) ReadVarInt(offset int64) (int64, error) {
	return readVarInt(s.content, offset+1)
}

func (s *snapshotObjectReader) ReadFloat(offset int64) (float64, error) {
	return readFloat(s.content, offset+1)
}

func (s *snapshotObjectReader) ReadBytes(offset int64) ([]byte, error) {
	return readBytes(s.content, offset+1)
}

func (s *snapshotObjectReader) ReadString(offset int64) (string, error) {
	return readString(s.content, offset+1)
}

func (s *snapshotObjectReader) ReadArray(offset int64) (arrayReader, error) {
	return newSnapshotArrayReader(s.content, offset)
}

func (s *snapshotObjectReader) ReadObject(offset int64) (objectReader, error) {
	return newSnapshotObjectReader(s.content, offset)
}

func (s *snapshotObjectReader) ObjectLen() int {
	return s.n
}

func (s *snapshotObjectReader) ObjectNames() ([]string, error) {
	names := make([]string, 0, s.n)

	for i := 0; i < s.n; i++ {
		boffset, err := s.content.Bytes(s.noffsets+int64(i*4), 4)
		if len(boffset) < 4 {
			return nil, fmt.Errorf("object name offset not read: %w", err)
		}

		offset := int64(int32(order.Uint32(boffset)))
		name, err := readString(s.content, offset)
		if err != nil {
			return nil, fmt.Errorf("object name offset not read: %w", err)
		}

		names = append(names, name)
	}

	return names, nil
}

func (s *snapshotObjectReader) ObjectNamesIndex(i int) (string, error) {
	boffset, err := s.content.Bytes(s.noffsets+int64(i*4), 4)
	if len(boffset) < 4 {
		return "", fmt.Errorf("object name offset not read: %w", err)
	}
	offset := int64(int32(order.Uint32(boffset)))
	name, err := readString(s.content, offset)
	if err != nil {
		return "", fmt.Errorf("object name offset not read: %w", err)
	}
	return name, nil
}

func (s *snapshotObjectReader) objectNameIndex(name string) (int, bool, error) {
	var nestedErr error

	nameB := readOnlyStringBytes(name)

	// golang sort.Search implementation embedded here to assist compiler in inlining.
	i, j := 0, s.n
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j

		// begin f(h):
		var boffset []byte
		boffset, nestedErr = s.content.Bytes(s.noffsets+int64(h*4), 4)
		if nestedErr != nil {
			break
		}

		var ret bool
		if len(boffset) >= 4 {
			offset := int64(int32(order.Uint32(boffset)))
			var r int
			r, nestedErr = compareBytes(s.content, offset, nameB)
			if nestedErr != nil {
				break
			}

			ret = r >= 0
		}

		// end f(h)
		if !ret {
			i = h + 1 // preserves f(i-1) == false
		} else {
			j = h // preserves f(j) == true
		}
	}

	if nestedErr != nil {
		return 0, false, nestedErr
	}

	if i >= s.n {
		return 0, false, nil
	}

	boffset, err := s.content.Bytes(s.noffsets+int64(i*4), 4)
	if len(boffset) < 4 {
		return 0, false, err
	}

	offset := int64(int32(order.Uint32(boffset)))
	r, err := compareBytes(s.content, offset, nameB)
	if err != nil {
		return 0, false, err
	}

	if r != 0 {
		return 0, false, nil
	}

	return i, true, nil
}

func (s *snapshotObjectReader) ObjectNameOffset(name string) (int64, bool, error) {
	i, ok, err := s.objectNameIndex(name)
	if err != nil || !ok {
		return 0, ok, err
	}

	boffset, err := s.content.Bytes(s.noffsets+int64(i*4), 4)
	if len(boffset) < 4 {
		return 0, false, fmt.Errorf("object name offset not read: %w", err)
	}

	return int64(int32(order.Uint32(boffset))), true, nil
}

func (s *snapshotObjectReader) ObjectValueOffset(name string) (int64, bool, error) {
	i, ok, err := s.objectNameIndex(name)
	if err != nil || !ok {
		return 0, ok, err
	}

	boffset, err := s.content.Bytes(s.voffsets+int64(i*4), 4)
	if len(boffset) < 4 {
		return 0, false, fmt.Errorf("object value offset not read: %w", err)
	}

	return int64(int32(order.Uint32(boffset))), true, nil
}

func (s *snapshotObjectReader) objectNameValueOffsets() ([]objectEntry, []int64, error) {
	properties := make([]objectEntry, s.n)
	offsets := make([]int64, s.n)

	for i := 0; i < s.n; i++ {
		boffset, err := s.content.Bytes(s.noffsets+int64(i*4), 4)
		if len(boffset) < 4 {
			return nil, nil, fmt.Errorf("object name offset not read: %w", err)
		}

		offset := int64(int32(order.Uint32(boffset)))
		name, err := readString(s.content, offset)
		if err != nil {
			return nil, nil, fmt.Errorf("object name offset not read: %w", err)
		}

		boffset, err = s.content.Bytes(s.voffsets+int64(i*4), 4)
		if len(boffset) < 4 {
			return nil, nil, fmt.Errorf("object value offset not read: %w", err)
		}

		properties[i] = objectEntry{name, nil}
		offsets[i] = int64(int32(order.Uint32(boffset)))
	}

	return properties, offsets, nil
}

func (s *snapshotObjectReader) objectNameOffsetsValueOffsets() ([]objectEntry, []int64, []int64, error) {
	properties := make([]objectEntry, s.n)
	noffsets := make([]int64, s.n)
	voffsets := make([]int64, s.n)

	for i := 0; i < s.n; i++ {
		boffset, err := s.content.Bytes(s.noffsets+int64(i*4), 4)
		if len(boffset) < 4 {
			return nil, nil, nil, fmt.Errorf("object name offset not read: %w", err)
		}

		offset := int64(int32(order.Uint32(boffset)))
		noffsets[i] = offset
		name, err := readString(s.content, offset)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("object name offset not read: %w", err)
		}

		boffset, err = s.content.Bytes(s.voffsets+int64(i*4), 4)
		if len(boffset) < 4 {
			return nil, nil, nil, fmt.Errorf("object value offset not read: %w", err)
		}

		properties[i] = objectEntry{name, nil}
		voffsets[i] = int64(int32(order.Uint32(boffset)))
	}

	return properties, noffsets, voffsets, nil
}

func (s *snapshotObjectReader) Reader() *utils.MultiReader {
	return s.content
}

func (s *snapshotObjectReader) ReadAt(p []byte, off int64) (n int, err error) {
	return s.content.ReadAt(p, off)
}

func (s *snapshotObjectReader) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, newBinaryReader(s.content, 0))
}

// writeString writes a variable length encoded string to buffer, returning the offset of the string start.
func writeString(s string, buffer *bytes.Buffer) {
	writeBytes(readOnlyStringBytes(s), buffer)
}

// writeBytes writes a variable length encoded byte array to buffer, returning the offset of the string start.
func writeBytes(p []byte, buffer *bytes.Buffer) {
	l := make([]byte, binary.MaxVarintLen64)

	ll := binary.PutVarint(l, int64(len(p)))
	buffer.Write(l[0:ll])
	buffer.Write(p)
}

func writeVarInt(v int64, buffer *bytes.Buffer) {
	l := make([]byte, binary.MaxVarintLen64)
	ll := binary.PutVarint(l, v)
	buffer.Write(l[0:ll])
}

// binaryReader is a facade over an utils.MultiReader to provide io.Reader compatible interface. This is a mere convenience util.
type binaryReader struct {
	source *utils.MultiReader
	offset int64
}

func newBinaryReader(source *utils.MultiReader, offset int64) *binaryReader {
	return &binaryReader{source: source, offset: offset}
}

func (br *binaryReader) Read(p []byte) (int, error) {
	n, err := br.source.ReadAt(p, br.offset)
	br.offset += int64(n)
	return n, err
}

func (br *binaryReader) ReadByte() (byte, error) {
	p, err := br.source.Bytes(br.offset, 1)
	if len(p) < 1 {
		return 0, err
	}

	br.offset += int64(len(p))
	return p[0], nil
}

func (br *binaryReader) Offset() int64 {
	return br.offset
}

var errOverflow = errors.New("binary: varint overflows a 64-bit integer")

// ReadUvarint reads an encoded unsigned integer from r and returns it as a uint64.
func (br *binaryReader) ReadUvarint() (uint64, error) {
	var x uint64
	var s uint
	for i := 0; ; i++ {
		b, err := br.ReadByte()
		if err != nil {
			return x, err
		}
		if b < 0x80 {
			if i > 9 || i == 9 && b > 1 {
				return x, errOverflow
			}
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
}

// ReadVarint reads an encoded signed integer from r and returns it as an int64.
func (br *binaryReader) ReadVarint() (int64, error) {
	ux, err := br.ReadUvarint() // ok to continue in presence of error
	x := int64(ux >> 1)
	if ux&1 != 0 {
		x = ^x
	}
	return x, err
}
