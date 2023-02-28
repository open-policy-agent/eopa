package grpc

import (
	"fmt"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config interface{}) plugins.Plugin {
	m.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateNotReady})

	return &grpcServerPlugin{
		manager:          m,
		config:           config.(Config),
		server:           New(m),
		shutdownComplete: make(chan struct{}),
	}
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if c.Addr == "" {
		return nil, fmt.Errorf("need at least one address to serve from")
	}
	return c, nil
}
