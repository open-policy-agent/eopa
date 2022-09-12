package utils

import (
	"bytes"
	"errors"
	"io"
)

// SafeRead reads len(data) bytes from reader to data. It also guarantees that if not all data was not read, there is an error, but masks the io.EOF error out if all the data requested was read.
// In other words, it guarantees there is an error returned, if not all bytes requested were read, and only then there is an error.
func SafeRead(reader io.Reader, data []byte) (int, error) {
	offset := 0
	for {
		n, err := reader.Read(data[offset:])
		offset += n

		if offset < len(data) && err == nil {
			// Not enough data returned, but neither error returned. Try again.
			continue
		}

		if offset == len(data) && err == io.EOF {
			return offset, nil
		}

		// Either no error and everything read or an error.

		return offset, err
	}
}

// SafeReadAt reads len(data) bytes from reader at offset to data. It also guarantees that if not all data was not read, there is an error, but masks the io.EOF error out if all the data
// requested was read. In other words, it guarantees there is an error returned, if not all bytes requested were read, and only then there is an error.
func SafeReadAt(reader *MultiReader, data []byte, offset int64) (int, error) {
	read := 0
	for {
		n, err := reader.ReadAt(data, offset)
		offset += int64(n)
		read += n

		if read < len(data) && err == nil {
			// Not enough data returned, but neither error returned. Try again.
			continue
		}

		if read == len(data) && errors.Is(err, io.EOF) {
			return read, nil
		}

		// Either no error and everything read or an error.

		return read, err
	}
}

// MultiReader supports reading from two concatenated readers, which have to be either bytes readers or multi readers. The static
// typing of the readers (instead of using io.ReaderAt interface) makes this escape analysis compatible: the buffers passed for Read
// don't escape because of the MultiReader.
type MultiReader struct {
	base int64 // To use with the multireader am.
	n    int64 // Total bytes available in reader a.
	ab   *BytesReader
	bb   *BytesReader
	am   *MultiReader
	bm   *MultiReader
}

func NewMultiReaderFromMultiReader(a *MultiReader, off int64, n int64) *MultiReader {
	return &MultiReader{am: a, base: off, n: n, bm: nil}
}

func NewMultiReaderFromMultiReaders(a *MultiReader, off int64, n int64, b *MultiReader) *MultiReader {
	return &MultiReader{am: a, base: off, n: n, bm: b}
}

func NewMultiReaderFromBytesReader(a *BytesReader) *MultiReader {
	return &MultiReader{ab: a, base: 0, n: int64(a.Len()), bb: NewBytesReader(nil)}
}

func NewMultiReaderFromBytesReaders(a, b *BytesReader) *MultiReader {
	return &MultiReader{ab: a, base: 0, n: int64(a.Len()), bb: b}
}

// Bytes returns the slice of n bytes at the provided offset. It returns io.EOF if n bytes are not available.
func (r *MultiReader) Bytes(offset int64, n int) ([]byte, error) {
	if r.ab != nil {
		switch {
		case offset+int64(n) <= int64(r.ab.Len()):
			return r.ab.bytes(offset, n), nil

		case offset < int64(r.ab.Len()):
			m := r.ab.bytes(offset, int(r.n-offset))

			k, err := r.bb.Bytes(0, n-len(m))
			if err != nil {
				return nil, err
			}

			result := make([]byte, len(m), n)
			copy(result, m)
			return append(result, k...), nil

		default: // offset >= r.n:
			return r.bb.Bytes(offset-r.n, n)
		}
	}

	base_offset := r.base + offset

	switch {
	case base_offset < r.n && base_offset+int64(n) <= r.n:
		return r.am.Bytes(base_offset, n)

	case base_offset < r.n:
		m, err := r.am.Bytes(base_offset, int(r.n-base_offset))
		if int64(len(m)) < r.n-base_offset {
			// Both reading too little as well as error are sufficient reasons to exit.
			return m, err
		}

		if r.bm == nil {
			return m, nil
		}

		result := make([]byte, len(m), n)
		copy(result, m)

		k, err := r.bm.Bytes(0, n-len(m))
		return append(result, k...), err

	default: // base_offset >= r.n:
		if r.bm != nil {
			return r.bm.Bytes(base_offset-r.n, n)
		}

		return nil, io.EOF
	}
}

