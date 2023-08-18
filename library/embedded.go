package embedded

import (
	"embed"
)

//go:embed *
var Library embed.FS
