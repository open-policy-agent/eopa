//go:build !use_opa_fork

package bundle

import (
	"github.com/open-policy-agent/opa/bundle"
)

func maybeAddPlugin(*bundle.ActivateOpts) {
}