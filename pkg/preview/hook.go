// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package preview

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/open-policy-agent/opa/v1/config"
	"github.com/open-policy-agent/opa/v1/plugins"
)

// PreviewConfig is the shape of the configuration object the plugin expects
type PreviewConfig struct {
	Enabled bool `json:"enabled"`
}

// PreviewHook holds the state of the preview endpoint
type PreviewHook struct {
	manager *plugins.Manager
	config  PreviewConfig
}

func NewHook() *PreviewHook {
	return &PreviewHook{
		config: PreviewConfig{
			Enabled: true,
		},
	}
}

func (p *PreviewHook) Init(m *plugins.Manager) {
	m.ExtraRoute(httpPrefix, metricName, p.ServeHTTP)
	m.ExtraRoute(httpPrefix+"/{path...}", metricName, p.ServeHTTP)
	for _, meth := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		m.ExtraRoute(meth+"/v0/preview/{path...}", metricName, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusMethodNotAllowed)
		})
	}
	p.manager = m
}

func (p *PreviewHook) OnConfigDiscovery(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return p.onConfig(ctx, conf)
}

func (p *PreviewHook) OnConfig(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return p.onConfig(ctx, conf)
}

func (p *PreviewHook) onConfig(_ context.Context, conf *config.Config) (*config.Config, error) {
	if conf.Extra["preview"] == nil {
		return conf, nil
	}

	// default to enabled, unless the provided config changes it
	previewConfig := PreviewConfig{Enabled: true}
	if err := json.Unmarshal(conf.Extra["preview"], &previewConfig); err != nil {
		return conf, err
	}

	p.config = previewConfig
	return conf, nil
}
