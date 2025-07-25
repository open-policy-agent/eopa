//go:build e2e

package decisionlogs

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/exp/maps"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

type payload struct {
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
}

type payloadLabels struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

var standardLabels = payloadLabels{
	Type: "enterprise-opa",
}

var stdIgnores = cmpopts.IgnoreFields(payload{},
	"Timestamp",
	"Metrics",
	"DecisionID",
	"Labels.ID",
	"Labels.Version",
	"NDBC",
	"Intermediate",
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

// This tests sends three API requests: one is logged as-is, one is dropped, and one is masked.
// It uses each of the three different buffer options (unbuffered, memory, disk).
func TestDecisionLogsConsoleOutput(t *testing.T) {
	policy := `
package test
import rego.v1

# always succeeds, but adds nd_builtin_cache entry
coin if rand.intn("coin", 2)

drop if {
	print(input)
	input.input.drop
}

mask contains {"op": "upsert", "path": "/input/replace", "value": "XXX"} if input.input.replace
mask contains {"op": "remove", "path": "/input/remove"} if input.input.remove
mask contains "/input/erase" if input.input.erase
`
	buffers := []struct {
		buffer string
	}{
		{buffer: "unbuffered"},
		{buffer: "memory"},
		{buffer: "disk"},
	}

	dropAndMask := map[string]string{
		"top-level": `
decision_logs:
  plugin: eopa_dl
  drop_decision: /test/drop
  mask_decision: /test/mask
plugins:
  eopa_dl:
    buffer:
      type: %s
    output:
      type: console
`,
		"per-output": `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    buffer:
      type: %s
    output:
      type: console
      drop_decision: /test/drop
      mask_decision: /test/mask
`,
	}

	for _, tc := range buffers {
		for dm, configFmt := range dropAndMask {
			t.Run("buffer="+tc.buffer+"/drop+mask="+dm, func(t *testing.T) {
				config := fmt.Sprintf(configFmt, tc.buffer)
				eopa, eopaOut, eopaErr := loadEnterpriseOPA(t, config, policy, eopaHTTPPort, false)
				if err := eopa.Start(); err != nil {
					t.Fatal(err)
				}
				wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

				{ // act 1: request is logged as-is
					req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), nil)
					if err != nil {
						t.Fatalf("http request: %v", err)
					}
					resp, err := http.DefaultClient.Do(req)
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
					req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), payload)
					if err != nil {
						t.Fatalf("http request: %v", err)
					}
					resp, err := http.DefaultClient.Do(req)
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
					req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), payload)
					if err != nil {
						t.Fatalf("http request: %v", err)
					}
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					if exp, act := 200, resp.StatusCode; exp != act {
						t.Fatalf("expected status %d, got %d", exp, act)
					}
				}

				// assert: check that DL logs have been output as expected
				logs := collectDL(t, eopaOut, false, 2)
				sort.Slice(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })

				{ // log for act 1
					dl := payload{
						Result: true,
						ID:     1,
						Labels: standardLabels,
					}
					if diff := cmp.Diff(dl, logs[0], stdIgnores); diff != "" {
						t.Errorf("diff: (-want +got):\n%s", diff)
					}
					{
						exp := []string{
							"counter_regovm_eval_instructions",
							"counter_regovm_virtual_cache_hits",
							"counter_regovm_virtual_cache_misses",
							"counter_server_query_cache_hit",
							"timer_rego_input_parse_ns",
							// "timer_rego_module_parse_ns", // TODO(philip): Triage *why* this is causing E2E test failures.
							"timer_rego_query_compile_ns",
							// "timer_rego_query_parse_ns", // TODO(philip): This missing metric has also caused some E2E test failures.
							"timer_regovm_eval_ns",
							"timer_server_handler_ns",
						}
						act := maps.Keys(logs[0].Metrics)
						if diff := cmp.Diff(exp, act, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
							t.Errorf("unexpected log[0] metrics: (-want +got):\n%s", diff)
						}
						// Was: https://github.com/styrainc/enterprise-opa-private/issues/625
						if act := logs[0].Metrics["timer_server_handler_ns"]; act == 0 {
							t.Error("expected timer_server_handler_ns > 0")
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
					if diff := cmp.Diff(dl, logs[1], stdIgnores); diff != "" {
						t.Errorf("diff: (-want +got):\n%s", diff)
					}
				}
			})
		}
	}
}

