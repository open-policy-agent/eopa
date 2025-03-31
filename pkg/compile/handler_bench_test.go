package compile_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

//go:embed testdata/bench_filters.rego
var benchRego []byte

//go:embed testdata/roles.json
var rolesJSON []byte

var roles = func() any {
	var roles any
	if err := json.Unmarshal(rolesJSON, &roles); err != nil {
		panic(err)
	}
	return roles
}()

func BenchmarkCompileHandler(b *testing.B) {
	b.ReportAllocs()
	chnd, _ := setup(b, benchRego, roles)

	input := map[string]any{
		"user": "caesar",
		"tenant": map[string]any{
			"id":   2,
			"name": "acmecorp",
		},
	}
	query := "data.filters.include"
	targets := []string{
		"application/vnd.styra.sql.postgresql+json",
		"application/vnd.styra.ucast.prisma+json",
	}

	for _, target := range targets {
		b.Run(strings.Split(target, "/")[1], func(b *testing.B) {
			// NB(sr): Unknowns are provided with the request: we don't want to benchmark the cache here
			// The percentile-recording tests below is making use of the unknowns cache.
			payload := map[string]any{
				"input":    input,
				"query":    query,
				"unknowns": []string{"input.tickets", "input.users"},
			}
			jsonData, err := json.Marshal(payload)
			if err != nil {
				b.Fatalf("Failed to marshal JSON: %v", err)
			}
			b.ResetTimer()

			for range b.N {
				req := httptest.NewRequest("POST", "/v1/compile", bytes.NewBuffer(jsonData))
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
