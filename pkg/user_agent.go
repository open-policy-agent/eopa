package pkg

import (
	"fmt"
	"runtime"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/open-policy-agent/opa/version"

	iversion "github.com/styrainc/load-private/pkg/internal/version"
)

// SetUserAgent overrides the OPA and Load UserAgent
func SetUserAgent(agent string) {
	if len(agent) > 0 {
		cmd.UserAgent(agent)
		iversion.UserAgent = fmt.Sprintf("%s/%s (%s, %s)", agent, version.Version, runtime.GOOS, runtime.GOARCH)
	}
}

// GetUserAgent returns the current UserAgent
func GetUserAgent() string {
	return iversion.UserAgent
}
