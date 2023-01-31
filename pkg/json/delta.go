package json

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	"github.com/styrainc/load-private/pkg/json/internal/utils"
)

const (
	deltaHeaderOffsetOffset = 0
	deltaHeaderOffsetLen    = 4
)

// deltaReader implements the contentReader. Content holds the snapshot and the delta concatenated; the reader will then apply deltas (if any) to snapshot, on the fly.
type deltaReader struct {
	content   *utils.MultiReader // Snapshot and delta concatenated.
	snapshot  *utils.MultiReader // Snapshot binary encoded.
	delta     *utils.MultiReader // Delta binary encoded.
	slen      int64              // Snapshot length.
	dops      int                // Delta ops #.
	dooffsets int64              // Offset override start offset.
	dvoffsets int64              // Value override start offset.
}

func newDeltaReader(snapshot *utils.MultiReader, slen int64, delta *utils.MultiReader) (*deltaReader, error) {
	offset := make([]byte, deltaHeaderOffsetLen)
	m, err := delta.ReadAt(offset, deltaHeaderOffsetOffset)
	if err != nil {
		return nil, err
	}
	if m < len(offset) {
		return nil, fmt.Errorf("delta header offset invalid")
	}

	dhr := int64(int32(order.Uint32(offset)))

	reader := newBinaryReader(delta, dhr)
	dops, err := reader.ReadVarint()
	if err != nil {
		return nil, err
	}

	if dops < 0 {
		return nil, fmt.Errorf("delta length invalid")
	}

	dooffsets := reader.Offset()
	dvoffsets := dooffsets + 4*dops

	content := utils.NewMultiReaderFromMultiReaders(snapshot, 0, slen, delta)
	return &deltaReader{content: content, snapshot: snapshot, delta: delta, slen: slen, dops: int(dops), dooffsets: dooffsets + slen, dvoffsets: dvoffsets + slen}, nil
}

func (d *deltaReader) Patches() map[int64]int64 {
	deltas := make(map[int64]int64, d.dops)

	for i := 0; i < d.dops; i++ {
		b := make([]byte, 4)
		n, err := d.content.ReadAt(b, d.dooffsets+int64(i*4))
		if n < len(b) {
			checkError(err)
		}

		offset := int64(order.Uint32(b))

		n, err = d.content.ReadAt(b, d.dvoffsets+int64(i*4))
		if n < len(b) {
			checkError(err)
		}
		value := int64(int32(order.Uint32(b)))
		deltas[offset] = value
	}

	return deltas
}

func (d *deltaReader) deltaOffset(offset int64) int64 {
	if d.slen < offset {
		// Offset is outside of the snapshot; hence, cannot be patched.
		return offset
	}

	// golang sort.Search implementation embedded here to assist compiler in inlining.
	i, j := 0, d.dops
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j

		// start f(h)
		b, err := d.content.Bytes(d.dooffsets+int64(h*4), 4)
		if len(b) < 4 {
			checkError(err)
		}

		off := int64(int32(order.Uint32(b)))
		ret := off >= offset
		// end f(h)

		if !ret {
			i = h + 1 // preserves f(i-1) == false
		} else {
			j = h // preserves f(j) == true
		}
	}

	if i >= d.dops {
		return offset
	}

	b, err := d.content.Bytes(d.dooffsets+int64(i*4), 4)
	if len(b) < 4 {
		checkError(err)
	}

	if offset != int64(int32(order.Uint32(b))) {
		return offset
	}

	// Offset has an override.

	b, err = d.content.Bytes(d.dvoffsets+int64(i*4), 4)
	if len(b) < 4 {
		checkError(err)
	}
	off := int64(int32(order.Uint32(b)))
	return off
}

func (d *deltaReader) ReadType(offset int64) (int, error) {
	return readType(d.content, d.deltaOffset(offset))
}

func (d *deltaReader) ReadVarInt(offset int64) (int64, error) {
	return readVarInt(d.content, d.deltaOffset(offset)+1)
}

func (d *deltaReader) ReadFloat(offset int64) (float64, error) {
	return readFloat(d.content, d.deltaOffset(offset)+1)
}

