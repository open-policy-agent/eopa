// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import "slices"

// Deprecated: Use slices.Contains instead.
// Contains returns true if input contains the given string.
func Contains(input []string, s string) bool {
	return slices.Contains(input, s)
}
