package sdk_test

import (
	"context"
	"testing"

	"github.com/open-policy-agent/eopa/pkg/compile"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/topdown/cache"
)

// TestCompile ensures that the compile package can be used with OSS OPA.
func TestCompile(*testing.T) {
	l := logging.New()
	iqc := cache.NewInterQueryCache(nil)
	iqvc := cache.NewInterQueryValueCache(context.Background(), nil)
	_ = compile.Handler(l, iqc, iqvc)
}