func (d *deltaReader) ReadBytes(offset int64) ([]byte, error) {
	return readBytes(d.content, d.deltaOffset(offset)+1)
}

func (d *deltaReader) ReadString(offset int64) (string, error) {
	return readString(d.content, d.deltaOffset(offset)+1)
}

func (d *deltaReader) ReadArray(offset int64) (arrayReader, error) {
	return newDeltaArrayReader(d, offset)
}

func (d *deltaReader) ReadObject(offset int64) (objectReader, error) {
	return newDeltaObjectReader(d, offset)
}

func (d *deltaReader) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, newBinaryReader(d.content, 0))
}

type deltaArrayReader struct {
	deltaReader
	impl arrayReader
}

func newDeltaArrayReader(dr *deltaReader, offset int64) (arrayReader, error) {
	impl, err := readArray(dr.content, dr.deltaOffset(offset))
	if err != nil {
		return nil, err
	}

	return &deltaArrayReader{deltaReader: *dr, impl: impl}, nil
}

func (d *deltaArrayReader) ArrayLen() (int, error) {
	return d.impl.ArrayLen()
}

func (d *deltaArrayReader) ArrayValueOffset(i int) (int64, error) {
	return d.impl.ArrayValueOffset(i)
}

type deltaObjectReader struct {
	deltaReader
	impl objectReader
}

func newDeltaObjectReader(d *deltaReader, offset int64) (objectReader, error) {
	var impl objectReader

	offset = d.deltaOffset(offset)

	t, err := readByte(d.content, offset)
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

	return &deltaObjectReader{deltaReader: *d, impl: impl}, nil
}

func (d *deltaObjectReader) ObjectLen() int {
	return d.impl.ObjectLen()
}

func (d *deltaObjectReader) ObjectNames() ([]string, error) {
	return d.impl.ObjectNames()
}

func (d *deltaObjectReader) ObjectNameOffset(name string) (int64, bool, error) {
	return d.impl.ObjectNameOffset(name)
}

func (d *deltaObjectReader) ObjectValueOffset(name string) (int64, bool, error) {
	return d.impl.ObjectValueOffset(name)
}

func (d *deltaObjectReader) objectNameValueOffsets() ([]objectEntry, []int64, error) {
	return d.impl.objectNameValueOffsets()
}

func (d *deltaObjectReader) objectNameOffsetsValueOffsets() ([]objectEntry, []int64, []int64, error) {
	return d.impl.objectNameOffsetsValueOffsets()
}

type deltaObjectReaderImpl struct {
	contentReader
	content  *utils.MultiReader
	sobj     objectReader
	n        int
	changed  int
	noffsets int64 // Offset to the beginning of the name offsets.
	voffsets int64 // Offset to the beginning of the value offsets.
}

func newDeltaObjectReaderImpl(r contentReader, content *utils.MultiReader, delta int64) (objectReader, error) {
	p := make([]byte, 4)
	m, err := content.ReadAt(p, delta+1)
	if m < len(p) {
		return nil, fmt.Errorf("object (path) full offset not read: %w", err)
	}
	fullOffset := int64(order.Uint32(p))

	reader := newBinaryReader(content, delta+1+4)
	n, err := reader.ReadVarint()
	if err != nil {
		return nil, err
	}

	if n < 0 {
		return nil, fmt.Errorf("object (patch) length invalid")
	}

	changed, err := reader.ReadVarint()
	if err != nil {
		return nil, err
	}

	if changed < 0 {
		return nil, fmt.Errorf("object (patch) change count invalid")
	}

	noffsets := reader.Offset()
	voffsets := noffsets + 4*changed

	sobj, err := newSnapshotObjectReader(content, fullOffset)
	if err != nil {
		return nil, err
	}

	return &deltaObjectReaderImpl{contentReader: r, content: content, sobj: sobj, n: int(n), changed: int(changed), noffsets: noffsets, voffsets: voffsets}, nil
}

func (d *deltaObjectReaderImpl) ObjectLen() int {
	return d.n
}

