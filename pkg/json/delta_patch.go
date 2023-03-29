package json

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/styrainc/load-private/pkg/json/internal/utils"
)

// deltaPatch transforms a JSON patch to a delta to apply over a snapshot or merges the JSON patch to an existing delta.
type deltaPatch struct {
	snapshot   *utils.MultiReader // Content without any deltas. Not modified.
	slen       int64
	delta      *utils.MultiReader // Plain deltas. As patches are applied, this is appended with new patches.
	content    *utils.MultiReader // Entire content, a mere facade over the snapshot and deltas.
	patches    map[int64]int64
	dependency interface{} // Storage level object
}

func newDeltaPatch(snapshot *utils.MultiReader, slen int64, delta *utils.MultiReader, objects []interface{}) *deltaPatch {
	if delta == nil {
		buffer := new(bytes.Buffer)
		buffer.Write(make([]byte, 4))
		order.PutUint32(buffer.Bytes()[deltaHeaderOffsetOffset:], uint32(buffer.Len()))

		// Write the patch header. Sort the patches as per their original offset.

		n := make([]byte, binary.MaxVarintLen64)
		buffer.Write(n[0:binary.PutVarint(n, 0)])

		delta := utils.NewMultiReaderFromBytesReader(utils.NewBytesReader(buffer.Bytes()))

		return &deltaPatch{
			snapshot:   snapshot,
			slen:       slen,
			delta:      delta,
			content:    utils.NewMultiReaderFromMultiReaders(snapshot, 0, slen, delta),
			patches:    make(map[int64]int64),
			dependency: objects[0],
		}
	}

	reader, err := newDeltaReader(snapshot, slen, delta)
	checkError(err)

	content := utils.NewMultiReaderFromMultiReaders(snapshot, 0, slen, delta)
	return &deltaPatch{
		snapshot:   snapshot,
		slen:       slen,
		delta:      delta,
		content:    content,
		patches:    reader.Patches(),
		dependency: objects[1],
	}
}

func (d *deltaPatch) ReadType(offset int64) (int, error) {
	return readType(d.content, d.offset(offset))
}

func (d *deltaPatch) ReadFloat(offset int64) (float64, error) {
	return readFloat(d.content, d.offset(offset)+1)
}

func (d *deltaPatch) ReadVarInt(offset int64) (int64, error) {
	return readVarInt(d.content, d.offset(offset)+1)
}

func (d *deltaPatch) ReadBytes(offset int64) ([]byte, error) {
	return readBytes(d.content, d.offset(offset)+1)
}

func (d *deltaPatch) ReadString(offset int64) (string, error) {
	return readString(d.content, d.offset(offset)+1)
}

func (d *deltaPatch) ReadArray(offset int64) (arrayReader, error) {
	return newDeltaPatchArrayReader(d, offset)
}

func (d *deltaPatch) ReadObject(offset int64) (objectReader, error) {
	return newDeltaPatchObjectReader(d, offset)
}

func (d *deltaPatch) WriteTo(w io.Writer) (int64, error) {
	n, err := io.Copy(w, newBinaryReader(d.snapshot, 0))
	if err != nil {
		return n, err
	}

	r, err := d.serialize()
	if err != nil {
		return n, err
	}

	data, _ := r.Bytes(0, r.Len())
	m, err := io.Copy(w, bytes.NewReader(data))
	return n + m, err
}

func (d *deltaPatch) WriteBlob(name string, value Blob) *snapshot {
	path := d.create(name)
	obj, _ := d.check(name)

	// Remove any children existing before writing the blob.
	for _, child := range obj.Names() {
		if strings.HasPrefix(child, "data:") {
			err := d.apply(Patch{
				Op{
					Op:   PatchOpRemove,
					Path: NewPointer(append(path, child)),
				},
			})
			checkError(err)
		}
	}

	err := d.apply(Patch{
		Op{
			Op:    PatchOpAdd,
			Path:  NewPointer(append(path, "kind")),
			Value: NewString(fmt.Sprintf("%d", Unstructured)),
		},
		Op{
			Op:          PatchOpAdd,
			Path:        NewPointer(append(path, "data")),
			valueBinary: value,
		},
	})
	checkError(err)
	return d.collections()
}

