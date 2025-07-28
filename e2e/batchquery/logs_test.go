//go:build e2e

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/open-policy-agent/eopa/e2e/utils"
	"github.com/open-policy-agent/eopa/e2e/wait"
)

var eopaHTTPPort int

func TestMain(m *testing.M) {
	r := rand.New(rand.NewSource(2908))
	for {
		port := r.Intn(38181) + 1
		if utils.IsTCPPortBindable(port) {
			eopaHTTPPort = port
			break
		}
	}

	os.Exit(m.Run())
}

type payload struct {
	Path            string         `json:"path"`
	Result          any            `json:"result"`
	Metrics         map[string]int `json:"metrics"`
	ID              int            `json:"req_id"`
	DecisionID      string         `json:"decision_id"`
	BatchDecisionID string         `json:"batch_decision_id"`
	Labels          payloadLabels  `json:"labels"`
	NDBC            map[string]any `json:"nd_builtin_cache"`
	Input           any            `json:"input"`
	Erased          []string       `json:"erased"`
	Masked          []string       `json:"masked"`
	Timestamp       time.Time      `json:"timestamp"`
	Intermediate    map[string]any `json:"intermediate_results"`
	Custom          map[string]any `json:"custom"`
}

type payloadLabels struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

var standardLabels = payloadLabels{
	Type: "eopa",
}

var stdIgnores = cmpopts.IgnoreFields(payload{},
	"Timestamp",
	"Metrics",
	"BatchDecisionID",
	"DecisionID",
	"ID",
	"Labels.ID",
	"Labels.Version",
	"NDBC",
	"Intermediate",
)

func TestDecisionLogsBatchQueryAPIResult(t *testing.T) {
	policy := `
package testmod

import rego.v1
import input.req1
import input.req2 as reqx
import input.req3.attr1

p contains x if { q[x]; not r[x] }
q contains x if { data.x.y[i] = x }
r contains x if { data.x.z[i] = x }
g = true if { req1.a[0] = 1; reqx.b[i] = 1 }
h = true if { attr1[i] > 1 }
gt1 = true if { req1 > 1 }
arr = [1, 2, 3, 4] if { true }
undef = true if { false }
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
			path := "testmod/gt1"
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

			{ // act: send Batch Query API request
				payload := map[string]any{"inputs": map[string]any{"AAA": map[string]any{"req1": 2}, "BBB": map[string]any{"req1": 3}, "CCC": map[string]any{"req1": 4}}}

				reqBytes, err := json.Marshal(payload)
				if err != nil {
					t.Fatalf("Failed to marshal JSON: %v", err)
				}
				req, err := http.NewRequest("POST",
					fmt.Sprintf("%s/v1/batch/data/%s", eopaURL, path),
					bytes.NewReader(reqBytes))
				if err != nil {
					t.Fatalf("failed to create request: %v", err)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", "application/json")
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("failed to execute request: %v", err)
				}
				defer resp.Body.Close()
				var respPayload struct {
					Responses map[string]any `json:"responses"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&respPayload); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				exp := map[string]any{
					"AAA": map[string]any{"result": true},
					"BBB": map[string]any{"result": true},
					"CCC": map[string]any{"result": true},
				}
				act := respPayload.Responses
				// Thanks to Claude 3.7 Sonnet for this ignore filter:
				ignoreDecisionId := cmp.FilterPath(func(p cmp.Path) bool {
					// Check if we're looking at a map key named "decision_id"
					if len(p) > 0 {
						if mapIdx, ok := p.Last().(cmp.MapIndex); ok {
							if key, ok := mapIdx.Key().Interface().(string); ok {
								return key == "decision_id"
							}
						}
					}
					return false
				}, cmp.Ignore())
				if diff := cmp.Diff(exp, act, ignoreDecisionId); diff != "" {
					t.Errorf("response: expected %v, got %v (response: %v)", exp, act, respPayload)
				}
			}

			// For the Decision Logs, we ignore the ID field, and check those manually later to ensure they fall in the right range.
			// Batch Query requests do not have a guaranteed evaluation order, so the IDs may arrive out-of-order.
			var output io.Reader
			switch c {
			case "top-level":
				output = eopaErr
			case "per-output":
				output = eopaOut
			}

			logs := collectDL(t, output, 3)
			dl1 := payload{
				Path:   path,
				Result: true,
				Input: map[string]any{
					"req1": float64(2),
				},
				ID:     2, // PUT policy is #1
				Labels: standardLabels,
				Custom: map[string]any{
					"type": "eopa.styra.com/batch",
				},
			}
			dl2 := payload{
				Path:   path,
				Result: true,
				Input: map[string]any{
					"req1": float64(3),
				},
				ID:     2, // PUT policy is #1
				Labels: standardLabels,
				Custom: map[string]any{
					"type": "eopa.styra.com/batch",
				},
			}
			dl4 := payload{
				Path:   path,
				Result: true,
				Input: map[string]any{
					"req1": float64(2),
				},
				// ID:     4, // PUT policy is #1
				Labels: standardLabels,
				Custom: map[string]any{
					"type": "eopa.styra.com/batch",
				},
			}
			// Scan over the logs, and pattern match each expected DL to the matching log entry.
			for _, dl := range []payload{dl1, dl2, dl4} {
				foundMatch := false
				for _, l := range logs {
					if reflect.DeepEqual(dl.Input, l.Input) {
						foundMatch = true
						if diff := cmp.Diff(dl, l, stdIgnores); diff != "" {
							t.Errorf("diff: (-want +got):\n%s", diff)
						}
					}
				}
				if !foundMatch {
					t.Fatalf("expected to find a decision log entry, but did not. Missing item: %v", dl)
				}
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

func loadEnterpriseOPA(t *testing.T, httpPort int, config string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	stdout, stderr := bytes.Buffer{}, bytes.Buffer{}

	tempDir := t.TempDir()
	args := []string{
		"run",
		"--server",
		"--addr", fmt.Sprintf("localhost:%d", httpPort),
		"--log-level=debug",
		"--disable-telemetry",
	}
	if config != "" {
		configFile := tempDir + "/config.yaml"
		if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--config-file", configFile)
	}
	bin := os.Getenv("BINARY")
	if bin == "" {
		bin = "eopa"
	}
	eopa := exec.Command(bin, args...)
	eopa.Stderr = &stderr
	eopa.Stdout = &stdout

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		if err := eopa.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		eopa.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("eopa stdout:\n%s", stdout.String())
			t.Logf("eopa stderr:\n%s", stderr.String())
		}
	})

	return eopa, &stdout, &stderr
}
