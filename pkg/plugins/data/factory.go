// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package data

import (
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/util"
)

type PathOverlapError struct {
	a, b ast.Ref // overlapping paths
}

func NewPathOverlapError(a, b ast.Ref) *PathOverlapError {
	return &PathOverlapError{a: a, b: b}
}

func (p *PathOverlapError) Error() string {
	return fmt.Sprintf("overlapping data paths: %q and %q", p.a, p.b)
}

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config any) plugins.Plugin {
	m.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
	p := &Data{
		manager: m,
		config:  config.(Config),
		plugins: make(map[string]plugins.Plugin, len(config.(Config).DataPlugins)),
	}
	for path, dp := range p.config.DataPlugins {
		p.plugins[path] = dp.Factory.New(p.manager, dp.Config)
	}
	return p
}

// check plugin path overlaps
//
//	plug and plug.suffix; overlap
func checkOverlappingPaths(data map[string]DataPlugin, path string) error {
	poe := &PathOverlapError{}

	var err error
	poe.a, err = ast.ParseRef("data." + path)
	if err != nil {
		return err
	}
	for key := range data {
		poe.b, err = ast.ParseRef("data." + key)
		if err != nil {
			return err
		}
		if poe.a.HasPrefix(poe.b) || poe.b.HasPrefix(poe.a) {
			return poe
		}
	}
	return nil
}

func (factory) Validate(manager *plugins.Manager, config []byte) (interface{}, error) {
	parsedConfig := Config{
		DataPlugins: make(map[string]DataPlugin),
	}
	initial := map[string]json.RawMessage{}
	if err := util.Unmarshal(config, &initial); err != nil {
		return nil, err
	}

	for path, dpConfig := range initial {
		if err := checkOverlappingPaths(parsedConfig.DataPlugins, path); err != nil {
			return nil, err
		}

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