// We're asserting two things here: that an object result is marshalled into
// a DL entry properly, and that we can mask parts of it.
func TestDecisionLogsComplexResult(t *testing.T) {
	policy := `
package test
import rego.v1

p := {"foo": "bar", "replace": "box", "remove": {"this": 42}} if input.a == "b"

mask contains {"op": "upsert", "path": "/result/replace", "value": "fox"}
mask contains {"op": "remove", "path": "/result/remove/this"}
`

	dropAndMask := map[string]string{
		"top-level": `
decision_logs:
  plugin: eopa_dl
  drop_decision: /test/drop
  mask_decision: /test/mask
plugins:
  eopa_dl:
    buffer:
      type: memory
    output:
      type: console
`,
		"per-output": `
decision_logs:
  plugin: eopa_dl
  drop_decision: /test/drop
  mask_decision: /test/mask
plugins:
  eopa_dl:
    buffer:
      type: memory
    output:
      type: console
`,
	}

	for dm, config := range dropAndMask {
		t.Run("drop+mask="+dm, func(t *testing.T) {

			eopa, eopaOut, eopaErr := loadEnterpriseOPA(t, config, policy, eopaHTTPPort, false)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			{ // act: send request
				req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/p", eopaHTTPPort), strings.NewReader(`{"input": {"a": "b"}}`))
				if err != nil {
					t.Fatalf("http request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if exp, act := 200, resp.StatusCode; exp != act {
					t.Fatalf("expected status %d, got %d", exp, act)
				}
			}

			logs := collectDL(t, eopaOut, false, 1)
			{ // log for act 1
				dl := payload{
					Result: map[string]any{
						"foo":     "bar",
						"remove":  map[string]any{},
						"replace": "fox",
					},
					Input:  map[string]any{"a": "b"},
					Erased: []string{"/result/remove/this"},
					Masked: []string{"/result/replace"},
					ID:     1,
					Labels: standardLabels,
				}
				if diff := cmp.Diff(dl, logs[0], stdIgnores); diff != "" {
					t.Errorf("diff: (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestDecisionLogsMemoryBatching(t *testing.T) {
	policy := `
package test
import rego.v1

# always succeeds, but adds nd_builtin_cache entry
coin if rand.intn("coin", 2)
`

	t.Run("flush_at_count", func(t *testing.T) {
		config := `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    buffer:
      type: memory
      flush_at_count: 2
    output:
      type: console
`
		eopa, eopaOut, eopaErr := loadEnterpriseOPA(t, config, policy, eopaHTTPPort, false)
		if err := eopa.Start(); err != nil {
			t.Fatal(err)
		}
		wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

		{ // act 1: first request, not logged yet
			resp, err := http.Post(fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), jsonType, nil)
			if err != nil {
				t.Fatalf("http request: %v", err)
			}
			defer resp.Body.Close()
			if exp, act := 200, resp.StatusCode; exp != act {
				t.Fatalf("expected status %d, got %d", exp, act)
			}
		}

		{ // assert 1: no decision logs
			time.Sleep(20 * time.Millisecond)
			exp, act := ``, eopaOut.String()
			if diff := cmp.Diff(exp, act); diff != "" {
				t.Fatalf("unexpected console logs: (-want, +got):\n%s", diff)
			}
		}

		{ // act 2: second request, triggers flush
			resp, err := http.Post(fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), jsonType, nil)
			if err != nil {
				t.Fatalf("http request: %v", err)
			}
			defer resp.Body.Close()
			if exp, act := 200, resp.StatusCode; exp != act {
				t.Fatalf("expected status %d, got %d", exp, act)
			}
		}

		_ = collectDL(t, eopaOut, false, 2) // if we pass this, we've got two logs
	})

	t.Run("flush_at_period", func(t *testing.T) {
		config := `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    buffer:
      type: memory
      flush_at_period: 3s
    output:
      type: console
`
		eopa, eopaOut, eopaErr := loadEnterpriseOPA(t, config, policy, eopaHTTPPort, false)
		if err := eopa.Start(); err != nil {
			t.Fatal(err)
		}
		wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

		{ // act 1: first request, not logged yet
			resp, err := http.Post(fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), jsonType, nil)
			if err != nil {
				t.Fatalf("http request: %v", err)
			}
			defer resp.Body.Close()
			if exp, act := 200, resp.StatusCode; exp != act {
				t.Fatalf("expected status %d, got %d", exp, act)
			}
		}

		{ // assert: no decision logs (yet)
			time.Sleep(100 * time.Millisecond)
			exp, act := ``, eopaOut.String()
			if diff := cmp.Diff(exp, act); diff != "" {
				t.Fatalf("unexpected console logs: (-want, +got):\n%s", diff)
			}
		}

		time.Sleep(3 * time.Second)
		_ = collectDL(t, eopaOut, false, 1) // if we pass this, we've got one log
	})
}

const jsonType = "application/json"

func TestDecisionLogsServiceOutput(t *testing.T) {
	policy := `
package test
import rego.v1

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
  credentials:
    bearer:
      token: opensesame
      scheme: Secret
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
      type: service
      service: dl-sink
`,
		},
		{
			note: "type=http",
			configFmt: `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
      type: http
      url: "%s/prefix/logs"
      headers:
        Authorization: Secret opensesame
        Content-Type: application/json
`,
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			buf := bytes.Buffer{}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				d, _ := httputil.DumpRequest(r, !tc.compressed)
				t.Log(string(d))
				switch {
				case r.URL.Path != "/prefix/logs":
				case r.Method != http.MethodPost:
				case r.Header.Get("Authorization") != "Secret opensesame":
				case r.Header.Get("Content-Type") != "application/json":
				case !expectedUA(r.Header.Get("User-Agent")):
				case tc.compressed && r.Header.Get("Content-Encoding") != "gzip":
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
			eopa, _, eopaErr := loadEnterpriseOPA(t, fmt.Sprintf(tc.configFmt, ts.URL), policy, eopaHTTPPort, false)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			{ // act: send API request
				req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), nil)
				if err != nil {
					t.Fatalf("http request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if exp, act := 200, resp.StatusCode; exp != act {
					t.Fatalf("expected status %d, got %d", exp, act)
				}
			}

			logs := collectDL(t, &buf, tc.compressed, 1)
			dl := payload{
				Result: true,
				ID:     1,
				Labels: standardLabels,
			}
			if diff := cmp.Diff(dl, logs[0], stdIgnores); diff != "" {
				t.Errorf("diff: (-want +got):\n%s", diff)
			}
		})
	}
}

var ua = regexp.MustCompile(`^Enterprise OPA/([0-9]+\.[0-9]+\.[0-9]+) Open Policy Agent/[0-9.]+ \([a-z]+, [a-z0-9-_]+\)$`)

func expectedUA(u string) bool {
	return ua.MatchString(u)
}

func TestDecisionLogsHttpRetry(t *testing.T) {
	expectedRetries := 5
	policy := `
package test
import rego.v1

coin if rand.intn("coin", 2)`

	config := `
# enterprise-opa.yml
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: http
      url: %s/prefix/logs
      retry:
        period: 1ms
        max_backoff: 1ms
        max_attempts: %d
        backoff_on: [429,410,418]
        drop_on: [500,508]`
	retryCodes := []int{http.StatusTooManyRequests, http.StatusGone, http.StatusTeapot}
	buf := bytes.Buffer{}
	retries := 0
	dropped := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path != "/prefix/logs":
		case r.Method != http.MethodPost:
		case !dropped:
			dropped = true
			w.WriteHeader(http.StatusLoopDetected)
			return
		case retries < 5:
			retries++
			code := retryCodes[rand.Intn(3)]
			w.WriteHeader(code)
			return
		default:
			io.Copy(&buf, r.Body)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)
	eopa, _, eopaErr := loadEnterpriseOPA(t, fmt.Sprintf(config, ts.URL, expectedRetries), policy, eopaHTTPPort, false)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool {
		return strings.Contains(s, "Server initialized")
	}, 5*time.Second)

	act := func() {
		req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		_, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
	}

	// dropped result
	wait.ForResult(t, act, func() error {
		if dropped {
			return nil
		}
		return errors.New("expected the first request to have been dropped but no drop detected")
	}, 5, time.Second)

	// retried result
	wait.ForResult(t, act, func() error {
		if retries == expectedRetries {
			return nil
		}
		return fmt.Errorf("expected %d retries, but got %d", expectedRetries, retries)
	}, 5, time.Second)

	// validate the log did go through after the 5 retries
	logsHTTP := collectDL(t, &buf, false, 1)
	dl := payload{
		Result: true,
		ID:     2,
		Labels: standardLabels,
	}
	if diff := cmp.Diff(dl, logsHTTP[0], stdIgnores); diff != "" {
		t.Errorf("diff: (-want +got):\n%s", diff)
	}
}

