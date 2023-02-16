package impact

import (
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/logs"
	"github.com/open-policy-agent/opa/util"
)

const Name = "impact_analysis"

// Config represents the configuration for the impact analysis plugin
type Config struct {
	Rate          float32 `json:"sampling_rate"` // 0 <= rate <= 1
	BundlePath    string  `json:"bundle_path"`   // bundle to use for second eval
	PublishEquals bool    `json:"publish_equal"` // publish DL even when resuls match (for metrics)
}

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config interface{}) plugins.Plugin {
	m.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
	c := config.(Config)
	p := &Impact{
		manager: m,
		config:  c,
		log:     m.Logger(),
	}
	if l := logs.Lookup(m); l != nil {
		p.dl = l
	}
	return p
}

func (factory) Validate(manager *plugins.Manager, config []byte) (interface{}, error) {
	parsedConfig := Config{}
	if err := util.Unmarshal(config, &parsedConfig); err != nil {
		return nil, err
	}
	if parsedConfig.Rate < 0 || parsedConfig.Rate > 1 {
		return nil, fmt.Errorf("sampling rate %f invalid: must be between 0 and 1 (inclusive)", parsedConfig.Rate)
	}
	if parsedConfig.BundlePath == "" {
		return nil, fmt.Errorf("bundle_path required")
	}
	fd, err := os.Open(parsedConfig.BundlePath)
	if err != nil {
		return nil, err
	}
	_ = fd.Close() // NOTE(sr): The file could have disappeared in the mean time, but it's unlikely
	return parsedConfig, nil
}
