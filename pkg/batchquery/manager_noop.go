//go:build !use_opa_fork

package batchquery

import (
	"net/http"

	"github.com/open-policy-agent/opa/v1/plugins"
)

func extraRoute(*plugins.Manager, string, string, http.HandlerFunc) {}
