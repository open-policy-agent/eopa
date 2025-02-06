package compile_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/logging/test"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/test/e2e"
	"github.com/open-policy-agent/opa/v1/topdown/cache"
	"github.com/styrainc/enterprise-opa-private/pkg/compile"
)

type Query struct {
	Query any `json:"query,omitempty"`
}
type Response struct {
	Result struct {
		Query    any   `json:"query,omitempty"`
		UCAST    Query `json:"ucast,omitempty"`
		Postgres Query `json:"postgres,omitempty"`
		MySQL    Query `json:"mysql,omitempty"`
		MSSQL    Query `json:"sqlserver,omitempty"`
	} `json:"result"`
	Metrics map[string]float64 `json:"metrics"`
}

func TestCompileHandlerMultiTarget(t *testing.T) {
	var roles map[string]any
	if err := json.Unmarshal(rolesJSON, &roles); err != nil {
		t.Fatalf("unmarshal roles: %v", err)
	}
	chnd, _ := setup(t, benchRego, map[string]any{"roles": roles})

	input := map[string]any{
		"user": "caesar",
		"tenant": map[string]any{
			"id":   2,
			"name": "acmecorp",
		},
	}
	query := "data.filters.include"
	target := "application/vnd.styra.multitarget+json"

	payload := map[string]any{ // NB(sr): unknowns are taken from metadata
		"input": input,
		"query": query,
		"options": map[string]any{
			"targetDialects": []string{
				"sql+postgres",
				"sql+mysql",
				"sql+sqlserver",
				"ucast+prisma",
			},
		},
	}
	resp := evalReq(t, chnd, payload, target)

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
			"timer_eval_constraints_ns":      0,
			"timer_extract_annotations_ns":   0,
			"timer_prep_partial_ns":          0,
			"timer_rego_external_resolve_ns": 0,
			"timer_rego_partial_eval_ns":     0,
			"timer_rego_query_compile_ns":    0,
			"timer_rego_query_parse_ns":      0,
			"timer_server_handler_ns":        0,
			"timer_translate_queries_ns":     0,
		}, resp.Metrics; !compareMetrics(exp, act) {
			t.Fatalf("unexpected metrics: want %v, got %v", exp, act)
		}
	}
}

func TestCompileHandlerMetrics(t *testing.T) {
	var roles map[string]any
	if err := json.Unmarshal(rolesJSON, &roles); err != nil {
		t.Fatalf("unmarshal roles: %v", err)
	}
	chnd, _ := setup(t, benchRego, map[string]any{"roles": roles})

	input := map[string]any{
		"user": "caesar",
		"tenant": map[string]any{
			"id":   2,
			"name": "acmecorp",
		},
	}
	query := "data.filters.include"
	targets := []string{
		"application/vnd.styra.sql.postgres+json",
		"application/vnd.styra.ucast.prisma+json",
	}

	for _, target := range targets {
		t.Run(strings.Split(target, "/")[1], func(t *testing.T) {
			payload := map[string]any{ // NB(sr): unknowns are taken from metadata
				"input": input,
				"query": query,
			}
			resp := evalReq(t, chnd, payload, target)
			if exp, act := map[string]float64{
				"timer_eval_constraints_ns":      0,
				"timer_extract_annotations_ns":   0,
				"timer_prep_partial_ns":          0,
				"timer_rego_external_resolve_ns": 0,
				"timer_rego_partial_eval_ns":     0,
				"timer_rego_query_compile_ns":    0,
				"timer_rego_query_parse_ns":      0,
				"timer_server_handler_ns":        0,
				"timer_translate_queries_ns":     0,
			}, resp.Metrics; !compareMetrics(exp, act) {
				t.Fatalf("unexpected metrics: want %v, got %v", exp, act)
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

type cf func(testing.TB, cache.InterQueryCache, cache.InterQueryValueCache)

func checks(fs ...cf) []cf { return fs }

func CHasEntry(key ast.Value) cf {
	return func(t testing.TB, iqc cache.InterQueryCache, _ cache.InterQueryValueCache) {
		t.Helper()
		if _, ok := iqc.Get(key); !ok {
			t.Fatalf("unexpected miss: %s", key)
		}
	}
}

func CHasNoEntry(key ast.Value) cf {
	return func(t testing.TB, iqc cache.InterQueryCache, _ cache.InterQueryValueCache) {
		t.Helper()
		if val, ok := iqc.Get(key); ok {
			t.Fatalf("unexpected hit: %s -> %v", key, val)
		}
	}
}

func VCHasEntry(key ast.Value) cf {
	return func(t testing.TB, _ cache.InterQueryCache, iqvc cache.InterQueryValueCache) {
		t.Helper()
		if _, ok := iqvc.Get(key); !ok {
			t.Fatalf("unexpected miss: %s", key)
		}
	}
}

func VCHasNoEntry(key ast.Value) cf {
	return func(t testing.TB, _ cache.InterQueryCache, iqvc cache.InterQueryValueCache) {
		t.Helper()
		if val, ok := iqvc.Get(key); ok {
			t.Fatalf("unexpected hit: %s -> %v", key, val)
		}
	}
}

func TestCompileHandlerCaches(t *testing.T) {
	ctx := context.Background()
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

_use_md := rego.metadata.rule()
`
	chnd, mgr := setup(t, []byte(policy), map[string]any{})
	config, _ := cache.ParseCachingConfig(nil)
	iqc := cache.NewInterQueryCache(config)
	iqvc := cache.NewInterQueryValueCache(ctx, config)
	mgr.SetCaches(iqc, iqvc)

	query, target := "data.filters.include", "application/vnd.styra.sql.postgres+json"

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
				"query": query,
			}
			resp := evalReq(t, chnd, payload, target)
			if exp, act := "WHERE foo.col = TRUE", resp.Result.Query; exp != act {
				t.Errorf("response: expected %v, got %v", exp, act)
			}

			for _, check := range tc.checks {
				check(t, iqc, iqvc)
			}
		})
	}
}

func setup(t testing.TB, rego []byte, data any) (http.Handler, *plugins.Manager) {
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
		t.Fatalf("write roles: %v", err)
	}
	if err := trt.Runtime.Store.Commit(ctx, txn); err != nil {
		t.Fatalf("commit: %v", err)
	}

	trt.Runtime.Manager.Info = ast.MustParseTerm(`{"foo": "bar", "fox": 100}`)
	chnd := compile.Handler(l)
	chnd.SetManager(trt.Runtime.Manager)

	return chnd, trt.Runtime.Manager
}

func evalReq(t testing.TB, h http.Handler, payload map[string]any, target string) Response {
	t.Helper()

	jsonData, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/compile?metrics=true", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", target)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	exp := http.StatusOK
	if act := rr.Code; exp != act {
		t.Errorf("response: %s", rr.Body.String())
		t.Fatalf("status code: expected %d, got %d", exp, act)
	}
	var resp Response
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	return resp
}
