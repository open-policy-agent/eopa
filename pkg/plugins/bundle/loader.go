package bundle

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/loader"
	"github.com/open-policy-agent/opa/metrics"
)

func init() {
	// file system json loader (load json or bjson)
	loader.RegisterExtension(".json", loadJSON)
}

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

func loadJSON(path string, bs []byte, m metrics.Metrics) (interface{}, error) {
	r, err := BjsonFromBinary(bs)
	if err != nil {
		return nil, err
	}
	return r.JSON(), nil
}
