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

	out := grpcServerPlugin{
		manager:          m,
		config:           config.(Config),
		server:           New(m, config.(Config)),
		logger:           m.Logger(),
		shutdownComplete: make(chan struct{}),
	}

	m.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateOK})
	return &out
}

// We don't transform any items here, we just validate that they're correct if present.
func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	// Ensure we have an address to serve on.
	if c.Addr == "" {
		return nil, fmt.Errorf("need at least one address to serve from")
	}
	// Check client authentication scheme.
	if c.Authentication != "" {
		switch c.Authentication {
		case "token", "tls", "off":
		default:
			return nil, fmt.Errorf("Unknown authentication scheme: %s", c.Authentication)
		}
	}
	// Check client authorization scheme.
	if c.Authorization != "" {
		switch c.Authorization {
		case "basic", "off":
		default:
			return nil, fmt.Errorf("Unknown authorization scheme: %s", c.Authorization)
		}
	}
	// Check minimum allowed TLS version.
	if c.TLS.MinVersion != "" {
		switch c.TLS.MinVersion {
		case "1.0", "1.1", "1.2", "1.3":
		default:
			return nil, fmt.Errorf("Unknown TLS version: %s", c.TLS.MinVersion)
		}
	}

	// Make sure *both* parameters are provided for server-side TLS.
	switch {
	case c.TLS.CertFile == "" && c.TLS.CertKeyFile == "":
	case c.TLS.CertFile != "" && c.TLS.CertKeyFile != "":
	default:
		return nil, fmt.Errorf("tls.cert_file and tls.cert_private_key_file must be specified together")
	}

	return c, nil
}
