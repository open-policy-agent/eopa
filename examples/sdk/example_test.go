// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

// Package sdk_test demonstrates the use of OPA's sdk package,
// github.com/open-policy-agent/opa/sdk, with EOPA's VM code,
// storage, and all plugins.
package sdk_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/sdk"
	"github.com/open-policy-agent/opa/v1/storage"

	eopa_sdk "github.com/open-policy-agent/eopa/pkg/sdk"
)

func ExampleDataPlugin() {
	ctx := context.Background()
	opts := eopa_sdk.DefaultOptions()
	opts.Config = strings.NewReader(fmt.Sprintf(`
plugins:
  data:
    roles:
      type: http
      url: %[1]s/example
`, testserver.URL))

	store := opts.Store
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "example", []byte(`package example
import rego.v1

default allow := false
allow if "admin" in data.roles[input.user]
`))
	}); err != nil {
		panic(err)
	}

	o, err := sdk.New(ctx, opts)
	if err != nil {
		panic(err)
	}
	m := metrics.New()
	do := sdk.DecisionOptions{
		Path:    "/example/allow",
		Input:   map[string]any{"user": "alice"},
		Metrics: m,
	}
	waitForData(o, "/roles")

	dec, err := o.Decision(ctx, do)
	if err != nil {
		panic(err)
	}
	o.Stop(ctx)

	fmt.Printf("result: %v\n", dec.Result)
	fmt.Printf("eval_instr: %d", m.Counter("regovm_eval_instructions").Value())

	// Output:
	// result: true
	// eval_instr: 22
}

func ExampleSQLSend() {
	// NOTE: We're using the sqlite database from https://docs.styra.com/load/tutorials/abac-with-sql
	ctx := context.Background()
	opts := eopa_sdk.DefaultOptions()
	o, err := sdk.New(ctx, opts)
	if err != nil {
		panic(err)
	}

	store := opts.Store
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "example", []byte(`package example

import rego.v1

query := sql.send({
        "driver": "sqlite",
        "data_source_name": "file:testdata/company.db",
        "query": "SELECT * FROM subordinates WHERE manager = $1 AND subordinate = $2",
        "args": [input.user, input.resource],
    })
`))
	}); err != nil {
		panic(err)
	}

	do := sdk.DecisionOptions{
		Path:  "/example/query",
		Input: map[string]any{"user": "bob", "resource": "alice"},
	}
	dec, err := o.Decision(ctx, do)
	if err != nil {
		panic(err)
	}
	o.Stop(ctx)

	fmt.Printf("result: %v\n", dec.Result)
	// Output:
	// result: map[rows:[[1 bob alice]]]
}

func ExampleDecisionLogsPlugin() {
	ctx := context.Background()
	opts := eopa_sdk.DefaultOptions()
	opts.Config = strings.NewReader(fmt.Sprintf(`
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    buffer:
      type: unbuffered
    output:
      type: http
      url: %[1]s/logs
`, dlSink.URL))

	store := opts.Store
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "example", []byte(`package example
import rego.v1

coin if rand.intn("flip", 1) == 0
`))
	}); err != nil {
		panic(err)
	}

	o, err := sdk.New(ctx, opts)
	if err != nil {
		panic(err)
	}
	do := sdk.DecisionOptions{
		Path: "/example/coin",
	}

	dec, err := o.Decision(ctx, do)
	if err != nil {
		panic(err)
	}
	o.Stop(ctx)

	var log struct {
		ID     string `json:"decision_id"`
		Result any    `json:"result"`
	}
	if err := json.NewDecoder(&dlBuffer).Decode(&log); err != nil {
		panic(err)
	}

	if dec.Result == log.Result {
		fmt.Println("results match.")
	}
	if log.ID == dec.ID {
		fmt.Println("decision IDs match.")
	}
	// Output:
	// results match.
	// decision IDs match.
}

