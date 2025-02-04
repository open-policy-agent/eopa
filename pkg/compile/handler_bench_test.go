package compile_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/logging/test"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/test/e2e"
	"github.com/styrainc/enterprise-opa-private/pkg/compile"
)

//go:embed bench_filters.rego
var benchRego []byte

//go:embed roles.json
var rolesJSON []byte

func BenchmarkCompileHandler(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	l := test.New()
	l.SetLevel(logging.Debug)
	params := e2e.NewAPIServerTestParams()
	params.Logger = l
	trt, err := e2e.NewTestRuntime(params)
	if err != nil {
		b.Fatalf("test runtime: %v", err)
	}
	b.Cleanup(trt.Cancel)

	txn := storage.NewTransactionOrDie(ctx, trt.Runtime.Store, storage.WriteParams)
	if err := trt.Runtime.Store.UpsertPolicy(ctx, txn, "test", benchRego); err != nil {
		b.Fatalf("upsert policy: %v", err)
	}
	var roles map[string]any
	if err := json.Unmarshal(rolesJSON, &roles); err != nil {
		b.Fatalf("unmarshal roles: %v", err)
	}
	if err := trt.Runtime.Store.Write(ctx, txn, storage.AddOp, storage.Path{"roles"}, roles); err != nil {
		b.Fatalf("write roles: %v", err)
	}
	if err := trt.Runtime.Store.Commit(ctx, txn); err != nil {
		b.Fatalf("commit: %v", err)
	}

	trt.Runtime.Manager.Info = ast.MustParseTerm(`{"foo": "bar", "fox": 100}`)
	chnd := compile.Handler(l)
	chnd.SetManager(trt.Runtime.Manager)

	input := map[string]any{
		"user": "caesar",
		"tenant": map[string]any{
			"id":   2,
			"name": "acmecorp",
		},
	}
	query := "data.filters.include"
	targets := map[string]string{
		"application/vnd.styra.sql+json":   "postgres",
		"application/vnd.styra.ucast+json": "prisma",
	}

	for target, dialect := range targets {
		b.Run(strings.Split(target, "/")[1], func(b *testing.B) {
			payload := map[string]any{ // NB(sr): unknowns are taken from metadata
				"input": input,
				"query": query,
				"options": map[string]any{
					"dialect": dialect,
				},
			}
			jsonData, err := json.Marshal(payload)
			if err != nil {
				b.Fatalf("Failed to marshal JSON: %v", err)
			}
			b.ResetTimer()

			for range b.N {
				req := httptest.NewRequest("POST", "/exp/compile", bytes.NewBuffer(jsonData))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", target)
				rr := httptest.NewRecorder()
				chnd.ServeHTTP(rr, req)
				exp := http.StatusOK
				if act := rr.Code; exp != act {
					b.Errorf("status code: expected %d, got %d", exp, act)
					var resp map[string]any
					if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
						b.Fatalf("unmarshal response: %v", err)
					}
					b.Fatalf("response: %v", resp)
				}
			}
		})
	}
}