func (d *deltaPatch) WriteJSON(name string, value Json) *snapshot {
	path := d.create(name)
	obj, _ := d.check(name)

	// Remove any children existing before writing the JSON.
	for _, child := range obj.Names() {
		if strings.HasPrefix(child, "data:") {
			err := d.apply(Patch{
				Op{
					Op:   PatchOpRemove,
					Path: NewPointer(append(path, child)),
				},
			})
			checkError(err)
		}
	}

	err := d.apply(Patch{
		Op{
			Op:    PatchOpAdd,
			Path:  NewPointer(append(path, "kind")),
			Value: NewString(fmt.Sprintf("%d", JSON)),
		},
		Op{
			Op:    PatchOpAdd,
			Path:  NewPointer(append(path, "data")),
			Value: value,
		},
	})
	checkError(err)
	return d.collections()
}

func (d *deltaPatch) PatchJSON(name string, patch Patch) (bool, *snapshot, error) {
	obj, path := d.check(name)
	if obj == nil {
		return false, nil, nil
	}

	// Modify the operations to include the path to the resource.
	for i := range patch {
		p, err := ParsePointer(patch[i].Path)
		if err != nil {
			return false, nil, err
		}

		patch[i].Path = NewPointer(append(path, append([]string{"data"}, p...)...))
	}

	if err := d.apply(patch); err != nil {
		return false, nil, err
	}

	return true, d.collections(), nil
}

func (d *deltaPatch) WriteDirectory(name string) *snapshot {
	path := d.create(name)
	obj, _ := d.check(name)

	if obj.Value("kind") != nil {
		err := d.apply(Patch{
			Op{
				Op:   PatchOpRemove,
				Path: NewPointer(append(path, "data")),
			},
			Op{
				Op:   PatchOpRemove,
				Path: NewPointer(append(path, "kind")),
			},
		})
		checkError(err)
	}
	return d.collections()
}

// Remove removes a resource. It returns true if removed.
func (d *deltaPatch) Remove(name string) (bool, *snapshot) {
	obj, path := d.check(name)
	if obj == nil {
		return false, d.collections()
	}

	// Only remove directory if it's empty.
	if kindImpl(obj) == Directory {
		for _, name := range obj.Names() {
			if strings.HasPrefix(name, "data:") {
				return false, d.collections()
			}
		}
	}

	err := d.apply(Patch{
		Op{
			Op:   PatchOpRemove,
			Path: NewPointer(path),
		},
	})
	checkError(err)
	return true, d.collections()
}

// WriteMeta updates the metadata for an existing resource.
func (d *deltaPatch) WriteMeta(name string, key string, value string) (bool, *snapshot) {
	obj, path := d.check(name)
	if obj == nil {
		return false, nil
	}

	err := d.apply(Patch{
		Op{
			Op:    PatchOpAdd,
			Path:  NewPointer(append(path, fmt.Sprintf("meta:%s", key))),
			Value: NewString(value),
		},
	})
	checkError(err)
	return true, d.collections()
}

func (d *deltaPatch) offset(offset int64) int64 {
	v, ok := d.patches[offset]
	if ok {
		return v
	}
	return offset
}

// check checks the resource is there.
func (d *deltaPatch) check(name string) (Object, []string) {
	var obj Object = newObject(d, 0)

	pathSegments := PathSegments(name)
	path := make([]string, 0, len(pathSegments))
	for _, seg := range pathSegments {
		path = append(path, "data:"+seg)

		if kindImpl(obj) != Directory {
			return nil, nil
		}

		child := obj.Value("data:" + seg)
		if child == nil {
			return nil, nil
		}

		if _, ok := child.(Object); !ok {
			return nil, nil
		}

		obj = child.(Object)
	}

	return obj, path
}

