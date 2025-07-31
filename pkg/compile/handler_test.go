// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package compile_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/logging/test"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/test/e2e"
	topdown_cache "github.com/open-policy-agent/opa/v1/topdown/cache"

	"github.com/open-policy-agent/eopa/pkg/compile"
)

type Query struct {
	Query any `json:"query,omitempty"`
	Masks any `json:"masks,omitempty"`
}
type Response struct {
	Result struct {
		Query    any   `json:"query,omitempty"`
		Masks    any   `json:"masks,omitempty"`
		UCAST    Query `json:"ucast"` // NB: omitempty has no effect on nested struct fields (so the linter tells me)
		Postgres Query `json:"postgresql"`
		MySQL    Query `json:"mysql"`
		MSSQL    Query `json:"sqlserver"`
		SQLite   Query `json:"sqlite"`
	} `json:"result"`
	Metrics map[string]float64 `json:"metrics"`
	Hints   []map[string]any   `json:"hints"`
}

func TestCompileHandlerMultiTarget(t *testing.T) {
	t.Parallel()
	var roles map[string]any
	if err := json.Unmarshal(rolesJSON, &roles); err != nil {
		t.Fatalf("unmarshal roles: %v", err)
	}
	chnd, _, _, _ := setup(t, benchRego, map[string]any{"roles": roles})

	input := map[string]any{
		"user": "caesar",
		"tenant": map[string]any{
			"id":   2,
			"name": "acmecorp",
		},
	}
	path := "filters/include"
	target := "application/vnd.styra.multitarget+json"

	payload := map[string]any{ // NB(sr): unknowns are taken from metadata
		"input": input,
		"options": map[string]any{
			"targetDialects": []string{
				"sql+postgresql",
				"sql+mysql",
				"sql+sqlserver",
				"sql+sqlite",
				"ucast+prisma",
			},
		},
	}
	resp, httpResp := evalReq(t, chnd, path, payload, target)

	{ // check results
		if exp, act := "WHERE ((tickets.tenant = E'2' AND users.name = E'caesar') OR (tickets.tenant = E'2' AND tickets.assignee IS NULL AND tickets.resolved = FALSE))", resp.Result.Postgres.Query; exp != act {
			t.Errorf("postgres, want %s, got %s", exp, act)
		}
		if exp, act := "WHERE ((tickets.tenant = '2' AND users.name = 'caesar') OR (tickets.tenant = '2' AND tickets.assignee IS NULL AND tickets.resolved = FALSE))", resp.Result.MySQL.Query; exp != act {
			t.Errorf("mysql, want %s, got %s", exp, act)
		}
		if exp, act := "WHERE ((tickets.tenant = N'2' AND users.name = N'caesar') OR (tickets.tenant = N'2' AND tickets.assignee IS NULL AND tickets.resolved = FALSE))", resp.Result.MSSQL.Query; exp != act {
			t.Errorf("mssql, want %s, got %s", exp, act)
		}
		if exp, act := "WHERE ((tickets.tenant = '2' AND users.name = 'caesar') OR (tickets.tenant = '2' AND tickets.assignee IS NULL AND tickets.resolved = FALSE))", resp.Result.SQLite.Query; exp != act {
			t.Errorf("mssql, want %s, got %s", exp, act)
		}

		exp, act := map[string]any{
			"operator": "or",
			"type":     "compound",
			"value": []any{
				map[string]any{
					"operator": "and",
					"type":     "compound",
					"value": []any{
						map[string]any{"field": "tickets.tenant", "operator": "eq", "type": "field", "value": float64(2)},
						map[string]any{"field": "users.name", "operator": "eq", "type": "field", "value": "caesar"},
					},
				},
				map[string]any{
					"operator": "and",
					"type":     "compound",
					"value": []any{
						map[string]any{"field": "tickets.tenant", "operator": "eq", "type": "field", "value": float64(2)},
						map[string]any{"field": "tickets.assignee", "operator": "eq", "type": "field", "value": nil},
						map[string]any{"field": "tickets.resolved", "operator": "eq", "type": "field", "value": false},
					},
				},
			},
		}, resp.Result.UCAST.Query
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("ucast, (-want, +got):\n%s", diff)
		}
	}
	{ // also check for metrics
		if exp, act := map[string]float64{
			"timer_eval_constraints_ns":             0,
			"timer_eval_mask_rule_ns":               0,
			"timer_extract_annotations_unknowns_ns": 0,
			"timer_extract_annotations_mask_ns":     0,
			"timer_prep_partial_ns":                 0,
			"timer_rego_external_resolve_ns":        0,
			"timer_rego_partial_eval_ns":            0,
			"timer_rego_query_compile_ns":           0,
			"timer_rego_query_parse_ns":             0,
			"timer_server_handler_ns":               0,
			"timer_translate_queries_ns":            0,
		}, resp.Metrics; !compareMetrics(exp, act) {
			t.Fatalf("unexpected metrics: want %v, got %v", exp, act)
		}
	}
	{ // and response content-type
		if exp, act := target, httpResp.Header.Get("Content-Type"); exp != act {
			t.Errorf("expected content-type %s, got %s", exp, act)
		}
	}
}

