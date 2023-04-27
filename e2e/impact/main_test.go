//go:build e2e

// package impact is for testing Load as container, running as server,
// with LIA enabled, and sending decision logs to a decision log service
package impact

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/styrainc/load-private/e2e/wait"
)

type payload struct {
	Result     any            `json:"result"`
	Metrics    map[string]int `json:"metrics"`
	ID         int            `json:"req_id"`
	DecisionID string         `json:"decision_id"`
}

type liaResponse struct {
	Result        any    `json:"value_b"`
	PrimaryResult any    `json:"value_a"`
	Input         any    `json:"input"`
	Path          string `json:"path"`
	EvalA         int    `json:"eval_ns_a"`
	EvalB         int    `json:"eval_ns_b"`
	ReqID         int    `json:"req_id"`
	DecisionID    string `json:"decision_id"`
	NodeID        string `json:"node_id"`
}

// NOTE(sr): These three tests check the following:
//  1. Use decision logs to assert the actual number of evals run; including
//     primary and secondary (LIA) evaluations.
//  2. Check the returned responses for the LIA POST call that controls the
//     LIA run.

func TestDecisionLogsAllEqual(t *testing.T) {
	ctx := context.Background()

	config := `
decision_logs:
  console: true
plugins:
  impact_analysis:
    decision_logs: true
`
	policy := `
package test
import future.keywords

p := rand.intn("test", 2)
`
	load, loadOut := loadLoad(t, config, policy, false)
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	// arrange: enable LIA via CLI
	ctl, ctlOut := loadCtl(t, "http://127.0.0.1:18181", "testdata/load-bundle.tar.gz", "--duration 10s --sample-rate 1 --equals")
	ctl.Stderr = os.Stderr
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}

	waitForLIAStart(ctx, t, loadOut)

	{ // act: evaluate the policy via the v1 data API
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		req, err := http.NewRequest("GET", "http://localhost:18181/v1/data/test/p", nil)
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
	logs := collectDL(ctx, t, loadOut, 2) // if we don't bail in this method, we've got two logs
	if diff := cmp.Diff(logs[0], logs[1], cmpopts.IgnoreFields(payload{}, "Metrics")); diff != "" {
		t.Errorf("diff: (-want +got):\n%s", diff)
	}
	// everything but the metrics are the same. Here, we expect the LIA log to
	// 1. have a different set of metrics
	// 2. have a higher timer_regovm_eval_ns, because of the test policy calling numbers.range()
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
		exp := []string{"counter_regovm_eval_instructions",
			"timer_rego_module_compile_ns",
			"timer_rego_query_compile_ns",
			"timer_regovm_eval_ns",
		}
		act := maps.Keys(logs[1].Metrics)
		if diff := cmp.Diff(exp, act, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
			t.Errorf("unexpected log[1] metrics: (-want +got):\n%s", diff)
		}
	}

	evalA, evalB := logs[0].Metrics["timer_regovm_eval_ns"], logs[1].Metrics["timer_regovm_eval_ns"]
	if evalA >= evalB {
		t.Errorf("expected secondary eval to take longer, got a: %d, b: %d (ns)", evalA, evalB)
	}

	waitForLIAEnd(ctx, t, loadOut, 10*time.Second)
	ctl.Wait()
	if testing.Verbose() && t.Failed() {
		t.Logf("impact output:\n%s", ctlOut.String())
	}

	act := []liaResponse{}
	if err := json.NewDecoder(ctlOut).Decode(&act); err != nil {
		t.Error(err)
	}
	if exp, act := 1, len(act); exp != act {
		t.Fatalf("expected %d diffs, got %d", exp, act)
	}
	{
		act := act[0]
		exp := liaResponse{
			ReqID: 2, // ReqID 2 is the LIA req above
			Path:  "test/p",
			Input: nil,
		}
		ignores := cmpopts.IgnoreFields(liaResponse{},
			"EvalA",
			"EvalB",
			"NodeID",
			"DecisionID",
			"Result",
			"PrimaryResult",
		)
		if diff := cmp.Diff(exp, act, ignores); diff != "" {
			t.Errorf("diff: (-want +got):\n%s", diff)
		}
		if !slices.Contains([]any{float64(0), float64(1)}, act.Result) {
			t.Errorf("expected result B to be 0 or 1, got %v", act.Result)
		}
		if !slices.Contains([]any{float64(0), float64(1)}, act.PrimaryResult) {
			t.Errorf("expected result A to be 0 or 1, got %v", act.PrimaryResult)
		}
		if act.EvalB < act.EvalA {
			t.Errorf("eval timers: expected B: %dns > A: %dns", act.EvalB, act.EvalA)
		}
	}
}

