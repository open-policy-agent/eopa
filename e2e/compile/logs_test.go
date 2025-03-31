//go:build e2e

package tests

import (
	cp "cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

type payload struct {
	Query        string         `json:"query"`
	Path         string         `json:"path"`
	Result       any            `json:"result"`
	Metrics      map[string]int `json:"metrics"`
	ID           int            `json:"req_id"`
	DecisionID   string         `json:"decision_id"`
	Labels       payloadLabels  `json:"labels"`
	NDBC         map[string]any `json:"nd_builtin_cache"`
	Input        any            `json:"input"`
	Erased       []string       `json:"erased"`
	Masked       []string       `json:"masked"`
	Timestamp    time.Time      `json:"timestamp"`
	Intermediate map[string]any `json:"intermediate_results"`
	Custom       map[string]any `json:"custom"`
}

type payloadLabels struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

var standardLabels = payloadLabels{
	Type:    "enterprise-opa",
	Version: os.Getenv("EOPA_VERSION"),
}

var stdIgnores = cmpopts.IgnoreFields(payload{}, "Timestamp", "Metrics", "DecisionID", "Labels.ID", "NDBC", "Intermediate")

func TestDecisionLogsCompileAPIResult(t *testing.T) {
	policy := `
package filters

# METADATA
# custom:
#   unknowns: [input.fruits]
#   mask_rule: data.filters.mask
include if input.fruits.name in input.favorites

default mask.fruits.supplier := {"replace": {"value": "***"}}
`

	configs := map[string]string{
		"top-level": `
decision_logs:
  console: true
`,
		"per-output": `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
      type: console
`,
	}
	eopaURL := fmt.Sprintf("http://localhost:%d", eopaHTTPPort)

	for c, config := range configs {
		t.Run(c, func(t *testing.T) {
			for _, tc := range []struct {
				query string
				path  string
			}{
				{path: "/filters/include"},
				{query: "data.filters.include"},
			} {
				t.Run(fmt.Sprintf("query=%s/path=%s", cp.Or(tc.query, "none"), cp.Or(tc.path, "none")), func(t *testing.T) {
					eopa, eopaOut, eopaErr := loadEnterpriseOPA(t, eopaHTTPPort, config)
					if err := eopa.Start(); err != nil {
						t.Fatal(err)
					}
					wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

					{ // store policy
						req, err := http.NewRequest("PUT", fmt.Sprintf("%s/v1/policies/policy.rego", eopaURL), strings.NewReader(policy))
						if err != nil {
							t.Fatalf("failed to create request: %v", err)
						}
						if _, err := http.DefaultClient.Do(req); err != nil {
							t.Fatalf("put policy: %v", err)
						}
					}

					{ // act: send Compile API request
						input := map[string]any{"favorites": []string{"banana", "orange"}}
						payload := map[string]any{
							"query": tc.query, // may be empty
							"input": input,
							"options": map[string]any{
								"targetSQLTableMappings": map[string]any{
									"postgresql": map[string]any{
										"fruits": map[string]string{
											"$self": "f",
											"name":  "n",
										},
									},
								},
							},
						}

						queryBytes, err := json.Marshal(payload)
						if err != nil {
							t.Fatalf("Failed to marshal JSON: %v", err)
						}
						req, err := http.NewRequest("POST",
							fmt.Sprintf("%s/v1/compile%s", eopaURL, tc.path), // tc.path could be empty
							strings.NewReader(string(queryBytes)))
						if err != nil {
							t.Fatalf("failed to create request: %v", err)
						}
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("Accept", "application/vnd.styra.sql.postgresql+json")
						resp, err := http.DefaultClient.Do(req)
						if err != nil {
							t.Fatalf("failed to execute request: %v", err)
						}
						defer resp.Body.Close()
						var respPayload struct {
							Result struct {
								Query any            `json:"query"`
								Masks map[string]any `json:"masks,omitempty"`
							} `json:"result"`
						}
						if err := json.NewDecoder(resp.Body).Decode(&respPayload); err != nil {
							t.Fatalf("failed to decode response: %v", err)
						}
						if exp, act := "WHERE f.n IN (E'banana', E'orange')", respPayload.Result.Query; exp != act {
							t.Errorf("response: expected %v, got %v (response: %v)", exp, act, respPayload)
						}
						exp, act := map[string]any{"fruits": map[string]any{"supplier": map[string]any{"replace": map[string]any{"value": "***"}}}}, respPayload.Result.Masks
						if diff := cmp.Diff(exp, act); diff != "" {
							t.Errorf("response: expected %v, got %v (response: %v)", exp, act, respPayload)
						}
					}

					var output io.Reader
					switch c {
					case "top-level":
						output = eopaErr
					case "per-output":
						output = eopaOut
					}

					logs := collectDL(t, output, 1)
					dl := payload{
						Query: tc.query,
						Path:  strings.TrimPrefix(tc.path, "/"),
						Result: map[string]any{
							"query": "WHERE f.n IN (E'banana', E'orange')",
							"masks": map[string]any{
								"fruits": map[string]any{
									"supplier": map[string]any{
										"replace": map[string]any{"value": "***"},
									},
								},
							},
						},
						Input: map[string]any{
							"favorites": []any{"banana", "orange"},
						},
						ID:     2, // PUT policy is #1
						Labels: standardLabels,
						Custom: map[string]any{
							"options": map[string]any{
								"nondeterministicBuiltins": false,
								"targetSQLTableMappings": map[string]any{
									"postgresql": map[string]any{
										"fruits": map[string]any{
											"$self": "f",
											"name":  "n",
										},
									},
								},
							},
							"unknowns":  []any{"input.fruits"},
							"type":      "eopa.styra.com/compile",
							"mask_rule": "data.filters.mask",
						},
					}
					if diff := cmp.Diff(dl, logs[0], stdIgnores); diff != "" {
						t.Errorf("diff: (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}

// collectDL either returns `exp` decision log payloads, or calls t.Fatal
func collectDL(t *testing.T, rdr io.Reader, exp int) []payload {
	t.Helper()
	for i := 0; i <= 3; i++ {
		time.Sleep(100 * time.Millisecond)
		ms := retrieveDLs(t, rdr)
		if act := len(ms); act == exp {
			return ms
		} else if act > exp || i == 3 {
			t.Fatalf("expected %d payloads, got %d", exp, act)
		}
	}
	return nil
}

func retrieveDLs(t *testing.T, rdr io.Reader) []payload {
	t.Helper()
	ms := []payload{}
	dec := json.NewDecoder(rdr)
	for {
		m := payload{}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode recorded DL: %v", err)
		}
		if m.Result != nil {
			ms = append(ms, m)
		}
	}
	return ms
}