// In this test, we also check that intermediate_results make it through the eopa_dl plugin
func TestDecisionLogsServiceAndConsoleOutput(t *testing.T) {
	policy := `
package test
import rego.v1

coin if rand.intn("coin", 2)
`

	configFmt := `
services:
- name: "dl-sink"
  url: "%s/prefix"
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
    - type: service
      service: dl-sink
    - type: console
`
	var buf bytes.Buffer
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path != "/prefix/logs":
		case r.Method != http.MethodPost:
		default: // all matches
			src, _ := gzip.NewReader(r.Body)
			io.Copy(&buf, src)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)
	eopa, eopaOut, eopaErr := loadEnterpriseOPA(t, fmt.Sprintf(configFmt, ts.URL), policy, eopaHTTPPort, false)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	{ // act: send API request
		req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := 200, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
	}

	logsHTTP := collectDL(t, &buf, true, 1)
	logsConsole := collectDL(t, eopaOut, false, 1)
	dl := payload{
		Result:       true,
		ID:           1,
		Labels:       standardLabels,
		Intermediate: map[string]any{"test.coin": []any{true}},
	}

	// NB(sr): we're not ignoring "Intermediate" here
	ignores := cmpopts.IgnoreFields(payload{}, "Timestamp", "Metrics", "DecisionID", "Labels.ID", "Labels.Version", "NDBC")
	if diff := cmp.Diff(dl, logsHTTP[0], ignores); diff != "" {
		t.Errorf("diff: (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(logsHTTP, logsConsole); diff != "" {
		t.Errorf("HTTP vs console sink diff: (-want +got):\n%s", diff)
	}
}

