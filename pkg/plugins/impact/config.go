package impact

import (
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/logs"
	"github.com/open-policy-agent/opa/util"
)

const Name = "impact_analysis"

// Config represents the configuration for the impact analysis plugin
type Config struct {
	DecisionLogs bool `json:"decision_logs"` // Also emit decision logs for secondary evals
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
	if l := logs.Lookup(m); l != nil && c.DecisionLogs {
		p.dl = l
	}
	return p
}

func (factory) Validate(manager *plugins.Manager, config []byte) (interface{}, error) {
	parsedConfig := Config{}
	if err := util.Unmarshal(config, &parsedConfig); err != nil {
		return nil, err
	}
	return parsedConfig, nil
}
