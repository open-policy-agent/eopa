package preview

import (
	"github.com/open-policy-agent/opa/plugins"
)

// Name represent the name used to refer to the preview plugin
const Name = "preview"

type factory struct{}

// Factory creates a new instance of the preview plugin factory
func Factory() plugins.Factory {
	return &factory{}
}

// New creates a new preview plugin struct
func (factory) New(m *plugins.Manager, _ interface{}) plugins.Plugin {
	m.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
	p := &Plugin{
		manager: m,
	}

	return p
}

// Validate parses and the passed plugin configuration
func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	return config, nil
}