func resetCaches(t testing.TB, s storage.Store) {
	ctx := context.Background()
	key := strconv.Itoa(rand.Int())
	txn := storage.NewTransactionOrDie(ctx, s, storage.WriteParams)
	if err := s.UpsertPolicy(ctx, txn, key, []byte(`package foo.num`+key)); err != nil {
		t.Fatalf("cache trigger: %v", err)
	}
	if err := s.Commit(ctx, txn); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestCompileHandlerMetrics(t *testing.T) {
	t.Parallel()
	var roles map[string]any
	if err := json.Unmarshal(rolesJSON, &roles); err != nil {
		t.Fatalf("unmarshal roles: %v", err)
	}
	chnd, _, _, mgr := setup(t, benchRego, map[string]any{"roles": roles})

	input := map[string]any{
		"user": "caesar",
		"tenant": map[string]any{
			"id":   2,
			"name": "acmecorp",
		},
	}
	path := "filters/include"
	targets := []string{
		"application/vnd.styra.sql.postgresql+json",
		"application/vnd.styra.ucast.prisma+json",
	}

	for _, target := range targets {
		t.Run(strings.Split(target, "/")[1], func(t *testing.T) {
			resetCaches(t, mgr.Store)
			payload := map[string]any{ // NB(sr): unknowns+mask_rule are taken from metadata
				"input": input,
			}
			{ // check metrics
				resp, _ := evalReq(t, chnd, path, payload, target)
				if exp, act := map[string]float64{
					"timer_eval_constraints_ns":             0,
					"timer_eval_mask_rule_ns":               0,
					"timer_extract_annotations_unknowns_ns": 0,
					"timer_extract_annotations_mask_ns":     0,
					"timer_prep_partial_ns":                 0,
					"timer_rego_external_resolve_ns":        0,
					"timer_rego_partial_eval_ns":            0,
					"timer_rego_query_compile_ns":           0,
					"timer_rego_query_parse_ns":             0,
					"timer_server_handler_ns":               0,
					"timer_translate_queries_ns":            0,
				}, resp.Metrics; !compareMetrics(exp, act) {
					t.Fatalf("unexpected metrics: want %v, got %v", exp, act)
				}
			}

			{ // Redo without resetting the cache: no extraction happens
				resp, _ := evalReq(t, chnd, path, payload, target)
				if n, ok := resp.Metrics["timer_extract_annotations_unknowns_ns"]; ok {
					t.Errorf("unexpected metric 'timer_extract_annotations_unknowns_ns': %v", n)
				}
				if n, ok := resp.Metrics["timer_extract_annotations_mask_ns"]; ok {
					t.Errorf("unexpected metric 'timer_extract_annotations_mask_ns': %v", n)
				}
			}

			{ // Redo without resetting the cache: no extraction happens
				resp, _ := evalReq(t, chnd, path, payload, target)
				if n, ok := resp.Metrics["timer_extract_annotations_unknowns_ns"]; ok {
					t.Errorf("unexpected metric 'timer_extract_annotations_unknowns_ns': %v", n)
				}
				if n, ok := resp.Metrics["timer_extract_annotations_mask_ns"]; ok {
					t.Errorf("unexpected metric 'timer_extract_annotations_mask_ns': %v", n)
				}
			}
		})
	}
}

// compareMetrics only checks that the keys of `exp` and `act` are the same,
// and that all values of `act` are non-zero.
func compareMetrics(exp, act map[string]float64) bool {
	return maps.EqualFunc(exp, act, func(_, b float64) bool {
		return b != 0
	})
}

type cf func(testing.TB, topdown_cache.InterQueryCache, topdown_cache.InterQueryValueCache)

func checks(fs ...cf) []cf { return fs }

func CHasEntry(key ast.Value) cf {
	return func(t testing.TB, iqc topdown_cache.InterQueryCache, _ topdown_cache.InterQueryValueCache) {
		t.Helper()
		if _, ok := iqc.Get(key); !ok {
			t.Fatalf("unexpected miss: %s", key)
		}
	}
}

func CHasNoEntry(key ast.Value) cf {
	return func(t testing.TB, iqc topdown_cache.InterQueryCache, _ topdown_cache.InterQueryValueCache) {
		t.Helper()
		if val, ok := iqc.Get(key); ok {
			t.Fatalf("unexpected hit: %s -> %v", key, val)
		}
	}
}

func VCHasEntry(key ast.Value) cf {
	return func(t testing.TB, _ topdown_cache.InterQueryCache, iqvc topdown_cache.InterQueryValueCache) {
		t.Helper()
		if _, ok := iqvc.Get(key); !ok {
			t.Fatalf("unexpected miss: %s", key)
		}
	}
}

func VCHasNoEntry(key ast.Value) cf {
	return func(t testing.TB, _ topdown_cache.InterQueryCache, iqvc topdown_cache.InterQueryValueCache) {
		t.Helper()
		if val, ok := iqvc.Get(key); ok {
			t.Fatalf("unexpected hit: %s -> %v", key, val)
		}
	}
}

func TestCompileHandlerCaches(t *testing.T) {
	t.Parallel()
	policy := `package filters
# METADATA
# scope: document
# custom:
#   unknowns: [input.foo]
include if {
	input.foo.col == http.send(input.req).body.p
}

include if {
	input.foo.col == regex.match("^foo$", input.bar)
}
`
	chnd, iqc, iqvc, _ := setup(t, []byte(policy), map[string]any{})

	path, target := "filters/include", "application/vnd.styra.sql.postgresql+json"

	req := map[string]any{
		"method":                       "GET",
		"url":                          testserver.URL,
		"force_cache":                  true,
		"force_cache_duration_seconds": 10,
		"cache_ignored_headers":        nil,
	}
	reqKey := ast.MustInterfaceToValue(req)
	reKey := ast.String("^foo$")

	for _, tc := range []struct {
		note   string
		input  map[string]any
		checks []cf
	}{
		{
			note: "http.send -> iqc",
			input: map[string]any{
				"req": req,
			},
			checks: checks(
				CHasEntry(reqKey),
				VCHasNoEntry(reqKey),
			),
		},
		{
			note: "regex.match -> iqvc",
			input: map[string]any{
				"bar": "foo",
			},
			checks: checks(
				CHasNoEntry(reKey),
				VCHasEntry(reKey),
			),
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			payload := map[string]any{ // NB(sr): unknowns are taken from metadata
				"input": tc.input,
			}
			resp, _ := evalReq(t, chnd, path, payload, target)
			if exp, act := "WHERE foo.col = TRUE", resp.Result.Query; exp != act {
				t.Errorf("response: expected %v, got %v", exp, act)
			}

			for _, check := range tc.checks {
				check(t, iqc, iqvc)
			}
		})
	}
}

