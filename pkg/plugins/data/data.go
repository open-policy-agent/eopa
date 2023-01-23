package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
	"github.com/styrainc/load/pkg/plugins/data/kafka"
)

const Name = "data"

var dataPluginRegistery = map[string]plugins.Factory{
	kafka.Name: kafka.Factory(),
} // type -> plugin

// Data plugin
type Data struct {
	manager *plugins.Manager
	config  Config
	plugins []plugins.Plugin
}

// Start starts the data plugins that have been configured.
func (c *Data) Start(ctx context.Context) error {
	for i := range c.plugins {
		if err := c.plugins[i].Start(ctx); err != nil {
			return err
		}
	}
	c.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateOK})
	return nil
}

// Stop stops the dynamic discovery process if configured.
func (c *Data) Stop(ctx context.Context) {
	for i := range c.plugins {
		c.plugins[i].Stop(ctx)
	}
	c.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
}

// Reconfigure sets up the c.plugins field according to c.config
func (c *Data) Reconfigure(ctx context.Context, cfg interface{}) {
	for i := range c.plugins {
		c.plugins[i].Reconfigure(ctx, cfg)
	}
}

// Lookup returns the data plugin registered with the manager.
func Lookup(manager *plugins.Manager) *Data {
	if p := manager.Plugin(Name); p != nil {
		return p.(*Data)
	}
	return nil
}

func dataPluginFromConfig(cfg json.RawMessage) (plugins.Factory, string, error) {
	type typeConfig struct {
		Type string `json:"type"`
	}
	t := typeConfig{}
	if err := util.Unmarshal(cfg, &t); err != nil {
		return nil, "", err
	}
	dp, ok := dataPluginRegistery[t.Type]
	if !ok {
		return nil, "", fmt.Errorf("data plugin not found: %s", t.Type)
	}
	return dp, t.Type, nil
}
