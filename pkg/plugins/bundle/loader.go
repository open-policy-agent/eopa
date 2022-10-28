package bundle

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/loader"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/rego"
)

type CustomLoader struct{}

func (*CustomLoader) Load(_ context.Context, m metrics.Metrics, paths []string) (map[string]*bundle.Bundle, error) {
	bundles := map[string]*bundle.Bundle{}

	for _, path := range paths {
		bndl, err := loader.NewFileLoader().
			WithMetrics(m).
			WithSkipBundleVerification(true).
			WithBundleLazyLoadingMode(true).
			AsBundle(path)
		if err != nil {
			return nil, fmt.Errorf("loading error: %s", err)
		}
		bundles[path] = bndl
	}

	return bundles, nil
}

func init() {
	rego.RegisterBundleLoader(&CustomLoader{})
}
