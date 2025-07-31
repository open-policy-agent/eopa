// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package testdata

import (
	"embed"
)

//go:embed *.txtar
var FS embed.FS
