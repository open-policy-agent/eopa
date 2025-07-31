// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package embedded

import (
	"embed"
)

//go:embed *
var Library embed.FS
