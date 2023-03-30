//go:build e2e

package decisionlogs

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/exp/maps"

	"github.com/open-policy-agent/opa/version"
)

type payload struct {
	Result     any            `json:"result"`
	Metrics    map[string]int `json:"metrics"`
	ID         int            `json:"req_id"`
	DecisionID string         `json:"decision_id"`
	Labels     payloadLabels  `json:"labels"`
	NDBC       map[string]any `json:"nd_builtin_cache"`
	Input      map[string]any `json:"input"`
	Erased     []string       `json:"erased"`
	Masked     []string       `json:"masked"`
}

type payloadLabels struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

var standardLabels = payloadLabels{
	Type:    "styra-load",
	Version: version.Version,
}

// This tests sends three API requests: one is logged as-is, one is dropped, and one is masked.
// It uses each of the three different buffer options (unbuffered, memory, disk).
func TestDecisionLogsConsoleOutput(t *testing.T) {
	ctx := context.Background()

	policy := `
package test
import future.keywords

# always succeeds, but adds nd_builtin_cache entry
coin if rand.intn("coin", 2)

drop if input.input.drop

mask contains {"op": "upsert", "path": "/input/replace", "value": "XXX"} if input.input.replace
mask contains {"op": "remove", "path": "/input/remove"} if input.input.remove
mask contains "/input/erase" if input.input.erase
`
	tests := []struct {
		buffer string
	}{
		{buffer: "unbuffered"},
		{buffer: "memory"},
		{buffer: "disk"},
	}
	configFmt := `
plugins:
  load_decision_logger:
    drop_decision: /test/drop
    mask_decision: /test/mask
    buffer:
      type: %s
    output:
      type: console
`
	for _, tc := range tests {
		t.Run("buffer="+tc.buffer, func(t *testing.T) {
			config := fmt.Sprintf(configFmt, tc.buffer)
			load, loadOut, loadErr := loadLoad(t, config, policy, false)
			if err := load.Start(); err != nil {
				t.Fatal(err)
			}
			waitForLog(ctx, t, loadErr, 1, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			{ // act 1: request is logged as-is
				req, err := http.NewRequest("POST", "http://localhost:28181/v1/data/test/coin", nil)
				if err != nil {
					t.Fatalf("http request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req.WithContext(ctx))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if exp, act := 200, resp.StatusCode; exp != act {
					t.Fatalf("expected status %d, got %d", exp, act)
				}
			}

			{ // act 2: request is dropped
				payload := strings.NewReader(`{"input": {"drop": "nooo"}}`)
				req, err := http.NewRequest("POST", "http://localhost:28181/v1/data/test/coin", payload)
				if err != nil {
					t.Fatalf("http request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req.WithContext(ctx))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if exp, act := 200, resp.StatusCode; exp != act {
					t.Fatalf("expected status %d, got %d", exp, act)
				}
			}

			{ // act 3: request is masked
				payload := strings.NewReader(`{"input": {"replace": ":)", "remove": ":)", "erase": ":)"}}`)
				req, err := http.NewRequest("POST", "http://localhost:28181/v1/data/test/coin", payload)
				if err != nil {
					t.Fatalf("http request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req.WithContext(ctx))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if exp, act := 200, resp.StatusCode; exp != act {
					t.Fatalf("expected status %d, got %d", exp, act)
				}
			}

			// assert: check that DL logs have been output as expected
			logs := collectDL(ctx, t, loadOut, false, 2)

			{ // log for act 1
				dl := payload{
					Result: true,
					ID:     1,
					Labels: standardLabels,
				}
				if diff := cmp.Diff(dl, logs[0], cmpopts.IgnoreFields(payload{}, "Metrics", "DecisionID", "Labels.ID", "NDBC")); diff != "" {
					t.Errorf("diff: (-want +got):\n%s", diff)
				}
				{
					exp := []string{"counter_regovm_eval_instructions",
						"counter_server_query_cache_hit",
						"timer_rego_input_parse_ns",
						"timer_rego_module_parse_ns",
						"timer_rego_query_compile_ns",
						"timer_rego_query_parse_ns",
						"timer_regovm_eval_ns",
						"timer_server_handler_ns",
					}
					act := maps.Keys(logs[0].Metrics)
					if diff := cmp.Diff(exp, act, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
						t.Errorf("unexpected log[0] metrics: (-want +got):\n%s", diff)
					}
				}
				{
					exp, act := []string{"rand.intn"}, maps.Keys(logs[0].NDBC)
					if diff := cmp.Diff(exp, act); diff != "" {
						t.Fatalf("unexpected NDBC: (-want, +got):\n%s", diff)
					}
				}
				{
					rand := logs[0].NDBC["rand.intn"]
					calls, ok := rand.(map[string]any)
					if !ok {
						t.Fatalf("rand.intn: expected %T, got %T: %[2]v", calls, rand)
					}
					exp, act := []string{`["coin",2]`}, maps.Keys(calls)
					if diff := cmp.Diff(exp, act); diff != "" {
						t.Fatalf("unexpected NDBC: (-want, +got):\n%s", diff)
					}
					value := calls[`["coin",2]`]
					num, ok := value.(float64)
					if !ok {
						t.Fatalf("rand.intn(\"coin\", 2): expected %T, got %T: %[2]v", num, value)
					}
					if num != 0 && num != 1 {
						t.Errorf("expected 0 or 1, got %v", num)
					}
				}
			}

			{ // log for act 3 (ignoring metrics)
				dl := payload{
					Result: true,
					Input:  map[string]any{"replace": "XXX"},
					Masked: []string{"/input/replace"},
					Erased: []string{"/input/erase", "/input/remove"},
					ID:     3,
					Labels: standardLabels,
				}
				if diff := cmp.Diff(dl, logs[1], cmpopts.IgnoreFields(payload{}, "Metrics", "DecisionID", "Labels.ID", "NDBC")); diff != "" {
					t.Errorf("diff: (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestDecisionLogsServiceOutput(t *testing.T) {
	ctx := context.Background()
	policy := `
package test
import future.keywords

coin if rand.intn("coin", 2)
`

	for _, tc := range []struct {
		note, configFmt string
		compressed      bool
	}{
		{
			note:       "type=service",
			compressed: true,
			configFmt: `
services:
- name: "dl-sink"
  url: "%s/prefix"
plugins:
  load_decision_logger:
    output:
      type: service
      service: dl-sink
`,
		},
		{
			note: "type=http",
			configFmt: `
plugins:
  load_decision_logger:
    output:
      type: http
      url: "%s/prefix/logs"
`,
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			buf := bytes.Buffer{}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path != "/prefix/logs":
				case r.Method != http.MethodPost:
				default: // all matches
					var src io.ReadCloser
					if tc.compressed {
						src, _ = gzip.NewReader(r.Body)
					} else {
						src = r.Body
					}
					io.Copy(&buf, src)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(ts.Close)
			load, _, loadErr := loadLoad(t, fmt.Sprintf(tc.configFmt, ts.URL), policy, false)
			if err := load.Start(); err != nil {
				t.Fatal(err)
			}
			waitForLog(ctx, t, loadErr, 1, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()

			{ // act: send API request
				req, err := http.NewRequest("POST", "http://localhost:28181/v1/data/test/coin", nil)
				if err != nil {
					t.Fatalf("http request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req.WithContext(ctx))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if exp, act := 200, resp.StatusCode; exp != act {
					t.Fatalf("expected status %d, got %d", exp, act)
				}
			}

			logs := collectDL(ctx, t, &buf, tc.compressed, 1)
			dl := payload{
				Result: true,
				ID:     1,
				Labels: standardLabels,
			}
			if diff := cmp.Diff(dl, logs[0], cmpopts.IgnoreFields(payload{}, "Metrics", "DecisionID", "Labels.ID", "NDBC")); diff != "" {
				t.Errorf("diff: (-want +got):\n%s", diff)
			}
		})
	}
}

func loadLoad(t *testing.T, config, policy string, opts ...any) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	var silent bool
	logLevel := "debug" // Needed for checking if server is ready
	for _, o := range opts {
		switch o := o.(type) {
		case bool:
			silent = o
		}
	}

	stdout, stderr := bytes.Buffer{}, bytes.Buffer{}
	dir := t.TempDir()
	confPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(confPath, []byte(config), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}
	policyPath := filepath.Join(dir, "eval.rego")
	if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}

	args := []string{
		"run",
		"--server",
		"--addr", "localhost:28181",
		"--config-file", confPath,
		"--log-level", logLevel,
		"--disable-telemetry",
	}
	load := exec.Command(binary(), append(args, policyPath)...)
	load.Stderr = &stderr
	load.Stdout = &stdout
	load.Env = append(load.Environ(),
		"STYRA_LOAD_LICENSE_TOKEN="+os.Getenv("STYRA_LOAD_LICENSE_TOKEN"),
		"STYRA_LOAD_LICENSE_KEY="+os.Getenv("STYRA_LOAD_LICENSE_KEY"),
	)

	t.Cleanup(func() {
		if load.Process == nil {
			return
		}
		if err := load.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		load.Wait()
		if testing.Verbose() && !silent {
			t.Logf("load stdout:\n%s", stdout.String())
			t.Logf("load stderr:\n%s", stderr.String())
		}
	})

	return load, &stdout, &stderr
}

func TestDecisionLogsFailsToStartWithTwoPlugins(t *testing.T) {
	config := `
decision_logs:
  console: true
plugins:
  load_decision_logger:
    buffer:
      type: memory
    output:
      type: console
`
	dir := t.TempDir()
	confPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(confPath, []byte(config), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}
	args := []string{
		"run",
		"--server",
		"--addr", "localhost:28181",
		"--config-file", confPath,
		"--log-level", "error",
		"--disable-telemetry",
	}

	stdout, stderr := bytes.Buffer{}, bytes.Buffer{}
	load := exec.Command(binary(), args...)
	load.Stderr = &stderr
	load.Stdout = &stdout
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	_ = load.Wait() // expected: exit 1
	if exp, act := ``, stdout.String(); exp != act {
		t.Errorf("stdout: expected %q, got %q", exp, act)
	}
	output := struct {
		Err   string
		Level string
	}{}
	if err := json.NewDecoder(&stderr).Decode(&output); err != nil {
		t.Fatal(err)
	}
	if exp, act := "load_decision_logger cannot be used together with OPA's decision logging", output.Err; exp != act {
		t.Errorf("err: expected %q, got %q", exp, act)
	}
	if exp, act := "error", output.Level; exp != act {
		t.Errorf("level: expected %q, got %q", exp, act)
	}
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "load"
	}
	return bin
}

func waitForLog(ctx context.Context, t *testing.T, rdr io.Reader, exp int, assert func(string) bool, dur time.Duration) {
	t.Helper()
	for i := 0; i <= 3; i++ {
		if i != 0 {
			time.Sleep(dur)
		}
		if act := retrieveReqCount(ctx, t, rdr, assert); act == exp {
			return
		} else if i == 3 {
			t.Fatalf("expected %d requests, got %d", exp, act)
		}
	}
	return
}

func retrieveReqCount(ctx context.Context, t *testing.T, rdr io.Reader, assert func(string) bool) int {
	t.Helper()
	c := 0
	dec := json.NewDecoder(rdr)
	for {
		m := struct {
			Msg string `json:"msg"`
		}{}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode recorded logs: %v", err)
		}
		if assert(m.Msg) {
			c++
		}
	}
	return c
}

// collectDL either returns `exp` decision log payloads, or calls t.Fatal
func collectDL(ctx context.Context, t *testing.T, rdr io.Reader, array bool, exp int) []payload {
	t.Helper()
	for i := 0; i <= 3; i++ {
		if i != 0 {
			time.Sleep(100 * time.Millisecond)
		}
		var ms []payload
		if array {
			if err := json.NewDecoder(rdr).Decode(&ms); err != nil {
				if err != io.EOF {
					t.Fatalf("decode recorded DL: %v", err)
				}
			}
		} else {
			ms = retrieveDLs(ctx, t, rdr)
		}
		if act := len(ms); act == exp {
			return ms
		} else if i == 3 {
			t.Fatalf("expected %d payloads, got %d", exp, act)
		}
	}
	return nil
}

func retrieveDLs(ctx context.Context, t *testing.T, rdr io.Reader) []payload {
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