func ExampleBundles() {
	ctx := context.Background()
	opts := eopa_sdk.DefaultOptions()
	opts.Config = strings.NewReader(fmt.Sprintf(`
services:
- name: bndl
  url: %[1]s
bundles:
  bundle.tar.gz:
    service: bndl
`, bundleServer.URL))

	o, err := sdk.New(ctx, opts)
	if err != nil {
		panic(err)
	}
	defer o.Stop(ctx)

	waitForData(o, "/roles")

	do := sdk.DecisionOptions{
		Path:  "/test/allow",
		Input: map[string]any{"action": "create", "user": "alice"},
	}

	dec, err := o.Decision(ctx, do)
	if err != nil {
		panic(err)
	}

	fmt.Printf("result: %v\n", dec.Result)
	// Output:
	// result: true
}

func ExampleBJSONBundles() {
	ctx := context.Background()
	opts := eopa_sdk.DefaultOptions()
	opts.Logger = logging.New()
	opts.Config = strings.NewReader(fmt.Sprintf(`
services:
- name: bndl
  url: %[1]s
bundles:
  bundle.bjson.tar.gz:
    service: bndl
`, bundleServer.URL))

	o, err := sdk.New(ctx, opts)
	if err != nil {
		panic(err)
	}
	defer o.Stop(ctx)

	waitForData(o, "/roles")

	do := sdk.DecisionOptions{
		Path:  "/test/allow",
		Input: map[string]any{"action": "create", "user": "alice"},
	}

	dec, err := o.Decision(ctx, do)
	if err != nil {
		panic(err)
	}

	fmt.Printf("result: %v\n", dec.Result)
	// Output:
	// result: true
}

func ExampleBJSONBundleViaDiscovery() {
	ctx := context.Background()

	// Note: for demonstration purposes only. Not required for SDK usage.
	os.Setenv("BUNDLE_HOST", bundleServer.URL)

	opts := eopa_sdk.DefaultOptions()
	opts.Logger = logging.New()
	opts.V0Compatible = true // Needed for the BJSON bundle.
	opts.Config = strings.NewReader(fmt.Sprintf(`
services:
- name: bndl
  url: %[1]s
discovery:
  resource: disco.tgz
  decision: disco/config
`, bundleServer.URL))

	o, err := sdk.New(ctx, opts)
	if err != nil {
		panic(err)
	}
	defer o.Stop(ctx)

	waitForData(o, "/roles")

	do := sdk.DecisionOptions{
		Path:  "/test/allow",
		Input: map[string]any{"action": "create", "user": "alice"},
	}

	dec, err := o.Decision(ctx, do)
	if err != nil {
		panic(err)
	}

	fmt.Printf("result: %v\n", dec.Result)
	// Output:
	// result: true
}

func waitForData(o *sdk.OPA, path string) {
	dec, err := o.Decision(context.Background(), sdk.DecisionOptions{Path: path})
	if err != nil {
		panic(err)
	}
	if dec.Result != nil {
		if m, ok := dec.Result.(map[string]any); ok && len(m) > 0 {
			return
		}
	}
	time.Sleep(100 * time.Millisecond)
}

var testserver = srv(func(w http.ResponseWriter, _ *http.Request) error {
	return json.NewEncoder(w).Encode(map[string]any{
		"alice": []string{"admin"},
		"bob":   []string{"tester", "reader"},
	})
})

var (
	dlBuffer = bytes.Buffer{}
	dlSink   = srv(func(_ http.ResponseWriter, r *http.Request) error {
		_, err := io.Copy(&dlBuffer, r.Body)
		return err
	})
)

var bundleServer = httptest.NewServer(http.FileServer(http.Dir("testdata")))

func srv(f func(http.ResponseWriter, *http.Request) error) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := f(w, r); err != nil {
			w.WriteHeader(500)
			fmt.Fprintln(w, err.Error())
		}
	}))
}