// create creates the directory for the file name but doesn't set any properties for the file itself. It reinvokes itself after each modification it does.
func (d *deltaPatch) create(name string) []string {
	var obj Object = newObject(d, 0)

	segs := PathSegments(name)
	var path []string
	for i := 0; i < len(segs); i++ {
		// Non-leafs in the hierarchy must be directories.

		if i < len(segs)-1 && kindImpl(obj) != Directory {
			err := d.apply(Patch{
				Op{
					Op:    PatchOpReplace,
					Path:  NewPointer(path),
					Value: NewObject(map[string]File{}),
				},
			})
			checkError(err)
			return d.create(name)
		}

		// Proceed one level deeper in the hierarchy. Removing any non-directory child.

		if obj.Value("data") != nil {
			err := d.apply(Patch{
				Op{
					Op:   PatchOpRemove,
					Path: NewPointer(append(path, "kind")),
				},
				Op{
					Op:   PatchOpRemove,
					Path: NewPointer(append(path, "data")),
				},
			})
			checkError(err)
		}

		// Add the child.

		seg := "data:" + segs[i]
		path = append(path, seg)

		child := obj.Value(seg)
		if child == nil {
			err := d.apply(Patch{
				Op{
					Op:    PatchOpAdd,
					Path:  NewPointer(path),
					Value: NewObject(map[string]File{}),
				},
			})
			checkError(err)
			return d.create(name)
		}

		obj = child.(Object)
	}

	return path
}

// apply applies the patch. After an error, the state of delta patch has the patch partially applied.
func (d *deltaPatch) apply(patch Patch) error {
	for _, op := range patch {
		// Find the existing, overlapping patch, if any, by traversing into the exact portion of the document to be patched.
		// If an add operation, the last segment doesn't have to exist as the patch can add that so special case it.

		var path string
		var offset int64
		var v File
		var err error

		switch op.Op {
		case PatchOpCreate:
			p, perr := ParsePointer(op.Path)
			if perr != nil {
				return perr
			}

			// Create any missing enclosing container.

			_, _, value, serr := d.search("")
			if serr != nil {
				return serr
			}

			for i, seg := range p[0 : len(p)-1] {
				value, _, err = d.traverse(value, seg)
				if err != nil {
					original := op.Value

					op.Path = NewPointer(p[0 : i+1])
					parent := NewObject(nil)
					op.Value = parent

					for _, seg := range p[i+1 : len(p)-1] {
						child := NewObject(nil)
						if _, ok := parent.Set(seg, child); ok {
							panic("not reached")
						}
						parent = child
					}

					if _, ok := parent.Set(p[len(p)-1], original); ok {
						panic("not reached")
					}
					break
				}
			}

			fallthrough

		case PatchOpAdd:
			p, perr := ParsePointer(op.Path)
			if perr != nil {
				return perr
			}

			if len(p) >= 1 {
				path, offset, v, err = d.search(NewPointer(p[0 : len(p)-1]))
			}

			// Try finding the exact referred entity, in case add is a replace.
			if p2, o2, v2, err2 := d.search(op.Path); err2 == nil {
				path, offset, v, err = p2, o2, v2, err2
			}

		case PatchOpReplace:
			path, offset, v, err = d.search(op.Path)

		case PatchOpRemove:
			p, perr := ParsePointer(op.Path)
			if perr != nil {
				return perr
			}

			if len(p) == 0 {
				// Cannot remove the whole document.
				return fmt.Errorf("json: invalid patch")
			}

			path, offset, v, err = d.search(NewPointer(p[0 : len(p)-1]))

		default:
			return fmt.Errorf("json: only add, create, replace, and remove supported")
		}

		if err != nil {
			return err
		}

		// Operation addresses either a) exactly the same path already patched, b) subset of a patch, or c) there are potential patches deeper in the document.
		// Either way, traverse the document to identify any patches that can be removed after adding the new patch. Note, the patches can't be removed before
		// actual patching as the patches are required to reconstruct the JSON to patch.

		removed := make(map[int64]struct{})
		if err := d.remove(offset, removed); err != nil {
			return err
		}

		op.Path = strings.TrimPrefix(op.Path, path)
		d.patches[offset], err = d.applyOp(v, op)
		if err != nil {
			// TODO: Non-atomic update.
			return err
		}

		delete(removed, offset)

		for offset := range removed {
			delete(d.patches, offset)
		}
	}

	return nil
}

