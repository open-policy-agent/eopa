// Package version implements helper functions for the stored version.
package version

import (
	"fmt"
	"runtime"

	opa_version "github.com/open-policy-agent/opa/v1/version"
)

// EOPA version (e.g. "1.16.0"), injected via LDFLAGS
var Version = "dev"

func UserAgent() string {
	return fmt.Sprintf("EOPA/%s Open Policy Agent/%s (%s, %s)", Version, opa_version.Version, runtime.GOOS, runtime.GOARCH)
}
