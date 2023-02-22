package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	bjson "github.com/styrainc/load-private/pkg/json"
	"github.com/styrainc/load-private/pkg/plugins/data/http"
	"github.com/styrainc/load-private/pkg/plugins/data/kafka"
	"github.com/styrainc/load-private/pkg/plugins/data/ldap"
	"github.com/styrainc/load-private/pkg/plugins/data/okta"
	inmem "github.com/styrainc/load-private/pkg/store"
)

const Name = "data"

var dataPluginRegistery = map[string]plugins.Factory{
	kafka.Name: kafka.Factory(),
	http.Name:  http.Factory(),
	okta.Name:  okta.Factory(),
	ldap.Name:  ldap.Factory(),
} // type -> plugin

// Data plugin
type Data struct {
	manager *plugins.Manager
	config  Config
	plugins map[string]plugins.Plugin
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
	nextCfg := cfg.(Config).DataPlugins
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		for path, next := range nextCfg {
			if _, ok := c.config.DataPlugins[path]; ok { // updated path
				// Reconfigure done below outside of transaction
			} else { // new path
				c.plugins[path] = next.Factory.New(c.manager, next.Config)
			}
		}

		// what's not in the new config (nextCfg) needs to be cleaned up: remove data, stop plugin, delete from c.plugins
		for ref := range c.plugins {
			if _, ok := nextCfg[ref]; ok {
				continue // keep this data
			}
			path := strings.Split(ref, ".")
			if err := c.manager.Store.Write(ctx, txn, storage.RemoveOp, path, nil); err != nil {
				return err
			}

			// When we remove `kafka.updates`, and the data content is like this:
			//
			// kafka:
			//   whatever:
			//     something: true
			//   updates: [...]
			//
			// we'll leave `kafka` untouched.
			//
			// When the data content is only
			//
			// kafka:
			//   updates: [...]
			//
			// we'll also remove the (now-empty) kafka key

			for i := len(path) - 1; i > 0; i-- {
				// TODO(sr): refactor this so we don't have to type-assert here
				parent, err := c.manager.Store.(inmem.BJSONReader).ReadBJSON(ctx, txn, path[:i])
				if err != nil {
					return err
				}
				if o, ok := parent.(bjson.Object); ok {
					if o.Len() > 0 {
						break
					}
				}
				if err := c.manager.Store.Write(ctx, txn, storage.RemoveOp, path[:i], nil); err != nil {
					return err
				}
			}
			c.plugins[ref].Stop(ctx)
			delete(c.plugins, ref)
		}
		return nil
	}); err != nil {
		c.manager.Logger().Warn("failed to reconfigure %s plugin: %v", Name, err)
	}
	for path := range c.plugins {
		c.plugins[path].Reconfigure(ctx, nextCfg[path].Config)
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
