//go:build !use_opa_fork

package compile

import (
	"net/http"

	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/topdown/cache"
)

func extraRoute(*plugins.Manager, string, string, http.HandlerFunc) {}

func getCaches(*plugins.Manager) (cache.InterQueryCache, cache.InterQueryValueCache) {
	return nil, nil
}
