package compile

import (
	"context"

	topdown_cache "github.com/open-policy-agent/opa/v1/topdown/cache"
)

type hook struct {
	InterQueryCache      topdown_cache.InterQueryCache
	InterQueryValueCache topdown_cache.InterQueryValueCache
}

func NewHook() *hook {
	return &hook{}
}

func (x *hook) OnInterQueryCache(_ context.Context, c topdown_cache.InterQueryCache) error {
	x.InterQueryCache = c
	return nil
}

func (x *hook) OnInterQueryValueCache(_ context.Context, c topdown_cache.InterQueryValueCache) error {
	x.InterQueryValueCache = c
	return nil
}
