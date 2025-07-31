// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package json

import (
	"bytes"
	"testing"
)

func FuzzDecode(f *testing.F) {
	f.Fuzz(func(t *testing.T, input []byte) {
		t.Parallel()
		_, _ = NewDecoder(bytes.NewReader(input)).Decode() // we're only interested in panics
	})
}
