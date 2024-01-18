package telemetry

import (
	"context"
	"sort"
	"sync/atomic"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/bundle"
	"github.com/open-policy-agent/opa/plugins/discovery"
)

const name = "telemetry/bundle_sizes"

var sizes atomic.Pointer[[]uint64]

// GatherBundleSize will retrieve the bundle sizes stored in our
// `sizes` package var.
func GatherBundleSizes(context.Context) (any, error) {
	if b := sizes.Load(); b != nil {
		return *b, nil
	}
	return nil, nil
}

// Setup hooks up our bundle_sizes telemetry gathering with the plugins.Manager:
// If it's already got a bundle plugin, we register a listener.
// If it doesn't, but has a discovery plugin, we'll register a listener for
// discovery updates, and if we have a bundle plugin after the discovery update,
// we reigster a listener with it.
func Setup(p *plugins.Manager, disco *discovery.Discovery) {
	if setupBundleListener(p) {
		return
	}

	disco.RegisterListener(name, func(bundle.Status) {
		// discovery was run: if there's a bundle plugin now, register with it
		_ = setupBundleListener(p)
	})
}

func setupBundleListener(p *plugins.Manager) bool {
	b := bundle.Lookup(p)
	if b == nil {
		return false
	}

	// NOTE(sr): Calling this multiple times is harmless: a previously-registered
	// callback of the same name will just be overwritten.
	b.RegisterBulkListener(name, func(bulk map[string]*bundle.Status) {
		s := make([]uint64, 0, len(bulk))
		for _, info := range bulk {
			s = append(s, uint64(info.Size))
		}
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		sizes.Store(&s)
	})
	return true
}