func TestDecisionLogsServiceOutputWithOAuth2(t *testing.T) {
	policy := `
package test
import rego.v1

coin if rand.intn("coin", 2)
`
	configFmt := `
services:
- name: "dl-sink"
  url: "%[1]s/prefix"
  credentials:
    oauth2:
      client_id: mememe
      client_secret: sesamememe
      token_url: "%[1]s/token"
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
      type: service
      service: dl-sink
`
	buf := bytes.Buffer{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Header().Add("content-type", jsonType)
			if err := json.NewEncoder(w).Encode(map[string]any{"access_token": "sometoken"}); err != nil {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		case r.URL.Path != "/prefix/logs":
		case r.Method != http.MethodPost:
		default: // all matches
			src, _ := gzip.NewReader(r.Body)
			io.Copy(&buf, src)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)
	eopa, _, eopaErr := loadEnterpriseOPA(t, fmt.Sprintf(configFmt, ts.URL), policy, eopaHTTPPort, false)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	{ // act: send API request
		req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := 200, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
	}

	_ = collectDL(t, &buf, true, 1)
}

func TestDecisionLogsServiceOutputWithTLS(t *testing.T) {
	policy := `
package test
import rego.v1

coin if rand.intn("coin", 2)
`
	configFmt := `
services:
- name: "dl-sink"
  url: "%[1]s/prefix"
  tls:
    ca_cert: testdata/tls/ca.pem
  credentials:
    client_tls:
      cert: testdata/tls/client-cert.pem
      private_key: testdata/tls/client-key.pem
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
      type: service
      service: dl-sink
`
	buf := bytes.Buffer{}
	caCert, err := os.ReadFile("testdata/tls/ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	mux := http.NewServeMux()
	mux.HandleFunc("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path != "/prefix/logs":
		case r.Method != http.MethodPost:
		default: // all matches
			src, _ := gzip.NewReader(r.Body)
			io.Copy(&buf, src)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	srv := &http.Server{
		Addr:    "127.0.0.1:9999",
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientCAs:  caCertPool,
			ClientAuth: tls.RequireAndVerifyClientCert,
		},
	}
	go srv.ListenAndServeTLS("testdata/tls/server-cert.pem", "testdata/tls/server-key.pem")
	t.Cleanup(func() { _ = srv.Close() })

	config := fmt.Sprintf(configFmt, "https://127.0.0.1:9999")
	eopa, _, eopaErr := loadEnterpriseOPA(t, config, policy, eopaHTTPPort, false)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	{ // act: send API request
		req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort), nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := 200, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
	}

	_ = collectDL(t, &buf, true, 1)
}

type gcpOverrideEndpoint string

func loadEnterpriseOPA(t *testing.T, config, policy string, httpPort int, opts ...any) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	var silent bool
	logLevel := "debug" // Needed for checking if server is ready
	for _, o := range opts {
		switch o := o.(type) {
		case bool:
			silent = o
		case gcpOverrideEndpoint:
			t.Setenv("STORAGE_EMULATOR_HOST", string(o))
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
		t.Fatalf("write policy: %v", err)
	}

	args := []string{
		"run",
		"--server",
		"--addr", fmt.Sprintf("localhost:%d", httpPort),
		"--config-file", confPath,
		"--log-level", logLevel,
		"--disable-telemetry",
	}
	eopa := exec.Command(binary(), append(args, policyPath)...)
	eopa.Stderr = &stderr
	eopa.Stdout = &stdout
	eopa.Env = append(eopa.Environ(),
		"EOPA_LICENSE_TOKEN="+os.Getenv("EOPA_LICENSE_TOKEN"),
		"EOPA_LICENSE_KEY="+os.Getenv("EOPA_LICENSE_KEY"),
		"OPA_DECISIONS_INTERMEDIATE_RESULTS=VALUE",
	)

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		if err := eopa.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		eopa.Wait()
		if testing.Verbose() && t.Failed() && !silent {
			t.Logf("eopa stdout:\n%s", stdout.String())
			t.Logf("eopa stderr:\n%s", stderr.String())
		}
	})

	return eopa, &stdout, &stderr
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "eopa"
	}
	return bin
}

// collectDL either returns `exp` decision log payloads, or calls t.Fatal
func collectDL(t *testing.T, rdr io.Reader, array bool, exp int) []payload {
	t.Helper()
	for i := 0; i <= 3; i++ {
		time.Sleep(100 * time.Millisecond)
		var ms []payload
		if array {
			if err := json.NewDecoder(rdr).Decode(&ms); err != nil {
				if err != io.EOF {
					t.Fatalf("decode recorded DL: %v", err)
				}
			}
		} else {
			ms = retrieveDLs(t, rdr)
		}
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