func (r *MultiReader) ReadAt(p []byte, offset int64) (n int, err error) {
	if r.ab != nil {
		switch {
		case offset < r.n && offset+int64(len(p)) < r.n:
			return r.ab.ReadAt(p, offset)

		case offset < r.n:
			m, err := r.ab.ReadAt(p[:r.n-offset], offset)
			if int64(m) < r.n-offset {
				// Both reading too little as well as error are sufficient reasons to exit.
				return m, err
			}

			k, err := r.bb.ReadAt(p[m:], 0)
			return m + k, err

		default: // offset >= r.n:
			return r.bb.ReadAt(p, offset-r.n)
		}
	}

	base_offset := r.base + offset

	switch {
	case base_offset < r.n && base_offset+int64(len(p)) < r.n:
		return r.am.ReadAt(p, base_offset)

	case base_offset < r.n:
		m, err := r.am.ReadAt(p[:r.n-base_offset], base_offset)
		if int64(m) < r.n-base_offset {
			// Both reading too little as well as error are sufficient reasons to exit.
			return m, err
		}

		if r.bm == nil {
			return m, nil
		}

		k, err := r.bm.ReadAt(p[m:], 0)
		return m + k, err

	default: // base_offset >= r.n:
		if r.bm != nil {
			return r.bm.ReadAt(p, base_offset-r.n)
		}

		return 0, io.EOF
	}
}

func (r *MultiReader) Len() int {
	if r.ab != nil {
		n := r.ab.Len() - int(r.base)
		if r.bb != nil {
			n += r.bb.Len()
		}

		return n
	}

	n := r.am.Len() - int(r.base)

	if r.bm != nil {
		n += r.bm.Len()
	}

	return n
}

func (r *MultiReader) Append(data []byte) {
	// TODO: Remove this unnecessary copy (which requires changing interfaces).

	if r.bb != nil {
		buffer := bytes.NewBuffer(make([]byte, 0, r.bb.Len()+len(data)))
		buffer.Write(r.bb.s)
		r.bb = NewBytesReader(append(buffer.Bytes(), data...))
		return
	}

	if r.ab != nil {
		r.n += int64(len(data))
		buffer := bytes.NewBuffer(make([]byte, 0, r.n))
		buffer.Write(r.ab.s)
		r.ab = NewBytesReader(append(buffer.Bytes(), data...))
		return
	}

	if r.bm != nil {
		r.bm.Append(data)
		return
	}

	r.am.Append(data)
	r.n += int64(len(data))
}

type BytesReader struct {
	s []byte
}

func NewBytesReader(b []byte) *BytesReader { return &BytesReader{b} }

// ReadAt implements the io.ReaderAt interface.
func (r *BytesReader) ReadAt(b []byte, off int64) (n int, err error) {
	// cannot modify state - see io.ReaderAt
	if off < 0 {
		return 0, errors.New("bytes.Reader.ReadAt: negative offset")
	}
	if off > int64(len(r.s)) {
		return 0, io.EOF
	}
	n = copy(b, r.s[off:])
	if n < len(b) {
		err = io.EOF
	}
	return
}

// Len returns the number of bytes of the unread portion of the
// slice.
func (r *BytesReader) Len() int {
	return len(r.s)
}

// Bytes returns the slice of n bytes at the provided offset. It returns io.EOF if n bytes are not available.
func (r *BytesReader) Bytes(offset int64, n int) ([]byte, error) {
	if offset+int64(n) > int64(len(r.s)) {
		return nil, io.EOF
	}

	return r.s[offset : offset+int64(n)], nil
}

func (r *BytesReader) bytes(offset int64, n int) []byte {
	return r.s[offset : offset+int64(n)]
}