func TestCompileHandlerHints(t *testing.T) {
	t.Parallel()
	typoRego := `package filters
# METADATA
# scope: document
# custom:
#   unknowns: [input.fruits]
include if input.fruits.name == "apple"
include if input.fruit.cost < input.max
`
	chnd, _, _, _ := setup(t, []byte(typoRego), map[string]any{})
	input := map[string]any{
		"max": 1,
	}
	path := "filters/include"
	target := "application/vnd.styra.sql.postgresql+json"

	payload := map[string]any{ // NB(sr): unknowns are taken from metadata
		"input": input,
	}
	resp, _ := evalReq(t, chnd, path, payload, target)

	{ // check results
		if exp, act := "WHERE fruits.name = E'apple'", resp.Result.Query; exp != act {
			t.Errorf("want %s, got %s", exp, act)
		}
	}
	{ // check hints
		exp := []map[string]any{
			{
				"location": map[string]any{
					"col":  float64(12),
					"row":  float64(7),
					"file": "test",
				},
				"message": "input.fruit.cost undefined, did you mean input.fruits.cost?",
			},
		}
		if diff := cmp.Diff(exp, resp.Hints); diff != "" {
			t.Errorf("unexpected hints (-want, +got):\n%s", diff)
		}
	}
}

func TestCompileHandlerMaskingRules(t *testing.T) {
	t.Parallel()
	var roles map[string]any
	if err := json.Unmarshal(rolesJSON, &roles); err != nil {
		t.Fatalf("unmarshal roles: %v", err)
	}

	input := map[string]any{
		"user": "caesar",
		"tenant": map[string]any{
			"id":   2,
			"name": "acmecorp",
		},
	}
	path := "filters/include"
	target := "application/vnd.styra.sql.postgresql+json"

	t.Run("mask rule from payload parameter", func(t *testing.T) {
		t.Parallel()
		chnd, _, _, _ := setup(t, benchRego, map[string]any{"roles": roles})
		payload := map[string]any{ // NB(sr): unknowns are taken from metadata
			"input": input,
			"options": map[string]any{
				"maskRule": "data.filters.masks",
			},
		}
		resp, _ := evalReq(t, chnd, path, payload, target)

		if exp, act := "WHERE ((tickets.tenant = E'2' AND users.name = E'caesar') OR (tickets.tenant = E'2' AND tickets.assignee IS NULL AND tickets.resolved = FALSE))", resp.Result.Query; exp != act {
			t.Fatalf("response: expected %v, got %v", exp, act)
		}
		exp, act := map[string]any{"tickets": map[string]any{"description": map[string]any{"replace": map[string]any{"value": "***"}}}}, resp.Result.Masks
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Fatalf("masks, (-want, +got):\n%s", diff)
		}
	})
	t.Run("mask rule from payload parameter + package-local matching", func(t *testing.T) {
		t.Parallel()
		chnd, _, _, _ := setup(t, benchRego, map[string]any{"roles": roles})
		payload := map[string]any{ // NB(sr): unknowns are taken from metadata
			"input": input,
			"options": map[string]any{
				"maskRule": "masks",
			},
		}
		resp, _ := evalReq(t, chnd, path, payload, target)

		if exp, act := "WHERE ((tickets.tenant = E'2' AND users.name = E'caesar') OR (tickets.tenant = E'2' AND tickets.assignee IS NULL AND tickets.resolved = FALSE))", resp.Result.Query; exp != act {
			t.Fatalf("response: expected %v, got %v", exp, act)
		}
		exp, act := map[string]any{"tickets": map[string]any{"description": map[string]any{"replace": map[string]any{"value": "***"}}}}, resp.Result.Masks
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Fatalf("masks, (-want, +got):\n%s", diff)
		}
	})
	t.Run("mask rule from rule annotation", func(t *testing.T) {
		t.Parallel()
		chnd, _, _, _ := setup(t, benchRego, map[string]any{"roles": roles})
		payload := map[string]any{
			"input": input,
		}
		resp, _ := evalReq(t, chnd, path, payload, target)

		if exp, act := "WHERE ((tickets.tenant = E'2' AND users.name = E'caesar') OR (tickets.tenant = E'2' AND tickets.assignee IS NULL AND tickets.resolved = FALSE))", resp.Result.Query; exp != act {
			t.Fatalf("response: expected %v, got %v", exp, act)
		}
		exp, act := map[string]any{"tickets": map[string]any{"id": map[string]any{"replace": map[string]any{"value": "***"}}}}, resp.Result.Masks
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Fatalf("masks, (-want, +got):\n%s", diff)
		}
	})
	t.Run("mask rule from rule annotation + package-local matching", func(t *testing.T) {
		t.Parallel()
		// Mangle the mask_rule annotation to make it package-local:
		benchRego := bytes.Replace(benchRego, []byte("mask_rule: data.filters.mask_from_annotation"), []byte("mask_rule: mask_from_annotation"), 1)

		chnd, _, _, _ := setup(t, benchRego, map[string]any{"roles": roles})
		payload := map[string]any{
			"input": input,
		}
		resp, _ := evalReq(t, chnd, path, payload, target)

		if exp, act := "WHERE ((tickets.tenant = E'2' AND users.name = E'caesar') OR (tickets.tenant = E'2' AND tickets.assignee IS NULL AND tickets.resolved = FALSE))", resp.Result.Query; exp != act {
			t.Fatalf("response: expected %v, got %v", exp, act)
		}
		exp, act := map[string]any{"tickets": map[string]any{"id": map[string]any{"replace": map[string]any{"value": "***"}}}}, resp.Result.Masks
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Fatalf("masks, (-want, +got):\n%s", diff)
		}
	})
}