func TestDecisionLogsSomeDiffs(t *testing.T) {

	ctx := context.Background()

	config := `
decision_logs:
  console: true
plugins:
  impact_analysis:
    decision_logs: true
`
	policy := `
package test
import future.keywords

q := true
`
	load, loadOut := loadLoad(t, config, policy, false)
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	// arrange: enable LIA via CLI
	ctl, ctlOut := loadCtl(t, "http://127.0.0.1:18181", "testdata/load-bundle.tar.gz", "--duration 10s --sample-rate 1")
	ctl.Stderr = os.Stderr
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}

	waitForLIAStart(ctx, t, loadOut)

	{ // act: evaluate the policy via the v1 data API, provide input
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		in := `{"input": {"a": true}}`
		req, err := http.NewRequest("POST", "http://localhost:18181/v1/data/test/q", strings.NewReader(in))
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

	{ // act 2: evaluate the policy via the v1 data API, provide empty input
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		in := `{"input": {}}`
		req, err := http.NewRequest("POST", "http://localhost:18181/v1/data/test/q", strings.NewReader(in))
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

	{ // assert: check that DL logs have been output as expected
		logs := collectDL(ctx, t, loadOut, 3)
		if diff := cmp.Diff(logs[1], logs[2], cmpopts.IgnoreFields(payload{}, "Metrics", "Result")); diff != "" {
			t.Errorf("diff: (-want +got):\n%s", diff)
		}

		if exp, act := true, logs[1].Result.(bool); exp != act {
			t.Errorf("expected primary result to be %v, got %v", exp, act)
		}
		if exp, act := any(nil), logs[2].Result; exp != act {
			t.Errorf("expected primary result to be %v, got %v", exp, act)
		}
	}

	waitForLIAEnd(ctx, t, loadOut, 10*time.Second)
	ctl.Wait()
	if testing.Verbose() && t.Failed() {
		t.Logf("impact output:\n%s", ctlOut.String())
	}

	act := []liaResponse{}
	if err := json.NewDecoder(ctlOut).Decode(&act); err != nil {
		t.Error(err)
	}
	if exp, act := 1, len(act); exp != act {
		t.Fatalf("expected %d diffs, got %d", exp, act)
	}

	{
		act := act[0]
		exp := liaResponse{
			ReqID:         3, // 1 is the LIA req above; 2 is the request with equal results
			Path:          "test/q",
			Input:         map[string]any{},
			PrimaryResult: true,
		}
		ignores := cmpopts.IgnoreFields(liaResponse{},
			"EvalA",
			"EvalB",
			"NodeID",
			"DecisionID",
		)
		if diff := cmp.Diff(exp, act, ignores); diff != "" {
			t.Errorf("diff: (-want +got):\n%s", diff)
		}
	}
}

func TestDecisionLogsAllDiffsSampling(t *testing.T) {
	const count = 1000
	ctx := context.Background()

	config := `
decision_logs:
  console: true
plugins:
  impact_analysis:
    decision_logs: true
`
	policy := `
package test
import future.keywords

q := true
`
	load, loadOut := loadLoad(t, config, policy, true)
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	// arrange: enable LIA via CLI
	ctl, ctlOut := loadCtl(t, "http://127.0.0.1:18181", "testdata/load-bundle.tar.gz", "--duration 10s --sample-rate 0.1")
	ctl.Stderr = os.Stderr
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}

	waitForLIAStart(ctx, t, loadOut)

	for i := 0; i < count; i++ { // act: evaluate the policy via the v1 data API, provide empty input, many times
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		in := `{"input": {}}`
		req, err := http.NewRequest("POST", "http://localhost:18181/v1/data/test/q", strings.NewReader(in))
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

	// assert: get all DLs emitted so far, check if their number looks OK
	logs := retrieveDLs(ctx, t, loadOut)
	act := len(logs) - count
	if act > count*0.2 || act <= 0 {
		t.Errorf("expected sample count to be +/- ~10%% of %d, got %d", count, act)
	}

	waitForLIAEnd(ctx, t, loadOut, 10*time.Second)
	ctl.Wait()
	if testing.Verbose() && t.Failed() {
		t.Logf("impact output:\n%s", ctlOut.String())
	}

	// count diffs returned to client
	{
		act := []struct{}{}
		if err := json.NewDecoder(ctlOut).Decode(&act); err != nil {
			t.Fatal(err)
		}
		if n := len(act); n > count*0.2 || n <= 0 {
			t.Errorf("expected sample count to be +/- ~10%% of %d, got %d", count, n)
		}
	}
}

func TestTUIOutputs(t *testing.T) {
	ctx := context.Background()
	const count = 20

	config := `
plugins:
  impact_analysis: {}
`
	policy := `
package test
import future.keywords

p := rand.intn("test", 2)
`

	us := regexp.MustCompile(`[1-9][0-9]*\.[0-9]+µs`)
	ms := regexp.MustCompile(`[1-9][0-9]*\.[0-9]+ms`)
	for _, tc := range []struct {
		format string
		group  bool
		assert func(*testing.T, *bytes.Buffer)
	}{
		{
			format: "pretty",
			assert: func(t *testing.T, buf *bytes.Buffer) {
				if !strings.Contains(buf.String(), "eval_duration_a") {
					t.Errorf("expected eval_ns_a to be rewritten to eval_duration_a")
				}
				if !strings.Contains(buf.String(), "eval_duration_b") {
					t.Errorf("expected eval_ns_b to be rewritten to eval_duration_b")
				}
				// NOTE(sr): We don't know much about the output but the primary eval should be
				// microseconds, the secondary milliseconds.
				if us.Find(buf.Bytes()) == nil {
					t.Errorf("expected some microsecond duration, found none")
				}
				if ms.Find(buf.Bytes()) == nil {
					t.Errorf("expected some millisecond duration, found none")
				}

				if exp, act := count+4, bytes.Count(buf.Bytes(), []byte("\n")); exp != act {
					t.Errorf("expected %d lines, found %d", exp, act)
				}
			},
		},
		{
			format: "pretty",
			group:  true,
			assert: func(t *testing.T, buf *bytes.Buffer) {
				for _, s := range []string{"mean", "median", "min", "max", "stddev", "var"} {
					for _, k := range []string{"primary", "secondary"} {
						prev := s + "_" + k + "_ns"
						rewr := s + "_" + k + "_duration"
						if !strings.Contains(buf.String(), rewr) {
							t.Errorf("expected %s to be rewritten to %s", prev, rewr)
						}
					}
				}
				// NOTE(sr): We don't know much about the output but the primary eval should be
				// microseconds, the secondary milliseconds.
				if us.Find(buf.Bytes()) == nil {
					t.Errorf("expected some microsecond duration, found none")
				}
				if ms.Find(buf.Bytes()) == nil {
					t.Errorf("expected some millisecond duration, found none")
				}

				// The output collapses into one group, so we know the line count of the table
				if exp, act := 5, bytes.Count(buf.Bytes(), []byte("\n")); exp != act {
					t.Errorf("expected %d lines, found %d", exp, act)
				}
			},
		},
		{
			format: "json",
			assert: func(t *testing.T, buf *bytes.Buffer) {
				var out []liaResponse
				if err := json.NewDecoder(buf).Decode(&out); err != nil {
					t.Fatal(err)
				}
				if exp, act := count, len(out); exp != act {
					t.Errorf("expected %d results, got %d", exp, act)
				}
				for _, o := range out {
					if o.PrimaryResult != o.Result {
						t.Errorf("expected equal results, got primary=%v, secondary=%v", o.PrimaryResult, o.Result)
					}
					assertNonZero(t, o.Input)
					assertNonZero(t, o.Path)
					assertNonZero(t, o.EvalA)
					assertNonZero(t, o.EvalB)
					assertNonZero(t, o.ReqID)
					assertNonZero(t, o.NodeID)
					// NB: DecisionID is empty because the server doesn't have DL enabled
				}
			},
		},
		{
			format: "json",
			group:  true,
			assert: func(t *testing.T, buf *bytes.Buffer) {
				var out []map[string]any
				if err := json.NewDecoder(buf).Decode(&out); err != nil {
					t.Fatal(err)
				}
				if exp, act := 1, len(out); exp != act {
					t.Errorf("expected %d results, got %d", exp, act)
				}
			},
		},
		{
			format: "csv",
			assert: func(t *testing.T, buf *bytes.Buffer) {
				if exp, act := count+1, bytes.Count(buf.Bytes(), []byte("\n")); exp != act {
					t.Errorf("expected %d lines, got %d", exp, act)
				}
				exp := `node_id,req_id,value_a,value_b,input,path,eval_ns_a,eval_ns_b`
				i := strings.Index(buf.String(), "\n")
				act := buf.String()[:i]
				if exp != act {
					t.Errorf("header line, got %q, expected %q", act, exp)
				}
			},
		},
		{
			format: "csv",
			group:  true,
			assert: func(t *testing.T, buf *bytes.Buffer) {
				if exp, act := 2, bytes.Count(buf.Bytes(), []byte("\n")); exp != act {
					t.Errorf("expected %d lines, got %d", exp, act)
				}
				exp := `path,input,n,mean_primary_ns,median_primary_ns,min_primary_ns,max_primary_ns,stddev_primary_ns,var_primary_ns,mean_secondary_ns,median_secondary_ns,min_secondary_ns,max_secondary_ns,stddev_secondary_ns,var_secondary_ns`
				i := strings.Index(buf.String(), "\n")
				act := buf.String()[:i]
				if exp != act {
					t.Errorf("header line, got %q, expected %q", act, exp)
				}
			},
		},
	} {
		t.Run(fmt.Sprintf("format=%s/group=%v", tc.format, tc.group), func(t *testing.T) {
			load, loadOut := loadLoad(t, config, policy, false)
			if err := load.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, loadOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			// arrange: enable LIA via CLI
			extraArgs := "--duration 2s --sample-rate 1 --equals --format " + tc.format
			if tc.group {
				extraArgs += " --group"
			}
			ctl, ctlOut := loadCtl(t, "http://127.0.0.1:18181", "testdata/load-bundle.tar.gz", extraArgs)
			ctl.Stderr = os.Stderr
			if err := ctl.Start(); err != nil {
				t.Fatal(err)
			}

			waitForLIAStart(ctx, t, loadOut)

			for i := 0; i < count; i++ { // act: evaluate the policy via the v1 data API, provide empty input, many times
				in := `{"input": {}}`
				req, err := http.NewRequest("POST", "http://localhost:18181/v1/data/test/p", strings.NewReader(in))
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

			waitForLIAEnd(ctx, t, loadOut, 2*time.Second)

			tc.assert(t, ctlOut)
			if t.Failed() && testing.Verbose() {
				t.Logf("CLI output: %s", ctlOut.String())
			}
		})
	}
}

type extra string

func loadLoad(t *testing.T, config, policy string, opts ...any) (*exec.Cmd, *bytes.Buffer) {
	var silent bool
	var extraArgs string
	logLevel := "debug"
	for _, o := range opts {
		switch o := o.(type) {
		case bool:
			silent = o
		case errorLogging:
			logLevel = "error"
		case extra:
			extraArgs = string(o)
		}
	}

	buf := bytes.Buffer{}
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
		"--addr", "localhost:18181",
		"--config-file", confPath,
		"--log-level", logLevel,
		"--disable-telemetry",
	}
	if extraArgs != "" {
		args = append(args, strings.Split(extraArgs, " ")...)
	}
	load := exec.Command(binary(), append(args, policyPath)...)
	load.Stderr = &buf
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
		if testing.Verbose() && t.Failed() && !silent {
			t.Logf("load output:\n%s", buf.String())
		}
	})

	return load, &buf
}

