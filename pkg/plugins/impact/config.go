// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package impact

import (
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/plugins/logs"
	"github.com/open-policy-agent/opa/v1/util"
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

func (factory) New(m *plugins.Manager, config any) plugins.Plugin {
	m.ExtraMiddleware(HTTPMiddleware)

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
	m.ExtraRoute(httpPrefix, metricName, p.ServeHTTP)
	return p
}

func (factory) Validate(_ *plugins.Manager, config []byte) (any, error) {
	parsedConfig := Config{}
	if err := util.Unmarshal(config, &parsedConfig); err != nil {
		return nil, err
	}
	return parsedConfig, nil
}
