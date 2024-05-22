package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	opa_bundle "github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/bundle"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/benthos"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/git"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/http"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/kafka"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/ldap"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/mongodb"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/okta"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/s3"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/types"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

const Name = "data"

var dataPluginRegistry = map[string]plugins.Factory{
	git.Name:               git.Factory(),
	http.Name:              http.Factory(),
	kafka.Name:             kafka.Factory(),
	ldap.Name:              ldap.Factory(),
	mongodb.Name:           mongodb.Factory(),
	okta.Name:              okta.Factory(),
	s3.Name:                s3.Factory(),
	string(benthos.Pulsar): benthos.Factory(benthos.Pulsar),
} // type -> plugin

// Data plugin
type Data struct {
	manager *plugins.Manager
	config  Config
	plugins map[string]plugins.Plugin
}

type dataPlugin interface {
	plugins.Plugin
	Name() string
	Path() storage.Path
}

// Start starts the data plugins that have been configured.
func (c *Data) Start(ctx context.Context) error {
	for i := range c.plugins {
		if err := c.plugins[i].Start(ctx); err != nil {
			return err
		}
		dp := c.plugins[i].(dataPlugin)
		c.manager.Store.(inmem.DataPlugins).RegisterDataPlugin(dp.Name(), dp.Path())
	}

	// NOTE(sr): The registered trigger is run whenever a store write happens.
	// So while we have a `ctx` in scope here, it's not sensible to put that into
	// the closure.
	c.manager.RegisterCompilerTrigger(func(txn storage.Transaction) { c.compilerTrigger(context.TODO(), txn) })
	c.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateOK})
	return nil
}

// Stop stops the dynamic discovery process if configured.
func (c *Data) Stop(ctx context.Context) {
	for i := range c.plugins {
		c.plugins[i].Stop(ctx)

		dp := c.plugins[i].(dataPlugin)
		c.manager.Store.(inmem.DataPlugins).RegisterDataPlugin(dp.Name(), nil)
	}
	c.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})
}

// Reconfigure sets up the c.plugins field according to c.config
func (c *Data) Reconfigure(ctx context.Context, cfg interface{}) {
	nextCfg := cfg.(Config).DataPlugins
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		for path, next := range nextCfg {
			if _, ok := c.config.DataPlugins[path]; ok { //nolint:revive // updated path
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
			if err := c.manager.Store.(inmem.WriterUnchecked).WriteUnchecked(ctx, txn, storage.RemoveOp, path, nil); err != nil {
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
				if err := c.manager.Store.(inmem.WriterUnchecked).WriteUnchecked(ctx, txn, storage.RemoveOp, path[:i], nil); err != nil {
					return err
				}
			}
			c.plugins[ref].Stop(ctx)

			dp := c.plugins[ref].(dataPlugin)
			c.manager.Store.(inmem.DataPlugins).RegisterDataPlugin(dp.Name(), nil)

			delete(c.plugins, ref)
		}
		return nil
	}); err != nil {
		c.manager.Logger().Warn("failed to reconfigure %s plugin: %v", Name, err)
	}
	for path := range c.plugins {
		c.plugins[path].Reconfigure(ctx, nextCfg[path].Config)

		dp := c.plugins[path].(dataPlugin)
		c.manager.Store.(inmem.DataPlugins).RegisterDataPlugin(dp.Name(), dp.Path())
	}
}

func (c *Data) compilerTrigger(ctx context.Context, txn storage.Transaction) {
	for path := range c.plugins {
		if tr, ok := c.plugins[path].(types.Triggerer); ok {
			if err := tr.Trigger(ctx, txn); err != nil {
				c.manager.Logger().Warn("%s plugin trigger failed: %v", Name, err)
			}
		}
	}

	// Check if any of the bundle roots overlap with our data plugin roots.
	// If they do, we'll log an error-level message and flick the data plugin status to ERROR.
	bndles, err := bundle.ReadBundleNamesFromStore(ctx, c.manager.Store, txn)
	if storage.IsNotFound(err) { // nothing to check
		return
	}
	if err != nil {
		c.Error("data plugin: read bundle names from store: %v", err)
		return
	}
	roots := make([]string, 0, len(bndles))
	for i := range bndles {
		rs, err := bundle.ReadBundleRootsFromStore(ctx, c.manager.Store, txn, bndles[i])
		if err != nil {
			c.Error("data plugin: read bundle roots for %s from store: %v", bndles[i], err)
			return
		}
		roots = append(roots, rs...)
	}
	for path := range c.plugins {
		dp := c.plugins[path].(dataPlugin)
		pluginRoot := dp.Path().String()[1:] // drop first `/`
		if opa_bundle.RootPathsContain(roots, pluginRoot) {
			c.Error("%s plugin: data.%s overlaps with bundle root %v", dp.Name(), path, roots)
			// continue here, there could be more than one overlap
		}
	}
}

func (c *Data) Error(fmt string, fs ...any) {
	c.manager.Logger().WithFields(map[string]any{"plugin": Name}).Error(fmt, fs...)
	c.manager.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateErr})
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
	dp, ok := dataPluginRegistry[t.Type]
	if !ok {
		return nil, "", fmt.Errorf("data plugin not found: %s", t.Type)
	}
	return dp, t.Type, nil
}