func setup(t testing.TB, rego []byte, data any) (http.Handler, topdown_cache.InterQueryCache, topdown_cache.InterQueryValueCache, *plugins.Manager) {
	ctx := context.Background()
	l := test.New()
	l.SetLevel(logging.Debug)
	params := e2e.NewAPIServerTestParams()
	params.Logger = l
	trt, err := e2e.NewTestRuntime(params)
	if err != nil {
		t.Fatalf("test runtime: %v", err)
	}
	t.Cleanup(trt.Cancel)

	txn := storage.NewTransactionOrDie(ctx, trt.Runtime.Store, storage.WriteParams)
	if err := trt.Runtime.Store.UpsertPolicy(ctx, txn, "test", rego); err != nil {
		t.Fatalf("upsert policy: %v", err)
	}
	if err := trt.Runtime.Store.Write(ctx, txn, storage.AddOp, storage.Path{}, data); err != nil {
		t.Fatalf("write data: %v", err)
	}
	if err := trt.Runtime.Store.Commit(ctx, txn); err != nil {
		t.Fatalf("commit: %v", err)
	}

	trt.Runtime.Manager.Info = ast.MustParseTerm(`{"foo": "bar", "fox": 100}`)
	config, _ := topdown_cache.ParseCachingConfig(nil)
	iqc := topdown_cache.NewInterQueryCache(config)
	iqvc := topdown_cache.NewInterQueryValueCache(ctx, config)
	chnd := compile.Handler(l, iqc, iqvc)
	if err := chnd.SetManager(trt.Runtime.Manager); err != nil {
		t.Fatalf("set manager: %v", err)
	}

	return chnd, iqc, iqvc, trt.Runtime.Manager
}

func evalReq(t testing.TB, h http.Handler, path string, payload map[string]any, target string) (Response, *http.Response) {
	t.Helper()

	jsonData, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	// CAVEAT(sr): We're using the httptest machinery to simulate a request, so the actual
	// request path is ignored.
	req := httptest.NewRequest("POST", fmt.Sprintf("/v1/compile/%s?metrics=true", path), bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", target)
	rr := httptest.NewRecorder()
	router := http.NewServeMux()
	router.Handle("POST /v1/compile/{path...}", h)
	router.ServeHTTP(rr, req)
	exp := http.StatusOK
	if act := rr.Code; exp != act {
		t.Errorf("response: %s", rr.Body.String())
		t.Fatalf("status code: expected %d, got %d", exp, act)
	}
	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return resp, rr.Result()
}
