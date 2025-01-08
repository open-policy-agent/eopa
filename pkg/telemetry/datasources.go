package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/open-policy-agent/opa/v1/config"
	"github.com/open-policy-agent/opa/v1/plugins"
)

// When OPA creates a *plugins.Manager with plugins.New(), it iterates over any
// configured hooks and allows them to read, and potentially mutate the
// *config.Config object, which will include any data plugin configs being used
// as part of an EOPA setup. The datasources telemetry works by creating just
// such a hook (*DatasourceTelemetryHook).
//
// Internally, this hook counts how many data plugins appear, grouped by their
// `type` field. This means that we don't know if the datasource 'worked' (how
// would you determine that in a general way?), just that it was in the config.
//
// Each time the hook runs, it overwrites a mutex protected package variable in
// this package (tmut, seenDatasources) that stores these counts. This behavior
// is important because the config, and thus the datasources that should be
// reported, can change at runtime due to discovery.

var tmut *sync.RWMutex = &sync.RWMutex{}
var seenDatasources map[string]int = make(map[string]int)

func recordSeenDatasources(seen map[string]int) {
	tmut.Lock()
	defer tmut.Unlock()

	seenDatasources = seen
}

func getSeenDatasources() map[string]int {
	tmut.RLock()
	defer tmut.RUnlock()
	return seenDatasources
}

// GatherDatasources returns a map[string]int containing how many of each type
// of datasource appeared in the most recent config. For the telemetry to be
// reported, you should pass this function to runtime.RegisterGatherers().
func GatherDatasources(_ context.Context) (any, error) {
	ds := getSeenDatasources()
	if len(ds) == 0 {
		return nil, nil
	}
	return ds, nil
}

type DatasourceTelemetryHook struct {
	manager *plugins.Manager
}

// NewDatasourceTelemetryHook() sets up the necessary callbacks so that
// datasource type and count telemetry will be gathered, and later made
// available via GatherDatasources(). Typically you should call this function
// and include it's result as an argument to `hooks.New()`, so that it can
// later be included in your plugin manager.
func NewDatasourceTelemetryHook() *DatasourceTelemetryHook {
	return &DatasourceTelemetryHook{}
}

func (h *DatasourceTelemetryHook) Init(m *plugins.Manager) {
	h.manager = m
}

func (h *DatasourceTelemetryHook) OnConfigDiscovery(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return h.onConfig(ctx, conf)
}

func (h *DatasourceTelemetryHook) OnConfig(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return h.onConfig(ctx, conf)
}

func (h *DatasourceTelemetryHook) onConfig(_ context.Context, conf *config.Config) (*config.Config, error) {

	sd := make(map[string]int)

	plugConf, ok := conf.Plugins["data"]

	if !ok {
		// Not having any configured data plugins is not an error
		// condition. We record an empty map in this situation though,
		// because we may have received a new config via discovery that
		// removed the data section.
		recordSeenDatasources(map[string]int{})
		return conf, nil
	}

	var pc any
	err := json.Unmarshal(plugConf, &pc)
	if err != nil {
		return nil, err
	}

	m, ok := pc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("telemetry: expected data plugin config to unmarshal as map[string]any, not %T", pc)
	}

	for dsName, dsConfI := range m {
		dsConf, ok := dsConfI.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("telemetry: expected datasource config for '%s' to unmarshal as map[string]any, not %T", dsName, dsConfI)
		}

		dsTypeI, ok := dsConf["type"]
		if !ok {
			return nil, fmt.Errorf("telemetry: expected datasource config for '%s' to have a 'type' field, but it did not", dsName)
		}

		dsType, ok := dsTypeI.(string)
		if !ok {
			return nil, fmt.Errorf("telemetry: expected datasource type for '%s' to be string, no %T", dsName, dsTypeI)
		}

		sd[dsType]++
	}

	recordSeenDatasources(sd)

	return conf, nil
}
