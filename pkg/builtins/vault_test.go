package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	testcontainersvault "github.com/testcontainers/testcontainers-go/modules/vault"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/topdown/cache"

	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func TestVaultSend(t *testing.T) {
	vault, address, token := startVault(t, "kv2", "test", map[string]string{"foo": "bar"})
	defer vault.Terminate(context.Background())
	now := time.Now()

	tests := []struct {
		note                string
		query               string
		result              string
		error               string
		doNotResetCache     bool
		time                time.Time
		interQueryCacheHits int
	}{
		{
			note:  "missing parameter(s)",
			query: `p := vault.send({"address": "%s", "token": "%s"})`,
			error: `eval_type_error: vault.send: operand 1 missing required request parameters(s): {"kv2_get"}`,
		},
		{
			note:   "a single secret",
			query:  `p := vault.send({"address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}})`,
			result: `{{"result": {"p": {"data": {"foo": "bar"}}}}}`,
		},
		{
			note: "intra-query cache",
			query: `p = [ resp1, resp2 ] {
				vault.send({"address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}}, resp1)
				vault.send({"address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}}, resp2) # cached
				}`,
			result: `{{"result": {"p": [{"data": {"foo": "bar"}}, {"data": {"foo": "bar"}}]}}}`,
		},
		{
			note:   "inter-query query cache warmup (default duration)",
			query:  `p := vault.send({"cache": true, "address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}})`,
			result: `{{"result": {"p": {"data": {"foo": "bar"}}}}}`,
		},
		{
			note:                "inter-query query cache check (default duration, valid)",
			query:               `p := vault.send({"cache": true, "address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}})`,
			result:              `{{"result": {"p": {"data": {"foo": "bar"}}}}}`,
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(interQueryCacheDurationDefault - 1),
			interQueryCacheHits: 1,
		},
		{
			note:            "inter-query query cache check (default duration, expired)",
			query:           `p := vault.send({"cache": true, "address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}})`,
			result:          `{{"result": {"p": {"data": {"foo": "bar"}}}}}`,
			doNotResetCache: true, // keep the warmup results
			time:            now.Add(interQueryCacheDurationDefault),
		},
		{
			note:   "inter-query query cache warmup (explicit duration)",
			query:  `p := vault.send({"cache": true, "cache_duration": "10s", "address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}})`,
			result: `{{"result": {"p": {"data": {"foo": "bar"}}}}}`,
		},
		{
			note:                "inter-query query cache check (explicit duration, valid)",
			query:               `p := vault.send({"cache": true, "cache_duration": "10s", "address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}})`,
			result:              `{{"result": {"p": {"data": {"foo": "bar"}}}}}`,
			doNotResetCache:     true, // keep the warmup results
			time:                now.Add(10*time.Second - 1),
			interQueryCacheHits: 1,
		},
		{
			note:            "inter-query query cache check (explicit duration, expired)",
			query:           `p := vault.send({"cache": true, "cache_duration": "10s", "address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "test"}})`,
			result:          `{{"result": {"p": {"data": {"foo": "bar"}}}}}`,
			doNotResetCache: true, // keep the warmup results
			time:            now.Add(10 * time.Second),
		},
		{
			note:  "error w/o raise",
			query: `p := vault.send({"address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "nonexisting"}})`,
			error: `eval_builtin_error: vault.send: secret not found: at kv2/data/nonexisting`,
		},
		{
			note:   "error with raise",
			query:  `p := vault.send({"address": "%s", "token": "%s", "kv2_get": {"mount_path": "kv2", "path": "nonexisting"}, "raise_error": false})`,
			result: `{{"result": {"p": {"error": {"message": "secret not found: at kv2/data/nonexisting"}}}}}`,
		},
	}

	var maxSize int64 = 1024 * 1024
	interQueryCache := cache.NewInterQueryCache(&cache.Config{
		InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
			MaxSizeBytes: &maxSize,
		},
	})

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			query := fmt.Sprintf(tc.query, address, token)
			if tc.time.IsZero() {
				tc.time = now
			}
			if !tc.doNotResetCache {
				interQueryCache = cache.NewInterQueryCache(&cache.Config{
					InterQueryBuiltinCache: cache.InterQueryBuiltinCacheConfig{
						MaxSizeBytes: &maxSize,
					},
				})
			}

			executeVault(t, interQueryCache, "package t\n"+query, "t", tc.result, tc.error, tc.time, tc.interQueryCacheHits)
		})
	}
}

func executeVault(tb testing.TB, interQueryCache cache.InterQueryCache, module string, query string, expectedResult string, expectedError string, time time.Time, expectedInterQueryCacheHits int) {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(module),
				Parsed: ast.MustParseModule(module),
			},
		},
	}

	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(query)
	if err := compiler.Build(context.Background()); err != nil {
		tb.Fatal(err)
	}

	var policy ir.Policy
	if err := json.Unmarshal(compiler.Bundle().PlanModules[0].Raw, &policy); err != nil {
		tb.Fatal(err)
	}

	executable, err := vm.NewCompiler().WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}

	_, ctx := vm.WithStatistics(context.Background())
	metrics := metrics.New()
	v, err := vm.NewVM().WithExecutable(executable).Eval(ctx, query, vm.EvalOpts{
		Metrics:                metrics,
		Time:                   time,
		Cache:                  builtins.Cache{},
		InterQueryBuiltinCache: interQueryCache,
		StrictBuiltinErrors:    true,
	})
	if expectedError != "" {
		if !strings.HasPrefix(err.Error(), expectedError) {
			tb.Fatalf("unexpected error: %v", err)
		}

		return
	}
	if err != nil {
		tb.Fatal(err)
	}

	if t := ast.MustParseTerm(expectedResult); v.Compare(t.Value) != 0 {
		tb.Fatalf("got %v wanted %v\n", v, expectedResult)
	}

	if hits := metrics.Counter(vaultSendInterQueryCacheHits).Value().(uint64); hits != uint64(expectedInterQueryCacheHits) {
		tb.Fatalf("got %v hits, wanted %v\n", hits, expectedInterQueryCacheHits)
	}
}

func startVault(t *testing.T, mount, path string, data map[string]string) (*testcontainersvault.VaultContainer, string, string) {
	return startVaultMulti(t, mount, map[string]map[string]string{path: data})
}

func startVaultMulti(t *testing.T, mount string, data map[string]map[string]string) (*testcontainersvault.VaultContainer, string, string) {
	t.Helper()

	token := "root-token"
	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithImage("hashicorp/vault:1.13.3"),
		testcontainersvault.WithToken(token),
	}

	for path, data := range data {
		d := ""
		for k, v := range data {
			d += k + "=" + v + " "
		}
		// NOTE(sr): "secret" seems to be enabled by default, as this would fail with
		//  error occurred during enable mount: path=secret/ error="path is already in use at secret/"
		if mount != "secret" {
			opts = append(opts, testcontainersvault.WithInitCommand(fmt.Sprintf("secrets enable --version=2 --path=%s kv", mount)))
		}

		opts = append(opts, testcontainersvault.WithInitCommand(fmt.Sprintf("kv put %s/%s %s", mount, path, d)))
	}

	ctx := context.Background()
	srv, err := testcontainersvault.RunContainer(ctx, opts...)
	if err != nil {
		t.Fatal(err)
	}

	address, err := srv.HttpHostAddress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return srv, address, token
}