func (d *deltaObjectReaderImpl) ObjectNames() ([]string, error) {
	names, err := d.sobj.ObjectNames()
	if err != nil {
		return nil, err
	}

	m := make(map[string]bool)

	for _, name := range names {
		m[name] = true
	}

	for i := 0; i < d.changed; i++ {
		noff, err := d.objectNameOffset(i)
		if err != nil {
			return nil, err
		}

		name, err := readString(d.content, noff)
		if err != nil {
			return nil, err
		}

		voff, err := d.objectValueOffset(i)
		if err != nil {
			return nil, err
		}

		if voff == -typeObjectPatch {
			// Negative offset indicates removed key-value pair.
			delete(m, name)
		} else {
			m[name] = true
		}
	}

	names = names[:0]

	for name := range m {
		names = append(names, name)
	}

	sort.Strings(names)

	return names, nil
}

func (d *deltaObjectReaderImpl) objectNameOffset(i int) (int64, error) {
	boffset := make([]byte, 4)

	n, err := d.content.ReadAt(boffset, d.noffsets+int64(i*4))
	if n < len(boffset) {
		return 0, fmt.Errorf("object name offset not read: %w", err)
	}

	return int64(int32(order.Uint32(boffset))), nil
}

func (d *deltaObjectReaderImpl) objectValueOffset(i int) (int64, error) {
	boffset := make([]byte, 4)

	n, err := d.content.ReadAt(boffset, d.voffsets+int64(i*4))
	if n < len(boffset) {
		return 0, fmt.Errorf("object value offset not read: %w", err)
	}

	return int64(int32(order.Uint32(boffset))), nil
}

func (d *deltaObjectReaderImpl) objectNameIndex(name string) (int, bool, error) {
	// golang sort.Search implementation embedded here to assist compiler in inlining.
	i, j := 0, d.changed
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j

		// begin f(h):
		noff, err := d.objectNameOffset(h)
		checkError(err)

		str, err := readString(d.content, noff)
		checkError(err)
		ret := str >= name
		// end f(h)

		if !ret {
			i = h + 1 // preserves f(i-1) == false
		} else {
			j = h // preserves f(j) == true
		}
	}

	if i >= d.changed {
		return 0, false, nil
	}

	noff, err := d.objectNameOffset(i)
	if err != nil {
		return 0, false, err
	}

	n, err := readString(d.content, noff)
	checkError(err)

	if n != name {
		return 0, false, nil
	}

	return i, true, nil
}

func (d *deltaObjectReaderImpl) ObjectNameOffset(name string) (int64, bool, error) {
	i, ok, err := d.objectNameIndex(name)
	if err != nil {
		return 0, false, err
	}

	if !ok {
		return d.sobj.ObjectNameOffset(name)
	}

	noff, err := d.objectNameOffset(i)
	if err != nil {
		return 0, false, err
	}

	voff, err := d.objectValueOffset(i)
	if err != nil {
		return 0, false, err
	}

	// Negative value offset indicates a removed key-value.

	if voff == -typeObjectPatch {
		return noff, false, nil
	}

	return noff, true, nil
}

func (d *deltaObjectReaderImpl) ObjectValueOffset(name string) (int64, bool, error) {
	i, ok, err := d.objectNameIndex(name)
	if err != nil {
		return 0, false, err
	}

	if !ok {
		return d.sobj.ObjectValueOffset(name)
	}

	voff, err := d.objectValueOffset(i)
	if err != nil {
		return 0, false, err
	}

	// Negative value offset indicates a removed key-value.

	if voff == -typeObjectPatch {
		return voff, false, nil
	}

	return voff, true, nil
}

