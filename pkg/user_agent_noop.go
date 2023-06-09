//go:build !use_opa_fork

package pkg

import (
	"fmt"
	"runtime"

	"github.com/open-policy-agent/opa/version"
)

// GetUserAgent returns the current UserAgent
func GetUserAgent() string {
	return fmt.Sprintf("Load/%s (%s, %s)", version.Version, runtime.GOOS, runtime.GOARCH)
}