func loadCtl(t *testing.T, addr string, path, extra string) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.Command(binary(), strings.Split("impact record --format json --addr "+addr+" --bundle "+path+" "+extra, " ")...)
	cmd.Stdout = &bytes.Buffer{}
	return cmd, cmd.Stdout.(*bytes.Buffer)
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "load"
	}
	return bin
}

func TestStopWhenCallerGoesAway(t *testing.T) {
	ctx := context.Background()

	config := `
decision_logs:
  console: true
plugins:
  impact_analysis:
    decision_logs: true
`
	policy := `
package test
import future.keywords

q := true
`
	load, loadOut := loadLoad(t, config, policy, false)
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	ctl, ctlOut := loadCtl(t, "http://127.0.0.1:18181", "testdata/load-bundle.tar.gz", "--duration 10s --sample-rate 1 --equals")
	ctl.Stderr = os.Stderr
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}

	waitForLIAStart(ctx, t, loadOut)

	// abort CLI call
	if err := ctl.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}

	waitForLIAEnd(ctx, t, loadOut, 10*time.Second)
	if testing.Verbose() && t.Failed() {
		t.Logf("impact output:\n%s", ctlOut.String())
	}
}

func TestTLSCommunication(t *testing.T) {
	ctx := context.Background()

	config := `
decision_logs:
  console: true
plugins:
  impact_analysis:
    decision_logs: true
`
	policy := `
package test
import future.keywords

q := true
`
	load, loadOut := loadLoad(t, config, policy, false, extra(`--tls-ca-cert-file testdata/tls/ca.pem --tls-cert-file testdata/tls/server-cert.pem --tls-private-key-file testdata/tls/server-key.pem`))
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	ctl, ctlOut := loadCtl(t, "https://127.0.0.1:18181", "testdata/load-bundle.tar.gz", "--duration 10s --sample-rate 1 --equals "+
		"--tls-ca-cert-file testdata/tls/ca.pem --tls-cert-file testdata/tls/client-cert.pem --tls-private-key-file testdata/tls/client-key.pem")
	ctl.Stderr = os.Stderr
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}

	waitForLIAStart(ctx, t, loadOut)

	// abort CLI call
	if err := ctl.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}

	waitForLIAEnd(ctx, t, loadOut, 10*time.Second)
	if testing.Verbose() && t.Failed() {
		t.Logf("impact output:\n%s", ctlOut.String())
	}
}