// search returns the nested element 'seg' within.
func (d *deltaPatch) search(path string) (string, int64, File, error) {
	ptr, err := ParsePointer(path)
	if err != nil {
		return "", 0, nil, err
	}

	offset := int64(0)
	var v File = newObject(d, offset)
	if _, ok := d.patches[offset]; ok {
		return "", offset, v, nil
	}

	for i, seg := range ptr {
		if _, ok := d.patches[offset]; ok {
			return NewPointer(ptr[0:i]), offset, v, nil
		}

		if _, ok := v.(Array); ok && i == len(ptr)-1 {
			break
		}

		v, offset, err = d.traverse(v, seg)
		if err != nil {
			break
		}
	}

	return path, offset, v, err
}

// traverse returns the nested element 'seg' within.
func (d *deltaPatch) traverse(v File, seg string) (File, int64, error) {
	switch j := v.(type) {
	case ArrayBinary:
		i, err := parseInt(seg)
		if err != nil {
			return nil, 0, err
		}

		if i < 0 || i >= j.Len() {
			return nil, 0, fmt.Errorf("json: path not found")
		}

		offset, err := j.content.ArrayValueOffset(i)
		if err != nil {
			return nil, 0, err
		}

		return newFile(j.content, offset), offset, nil

	case ObjectBinary:
		offset, ok, err := j.content.ObjectValueOffset(seg)
		if err != nil {
			return nil, 0, err
		}

		if !ok {
			return nil, 0, fmt.Errorf("json: path not found")
		}

		return newFile(j.content, offset), offset, nil

	default:
		return nil, 0, fmt.Errorf("invalid patch")
	}
}

func (d *deltaPatch) applyOp(v File, op Op) (int64, error) {
	patched, err := op.applyTo(v)
	if err != nil {
		return 0, err
	}

	// Append the changed offset to the end of the delta.

	var buffer bytes.Buffer
	if _, err := serialize(patched, newEncodingCache(), &buffer, int32(d.slen+int64(d.delta.Len()))); err != nil {
		return 0, err
	}

	offset := d.slen + int64(d.delta.Len())

	d.delta.Append(buffer.Bytes())
	return offset, nil
}

// walk recursively deletes the patches for the given json entity and offset.
func (d *deltaPatch) remove(offset int64, removed map[int64]struct{}) error {
	if offset < 0 {
		// Embedded type, nothing to remove.
		return nil
	}

	t, err := d.ReadType(offset)
	checkError(err)

	switch t {
	case typeArray:
		content, err := d.ReadArray(offset)
		checkError(err)

		l, err := content.ArrayLen()
		checkError(err)

		for i := 0; i < l; i++ {
			offset, err := content.ArrayValueOffset(i)
			if err != nil {
				return err
			}

			if err := d.remove(offset, removed); err != nil {
				return err
			}
		}

		removed[offset] = struct{}{}
		return nil

	case typeObjectFull, typeObjectThin, typeObjectPatch:
		content, err := d.ReadObject(offset)
		checkError(err)

		_, offsets, err := content.objectNameValueOffsets()
		checkError(err)

		for _, offset := range offsets {
			if err := d.remove(offset, removed); err != nil {
				return err
			}
		}

		removed[offset] = struct{}{}
		return nil

	default:
		removed[offset] = struct{}{}
		return nil
	}
}

func (d *deltaPatch) serialize() (*utils.BytesReader, error) {
	// Write a header offset holder, to be updated once the patch generation is complete.

	buffer := new(bytes.Buffer)
	buffer.Write(make([]byte, 4))

	// Compute the binary diffs for the patches. Note this also recomputes the existing delta patches that were not replaced.

	offsets := make([]int64, 0, len(d.patches))
	for off := range d.patches {
		offsets = append(offsets, off)
	}

	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })

	patches := make(map[int64]int64)
	encodingCache, hashCacheA := newEncodingCache(), newHashCache(d)

	for _, offset := range offsets {
		isRoot := offset == 0 // Root element of the document cannot use embedding.
		if _, _, err := diffImpl(newSnapshotReader(d.snapshot), offset, d.slen, d, offset, !isRoot, buffer, patches, encodingCache, hashCacheA, newHashCache(d)); err != nil {
			return nil, err
		}
	}

	// Update the header offset.

	order.PutUint32(buffer.Bytes()[deltaHeaderOffsetOffset:], uint32(buffer.Len()))

	// Write the patch header. Sort the patches as per their original offset.

	n := make([]byte, binary.MaxVarintLen64)
	buffer.Write(n[0:binary.PutVarint(n, int64(len(patches)))])

	offsets = make([]int64, 0, len(patches))
	for off := range patches {
		offsets = append(offsets, off)
	}

	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })

	for _, ooff := range offsets {
		writeInt32(buffer, int32(ooff))
	}

	for _, ooff := range offsets {
		writeInt32(buffer, int32(patches[ooff]))
	}

	return utils.NewBytesReader(buffer.Bytes()), nil
}

