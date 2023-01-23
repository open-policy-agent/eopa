package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config interface{}) plugins.Plugin {
	m.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
	p := &Data{
		manager: m,
		config:  config.(Config),
	}
	for _, dp := range p.config.DataPlugins {
		p.plugins = append(p.plugins, dp.Factory.New(p.manager, dp.Config))
	}
	p.Reconfigure(context.TODO(), p.config)
	return p
}

func (factory) Validate(manager *plugins.Manager, config []byte) (interface{}, error) {
	parsedConfig := Config{
		DataPlugins: make(map[string]DataPlugin),
	}
	initial := map[string]json.RawMessage{}
	if err := util.Unmarshal(config, &initial); err != nil {
		return nil, err
	}

	// TODO(sr): check path overlaps
	for path, dpConfig := range initial {
		dp, t, err := dataPluginFromConfig(dpConfig)
		if err != nil {
			return nil, err
		}

		// add the path to the data plugin's config
		m := map[string]any{}
		if err := util.Unmarshal(dpConfig, &m); err != nil {
			return nil, err
		}
		m["path"] = path
		delete(m, "type")
		validated, err := dp.Validate(manager, util.MustMarshalJSON(m))
		if err != nil {
			return nil, fmt.Errorf("data plugin %s (%s): %w", t, path, err)
		}
		parsedConfig.DataPlugins[path] = DataPlugin{Factory: dp, Config: validated}
	}
	return parsedConfig, nil
}