func (d *deltaObjectReaderImpl) objectNameValueOffsets() ([]objectEntry, []int64, error) {
	properties, offsets, err := d.sobj.objectNameValueOffsets()
	if err != nil {
		return nil, nil, err
	}

	m := make(map[string]int64, len(properties)+d.changed)

	for i := range properties {
		m[properties[i].name] = offsets[i]
	}

	for i := 0; i < d.changed; i++ {
		noff, err := d.objectNameOffset(i)
		if err != nil {
			return nil, nil, err
		}

		name, err := readString(d.content, noff)
		if err != nil {
			return nil, nil, err
		}

		voff, err := d.objectValueOffset(i)
		if err != nil {
			return nil, nil, err
		}

		if voff == -typeObjectPatch {
			// Negative offset indicates removed key-value pair.
			delete(m, name)
		} else {
			m[name] = voff
		}
	}

	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}

	sort.Strings(names)

	entries, offsets := make([]objectEntry, len(names)), make([]int64, len(names))

	for i, name := range names {
		entries[i] = objectEntry{name, nil}
		offsets[i] = m[name]
	}

	return entries, offsets, nil
}

func (d *deltaObjectReaderImpl) objectNameOffsetsValueOffsets() ([]objectEntry, []int64, []int64, error) {
	properties, noffsets, voffsets, err := d.sobj.objectNameOffsetsValueOffsets()
	if err != nil {
		return nil, nil, nil, err
	}

	type offsets struct {
		name  int64
		value int64
	}

	m := make(map[string]offsets, len(properties)+d.changed)

	for i := range properties {
		m[properties[i].name] = offsets{noffsets[i], voffsets[i]}
	}

	for i := 0; i < d.changed; i++ {
		noff, err := d.objectNameOffset(i)
		if err != nil {
			return nil, nil, nil, err
		}

		name, err := readString(d.content, noff)
		if err != nil {
			return nil, nil, nil, err
		}

		voff, err := d.objectValueOffset(i)
		if err != nil {
			return nil, nil, nil, err
		}

		if voff == -typeObjectPatch {
			// Negative offset indicates removed key-value pair.
			delete(m, name)
		} else {
			m[name] = offsets{noff, voff}
		}
	}

	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}

	sort.Strings(names)

	entries, noffsets, voffsets := make([]objectEntry, len(names)), make([]int64, len(names)), make([]int64, len(names))

	for i, name := range names {
		entries[i] = objectEntry{name, nil}
		o := m[name]
		noffsets[i] = o.name
		voffsets[i] = o.value
	}

	return entries, noffsets, voffsets, nil
}

func diff(a contentReader, alen int64, b contentReader) (*utils.BytesReader, int64, bool, error) {
	buffer := new(bytes.Buffer)
	patches := make(map[int64]int64)

	// Write a header offset holder, to be updated once the patch generation is complete.

	buffer.Write(make([]byte, 4))

	// Execute diff.

	_, _, err := diffImpl(a, 0, alen, b, 0, false, buffer, patches, newEncodingCache(), newHashCache(a), newHashCache(b))
	if err != nil {
		return nil, 0, false, err
	}

	// Update the header offset.

	order.PutUint32(buffer.Bytes()[deltaHeaderOffsetOffset:], uint32(buffer.Len()))

	// Write the patch header. Sort the patches as per their original offset.

	n := make([]byte, binary.MaxVarintLen64)
	buffer.Write(n[0:binary.PutVarint(n, int64(len(patches)))])

	offsets := make([]int, 0, len(patches))
	for off := range patches {
		offsets = append(offsets, int(off))
	}

	sort.Ints(offsets)

	for _, ooff := range offsets {
		writeInt32(buffer, int32(ooff))
	}

	for _, ooff := range offsets {
		writeInt32(buffer, int32(patches[int64(ooff)]))
	}

	return utils.NewBytesReader(buffer.Bytes()), int64(buffer.Len()), len(offsets) == 0, nil
}

