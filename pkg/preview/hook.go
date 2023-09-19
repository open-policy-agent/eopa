package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/open-policy-agent/opa/config"
	"github.com/open-policy-agent/opa/plugins"
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
	m.GetRouter().Handle(httpPrefix, p).Methods(http.MethodPost)
	m.GetRouter().Handle(fmt.Sprintf("%s/{path:.+}", httpPrefix), p).Methods(http.MethodPost)
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
