//go:build use_opa_fork

package compile

import (
	"net/http"

	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/topdown/cache"
)

func extraRoute(m *plugins.Manager, path, prom string, h http.HandlerFunc) {
	m.ExtraRoute(path, prom, h)
}

func getCaches(m *plugins.Manager) (cache.InterQueryCache, cache.InterQueryValueCache) {
	return m.GetCaches()
}
