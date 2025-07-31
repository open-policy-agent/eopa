// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package impact_test

import (
	"os"
	"testing"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/plugins/discovery"
	"github.com/open-policy-agent/opa/v1/topdown"

	"github.com/open-policy-agent/eopa/pkg/plugins/impact"
	inmem "github.com/open-policy-agent/eopa/pkg/storage"
)

func pluginMgr(t *testing.T, config string) *plugins.Manager {
	t.Helper()
	h := topdown.NewPrintHook(os.Stderr)
	opts := []func(*plugins.Manager){
		plugins.PrintHook(h),
		plugins.EnablePrintStatements(true),
	}
	if !testing.Verbose() {
		opts = append(opts, plugins.Logger(logging.NewNoOpLogger()))
		opts = append(opts, plugins.ConsoleLogger(logging.NewNoOpLogger()))
	}

	store := inmem.New()
	mgr, err := plugins.New([]byte(config), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	disco, err := discovery.New(mgr,
		discovery.Factories(map[string]plugins.Factory{
			impact.Name: impact.Factory(),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}
