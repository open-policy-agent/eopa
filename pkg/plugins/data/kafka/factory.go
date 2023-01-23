package kafka

import (
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config interface{}) plugins.Plugin {

	m.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})

	c := config.(Config)
	return &Data{
		config:        c,
		log:           m.Logger(),
		exit:          make(<-chan struct{}),
		path:          ast.MustParseRef("data." + config.(Config).Path),
		manager:       m,
		transformRule: ast.MustParseRef(c.RegoTransformRule),
	}
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if len(c.BrokerURLs) == 0 {
		return nil, fmt.Errorf("need at least one broker URL")
	}
	if len(c.Topics) == 0 {
		return nil, fmt.Errorf("need at least one topic")
	}
	if c.RegoTransformRule == "" {
		return nil, fmt.Errorf("rego transform rule required")
	}
	return c, nil
}