func diffImpl(a contentReader, aoff int64, alen int64, b contentReader, boff int64, embeddingAllowed bool, buffer *bytes.Buffer, patches map[int64]int64, cache *encodingCache,
	hcachea *hashCache, hcacheb *hashCache,
) (int64, bool, error) {
	var ta, tb int
	var err error

	// Determine the types, detecting possible embedding.
	if aoff >= 0 {
		ta, err = a.ReadType(aoff)
	} else {
		ta = int(-aoff)
	}

	if err != nil {
		return 0, false, err
	}

	if boff >= 0 {
		tb, err = b.ReadType(boff)
	} else {
		tb = int(-boff)
	}

	if err != nil {
		return 0, false, err
	}

	// Types are not the same, write the b value.
	if tb != ta {
		if !((tb == typeObjectFull || tb == typeObjectThin || tb == typeObjectPatch) &&
			(ta == typeObjectFull || ta == typeObjectThin || ta == typeObjectPatch)) {
			noff, err := reserialize(b, alen, boff, embeddingAllowed, buffer, cache)
			if noff >= 0 {
				patches[aoff] = noff
			}
			return noff, true, err
		}
	}

	switch ta {
	case typeNil, typeFalse, typeTrue:
		return 0, false, nil

	case typeString:
		sa, err := a.ReadString(aoff)
		if err != nil {
			return 0, false, err
		}

		sb, err := b.ReadString(boff)
		if err != nil {
			return 0, false, err
		}

		if sa == sb {
			return 0, false, nil
		}

		noff := writeCachedString(sb, int64(buffer.Len())+alen, buffer, cache)

		patches[aoff] = noff

		return noff, true, nil

	case typeStringInt:
		va, err := a.ReadVarInt(aoff)
		if err != nil {
			return 0, false, err
		}

		vb, err := b.ReadVarInt(boff)
		if err != nil {
			return 0, false, err
		}

		if va == vb {
			return 0, false, nil
		}

		noff := int64(buffer.Len()) + alen
		buffer.WriteByte(typeStringInt)
		writeVarInt(vb, buffer)

		patches[aoff] = noff

		return noff, true, nil

	case typeNumber:
		va, err := a.ReadString(aoff)
		if err != nil {
			return 0, false, err
		}

		vb, err := b.ReadString(boff)
		if err != nil {
			return 0, false, err
		}

		if va == vb {
			return 0, false, nil
		}

		noff := int64(buffer.Len()) + alen
		buffer.WriteByte(typeNumber)
		writeString(vb, buffer)

		patches[aoff] = noff

		return noff, true, nil

	case typeArray:
		aa, err := a.ReadArray(aoff)
		if err != nil {
			return 0, false, err
		}
		ab, err := b.ReadArray(boff)
		if err != nil {
			return 0, false, err
		}

		offsets, changes, err := arrayDiff(a, aa, b, ab, hcachea, hcacheb)
		if err != nil || !changes {
			return 0, false, err
		}

		headerStart := int64(buffer.Len())
		buffer.WriteByte(typeArray)

		n := make([]byte, binary.MaxVarintLen64)
		buffer.Write(n[0:binary.PutVarint(n, int64(len(offsets)))]) // Length.

		// Write offset placeholders.

		poffsets := buffer.Len()
		buffer.Write(make([]byte, 4*len(offsets)))

		// Write the values, if not in 'a'.

		for i := range offsets {
			if offsets[i].b {
				voff, err := reserialize(b, alen, offsets[i].offset, true, buffer, cache)
				if err != nil {
					return 0, false, err
				}

				offsets[i].offset = voff
			}
		}

		// Update the value offsets now that all values have been written.

		for _, offset := range offsets {
			order.PutUint32(buffer.Bytes()[poffsets:poffsets+4], uint32(offset.offset))
			poffsets += 4
		}

		patches[aoff] = headerStart + alen

		// Report no changes (false) as the array patching works without support from its container. This is contrary to the primitive values, with which
		// value caching prevents in situ changes.

		return headerStart + alen, false, nil

	case typeObjectFull, typeObjectThin, typeObjectPatch:
		// Write the header, with a placeholder for the offset.

		// Compare the objects.

		oa, err := a.ReadObject(aoff)
		if err != nil {
			return 0, false, err
		}

		ob, err := b.ReadObject(boff)
		if err != nil {
			return 0, false, err
		}

		alla, naoffsets, vaoffsets, err := oa.objectNameOffsetsValueOffsets()
		if err != nil {
			return 0, false, err
		}

		allb, vboffsets, err := ob.objectNameValueOffsets()
		if err != nil {
			return 0, false, err
		}

		type offsets struct {
			name  int64
			value int64
		}

		namesValues := make(map[string]offsets)

		for i, entry := range alla {
			name := entry.name
			noff, voffa := naoffsets[i], vaoffsets[i]

			if j := nameInEntrySlice(allb, name); j >= 0 {
				voffb := vboffsets[j]
				offset, changed, err := diffImpl(a, voffa, alen, b, voffb, true, buffer, patches, cache, hcachea, hcacheb)
				if err != nil {
					return 0, false, err
				}

				if changed {
					// Value changed, include the new value to object patch.
					namesValues[name] = offsets{noff, offset}

					delete(patches, voffa)
				}
				// else: Nested elements might have changed, but nothing to include to the object patch.
			} else {
				// Property removed.
				namesValues[name] = offsets{noff, -typeObjectPatch}
			}
		}

		for i, entry := range allb {
			if name := entry.name; nameInEntrySlice(alla, name) < 0 {
				// New property added.
				noff := writeCachedString(name, alen+int64(buffer.Len()), buffer, cache)

				voff, err := reserialize(b, alen, vboffsets[i], true, buffer, cache)
				if err != nil {
					return 0, false, err
				}

				namesValues[name] = offsets{noff + 1, voff} // Write above adds a type byte, which is not strictly speaking needed. For now, just skip it. TODO.
			}
		}

		// If no changes in this value, no need to create an object patch. Note that the patches might been updated, though.
		if len(namesValues) == 0 {
			return 0, false, nil
		}

		headerStart := int64(buffer.Len())
		buffer.WriteByte(typeObjectPatch)

		writeInt32(buffer, int32(aoff))

		n := make([]byte, binary.MaxVarintLen64)
		buffer.Write(n[0:binary.PutVarint(n, int64(len(allb)))])        // Length.
		buffer.Write(n[0:binary.PutVarint(n, int64(len(namesValues)))]) // # of changes.

		sorted := make([]string, 0, len(namesValues))
		for name := range namesValues {
			sorted = append(sorted, name)
		}

		sort.Strings(sorted)

		for _, name := range sorted {
			writeInt32(buffer, int32(namesValues[name].name))
		}

		for _, name := range sorted {
			writeInt32(buffer, int32(namesValues[name].value))
		}

		patches[aoff] = headerStart + alen

		// Report no changes (false) as the object patching works without support from its container. This is contrary to the primitive values, with which
		// value caching prevents in situ changes.

		return headerStart + alen, false, nil

	case typeBinaryFull:
		ba, err := a.ReadBytes(aoff)
		if err != nil {
			return 0, false, err
		}

		bb, err := b.ReadBytes(boff)
		if err != nil {
			return 0, false, err
		}

		if bytes.Equal(ba, bb) {
			return 0, false, nil
		}

		headerStart := int64(buffer.Len())

		buffer.WriteByte(typeBinaryFull)
		writeBytes(bb, buffer)

		patches[aoff] = headerStart + alen

		return headerStart + alen, true, nil

	default:
		panic("json: unsupported type")
	}
}

