//go:build e2e

package testdata

import (
	"embed"
)

//go:embed *.txtar
var FS embed.FS
