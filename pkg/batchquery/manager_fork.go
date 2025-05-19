//go:build use_opa_fork

package batchquery

import (
	"net/http"

	"github.com/open-policy-agent/opa/v1/plugins"
)

func extraRoute(m *plugins.Manager, path, prom string, h http.HandlerFunc) {
	m.ExtraRoute(path, prom, h)
}