func (d *deltaPatch) collections() *snapshot {
	now := testTime
	if now.IsZero() {
		now = time.Now()
	}

	err := d.apply(d.setMetaRecursively(nil, newObject(d, 0), "timestamp", fmt.Sprintf("%d", now.UnixNano())))
	checkError(err)

	return &snapshot{ObjectBinary: newObject(d, 0), slen: d.slen, blen: int64(d.content.Len()), objects: []interface{}{nil, d.dependency}}
}

func (d *deltaPatch) setMetaRecursively(path []string, obj Object, key string, value string) Patch {
	var patches Patch

	if metaKey := fmt.Sprintf("meta:%s", key); obj.Value(metaKey) == nil {
		patches = Patch{
			Op{
				Op:    PatchOpAdd,
				Path:  NewPointer(append(path, fmt.Sprintf("meta:%s", key))),
				Value: NewString(value),
			},
		}
	}

	if kindImpl(obj) != Directory {
		return patches
	}

	for _, name := range obj.Names() {
		if !strings.HasPrefix(name, "data:") {
			continue
		}

		patches = append(patches, d.setMetaRecursively(append(path, name), obj.Value(name).(Object), key, value)...)
	}

	return patches
}

type deltaPatchArrayReader struct {
	deltaPatch
	impl arrayReader
}

func newDeltaPatchArrayReader(dr *deltaPatch, offset int64) (arrayReader, error) {
	impl, err := readArray(dr.content, dr.offset(offset))
	if err != nil {
		return nil, err
	}

	return &deltaPatchArrayReader{deltaPatch: *dr, impl: impl}, nil
}

func (d *deltaPatchArrayReader) ArrayLen() (int, error) {
	return d.impl.ArrayLen()
}

func (d *deltaPatchArrayReader) ArrayValueOffset(i int) (int64, error) {
	return d.impl.ArrayValueOffset(i)
}

type deltaPatchObjectReader struct {
	*deltaPatch
	impl objectReader
}

func newDeltaPatchObjectReader(d *deltaPatch, offset int64) (objectReader, error) {
	var impl objectReader
	var err error

	offset = d.offset(offset)

	var t byte
	t, err = readByte(d.content, offset)
	if err != nil {
		return nil, err
	}

	if t == typeObjectPatch {
		impl, err = newDeltaObjectReaderImpl(d, d.content, offset)
	} else {
		impl, err = readObject(d.content, offset)
	}

	if err != nil {
		return nil, err
	}

	return &deltaPatchObjectReader{deltaPatch: d, impl: impl}, nil
}

func (d *deltaPatchObjectReader) ObjectLen() int {
	return d.impl.ObjectLen()
}

func (d *deltaPatchObjectReader) ObjectNames() ([]string, error) {
	return d.impl.ObjectNames()
}

func (d *deltaPatchObjectReader) ObjectNamesIndex(i int) (string, error) {
	return d.impl.ObjectNamesIndex(i)
}

func (d *deltaPatchObjectReader) ObjectNameOffset(name string) (int64, bool, error) {
	return d.impl.ObjectNameOffset(name)
}

func (d *deltaPatchObjectReader) ObjectValueOffset(name string) (int64, bool, error) {
	return d.impl.ObjectValueOffset(name)
}

func (d *deltaPatchObjectReader) objectNameValueOffsets() ([]objectEntry, []int64, error) {
	return d.impl.objectNameValueOffsets()
}

func (d *deltaPatchObjectReader) objectNameOffsetsValueOffsets() ([]objectEntry, []int64, []int64, error) {
	return d.impl.objectNameOffsetsValueOffsets()
}