func nameInEntrySlice(entries []objectEntry, name string) int {
	// golang sort.Search implementation embedded here to assist compiler in inlining.
	i, j := 0, len(entries)
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j

		// begin f(h):
		ret := entries[h].name >= name
		// end f(h)

		if !ret {
			i = h + 1 // preserves f(i-1) == false
		} else {
			j = h // preserves f(j) == true
		}
	}
	if i < len(entries) && entries[i].name == name {
		return i
	}

	return -1
}

// reserialize clones the given object.
func reserialize(src contentReader, alen int64, off int64, embeddingAllowed bool, buffer *bytes.Buffer, cache *encodingCache) (int64, error) {
	var t int
	var err error

	if off >= 0 {
		t, err = src.ReadType(off)
	} else {
		t = int(-off)
	}

	if err != nil {
		return 0, err
	}

	start := int64(buffer.Len())

	switch t {
	case typeNil, typeFalse, typeTrue:
		if embeddingAllowed {
			return int64(-t), nil
		}

		buffer.WriteByte(byte(t))
		return alen + start, nil

	case typeString:
		v, err := src.ReadString(off)
		if err != nil {
			return 0, err
		}

		return writeCachedString(v, alen+start, buffer, cache), nil

	case typeStringInt:
		v, err := src.ReadVarInt(off)
		if err != nil {
			return 0, err
		}

		buffer.WriteByte(typeStringInt)
		writeVarInt(v, buffer)
		return alen + start, nil

	case typeNumber:
		v, err := src.ReadString(off)
		if err != nil {
			return 0, err
		}

		return writeCachedNumber(v, alen+start, buffer, cache), nil

	case typeArray:
		a, err := src.ReadArray(off)
		if err != nil {
			return 0, err
		}

		l, err := a.ArrayLen()
		if err != nil {
			return 0, err
		}

		buffer.WriteByte(typeArray)

		// Write # of properties.
		writeVarInt(int64(l), buffer)

		// Write value offset placeholders.
		offsets := buffer.Len()
		buffer.Write(make([]byte, 4*l))

		for i := 0; i < l; i++ {
			voff, err := a.ArrayValueOffset(i)
			if err != nil {
				return 0, err
			}

			valueOffset, err := reserialize(src, alen, voff, true, buffer, cache)
			if err != nil {
				return 0, err
			}

			order.PutUint32(buffer.Bytes()[offsets:offsets+4], uint32(valueOffset))
			offsets += 4
		}

		return alen + start, nil

	case typeObjectFull, typeObjectThin, typeObjectPatch:
		obj, err := src.ReadObject(off)
		if err != nil {
			return 0, err
		}

		// First use of object type. Write the full object description. Start with the type.
		buffer.WriteByte(typeObjectFull)

		properties, offsets, err := obj.objectNameValueOffsets()
		if err != nil {
			return 0, err
		}

		// Write # of properties.
		writeVarInt(int64(len(properties)), buffer)

		// Write property offset placeholders.
		keyOffsets := buffer.Len()
		buffer.Write(make([]byte, 4*len(properties)))

		// Write value offset placeholders.
		valueOffsets := buffer.Len()
		buffer.Write(make([]byte, 4*len(properties)))

		// Write property names, updating the offset placeholders while doing so. If string found in cache, merely update the offset placeholder.
		for _, property := range properties {
			offset := writeCachedString(property.name, alen+int64(buffer.Len()), buffer, cache)
			order.PutUint32(buffer.Bytes()[keyOffsets:keyOffsets+4], uint32(offset+1))
			keyOffsets += 4
		}

		// Write the property values, updating the offset placeholders after each.
		for i := range offsets {
			voff := offsets[i]
			valueOffset, err := reserialize(src, alen, voff, true, buffer, cache)
			if err != nil {
				return 0, err
			}

			order.PutUint32(buffer.Bytes()[valueOffsets:valueOffsets+4], uint32(valueOffset))
			valueOffsets += 4
		}

		// TODO: Object thinning to be added.

		return alen + start, nil

	case typeBinaryFull:
		v, err := src.ReadBytes(off)
		if err != nil {
			return 0, err
		}

		buffer.WriteByte(typeBinaryFull)
		writeBytes(v, buffer)
		return alen + start, nil
	}

	return 0, fmt.Errorf("delta: unsupported type: %d", t)
}

func writeInt32(buffer *bytes.Buffer, value int32) {
	x := make([]byte, 4)
	order.PutUint32(x, uint32(value))
	buffer.Write(x)
}

func writeCachedString(v string, offset int64, buffer *bytes.Buffer, cache *encodingCache) int64 {
	existing := cache.CacheString(v, int32(offset))
	if existing == int32(offset) {
		buffer.WriteByte(typeString)
		writeString(v, buffer)
		return offset
	}
	return int64(existing)
}

func writeCachedNumber(v string, offset int64, buffer *bytes.Buffer, cache *encodingCache) int64 {
	existing := cache.CacheNumber(v, int32(offset))
	if existing == int32(offset) {
		buffer.WriteByte(typeNumber)
		writeString(v, buffer)
		return offset
	}
	return int64(existing)
}
