package telemetry

import (
	"context"
	"sort"
	"sync"

	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/plugins/bundle"
	"github.com/open-policy-agent/opa/v1/plugins/discovery"
)

const name = "telemetry/bundles"

type bundleData struct {
	Size   int    `json:"size"`             // size in bytes
	Type   string `json:"type"`             // type: snapshot/delta
	Format string `json:"format,omitempty"` // format: json/bjson
}

// bundle data keyed by bundle name -- names are not included in telemetry payloads!
var data = map[string]bundleData{}
var dataMtx = &sync.Mutex{}

// GatherBundleData will retrieve the bundle data stored in our
// `data` package var.
func GatherBundleData(context.Context) (any, error) {
	dataMtx.Lock()
	defer dataMtx.Unlock()
	if len(data) == 0 {
		return nil, nil
	}

	// drop the names, return array (ordered by size)
	arr := make([]bundleData, 0, len(data))
	for _, d := range data {
		arr = append(arr, d)
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].Size < arr[j].Size })
	return arr, nil
}

// Setup hooks up our bundles telemetry gathering with the plugins.Manager:
// If it's already got a bundle plugin, we register a listener.
// If it doesn't, but has a discovery plugin, we'll register a listener for
// discovery updates, and if we have a bundle plugin after the discovery update,
// we register a listener with it.
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
		dataMtx.Lock()
		defer dataMtx.Unlock()
		dataNew := make(map[string]bundleData, len(bulk))

		for name, info := range bulk {
			bd, ok := data[name] // copy over any "format" previously gathered
			if !ok {
				bd = bundleData{Format: "json"}
			}
			bd.Size = info.Size
			bd.Type = info.Type
			dataNew[name] = bd
		}
		data = dataNew
	})
	return true
}

func SetBJSON(name string) {
	dataMtx.Lock()
	defer dataMtx.Unlock()
	bd, ok := data[name]
	if !ok {
		bd = bundleData{}
	}
	bd.Format = "bjson"
	data[name] = bd
}