type errorLogging struct{}

func TestStillWorksWithoutDecisionLogsAndErrorLogging(t *testing.T) {
	const count = 100
	ctx := context.Background()
	config := `
plugins:
  impact_analysis: {}
`
	policy := `
package test
import future.keywords

q := true
`
	load, _ := loadLoad(t, config, policy, true, errorLogging{})
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	// NOTE(sr): In this test, we have no side-channel to know if LIA is enabled, or stopped.
	// So we'll be gracious with waiting times, and very loose in our assertions.
	time.Sleep(time.Second)

	ctl, ctlOut := loadCtl(t, "http://127.0.0.1:18181", "testdata/load-bundle.tar.gz", "--duration 10s --sample-rate 1 --equals")
	ctl.Stderr = os.Stderr
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}

	// LIA runs for 10 seconds, so we'll give it a bit of time to start, and wait here
	// before starting the API requests.
	time.Sleep(time.Second)

	for i := 0; i < count; i++ { // act: evaluate the policy via the v1 data API, provide empty input, many times
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		in := `{"input": {}}`
		req, err := http.NewRequest("POST", "http://localhost:18181/v1/data/test/q", strings.NewReader(in))
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

	if err := ctl.Wait(); err != nil {
		t.Fatal(err)
	}
	if testing.Verbose() && t.Failed() {
		t.Logf("impact output:\n%s", ctlOut.String())
	}

	act := []liaResponse{}
	if err := json.NewDecoder(ctlOut).Decode(&act); err != nil {
		t.Error(err)
	}
	if exp, act := 1, len(act); exp > act {
		t.Fatalf("expected >=%d diffs, got %d", exp, act)
	}
}

// collectDL either returns `exp` decision log payloads, or calls t.Fatal
func collectDL(ctx context.Context, t *testing.T, rdr io.Reader, exp int) []payload {
	t.Helper()
	for i := 0; i <= 3; i++ {
		if i != 0 {
			time.Sleep(100 * time.Millisecond)
		}
		ms := retrieveDLs(ctx, t, rdr)
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
		if m.DecisionID != "" {
			ms = append(ms, m)
		}
	}
	return ms
}

func waitForLIAStart(ctx context.Context, t *testing.T, b *bytes.Buffer) {
	t.Helper()
	wait.ForLog(t, b, func(s string) bool { return strings.HasPrefix(s, "started live impact analysis") }, time.Second)
}

func waitForLIAEnd(ctx context.Context, t *testing.T, b *bytes.Buffer, dur time.Duration) {
	t.Helper()
	wait.ForLog(t, b, func(s string) bool { return strings.HasPrefix(s, "stopped live impact analysis") }, dur)
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

func assertNonZero(t *testing.T, a any) {
	t.Helper()
	if reflect.ValueOf(a).IsZero() {
		t.Errorf("expected %v to be non-zero", a)
	}
}
