// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"fmt"
	"io"
)

// Blob represents a binary blob.
type Blob interface {
	File
	Value() []byte
}

type blobImpl struct {
	data []byte
}

func newBlob(reader contentReader, offset int64) Blob {
	data, err := reader.ReadBytes(offset)
	checkError(err)
	return &blobImpl{data: data}
}

func NewBlob(data []byte) Blob {
	return &blobImpl{data: data}
}

func (b *blobImpl) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(b.data)
	return int64(n), err
}

func (b *blobImpl) Contents() interface{} {
	return b.Value()
}

func (b *blobImpl) Value() []byte {
	return b.data
}

func (b *blobImpl) String() string {
	return fmt.Sprintf("<%d bytes of binary>", len(b.data))
}

func (b *blobImpl) Clone(bool) File {
	data := make([]byte, len(b.data))
	copy(data, b.data)
	return NewBlob(data)
}
