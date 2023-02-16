//go:build e2e

// package impact is for testing Load as container, running as server,
// with LIA enabled, and sending decision logs to a decision log service
package impact

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
	"golang.org/x/exp/maps"
)

const defaultImage = "ko.local:edge" // built via `make build-local`

var dockerPool = func() *dockertest.Pool {
	p, err := dockertest.NewPool("")
	if err != nil {
		panic(err)
	}

	if err = p.Client.Ping(); err != nil {
		panic(err)
	}
	return p
}()

type payload struct {
	Result     any            `json:"result"`
	Metrics    map[string]int `json:"metrics"`
	ID         int            `json:"req_id"`
	DecisionID string         `json:"decision_id"`
}

func TestDecisionLogsAllEqual(t *testing.T) {

	cleanupPrevious(t)
	ctx := context.Background()

	config := `
decision_logs:
  console: true
plugins:
  impact_analysis:
    sampling_rate: 1
    bundle_path: /load-bundle.tar.gz
    publish_equal: true
`
	policy := `
package test
import future.keywords

p := rand.intn("test", 2)
`
	load := loadLoad(t, config, policy)

	{ // act: evaluate the policy via the v1 data API
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		req, err := http.NewRequest("GET", "http://localhost:"+load.GetPort("8181/tcp")+"/v1/data/test/p", nil)
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
	logs := collectDL(ctx, t, load.Container.ID, 2) // if we don't bail in this method, we've got two logs
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
			"timer_rego_load_bundles_ns",
			"timer_rego_module_parse_ns",
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
}

func TestDecisionLogsSomeDiffs(t *testing.T) {

	cleanupPrevious(t)
	ctx := context.Background()

	config := `
decision_logs:
  console: true
plugins:
  impact_analysis:
    sampling_rate: 1
    bundle_path: /load-bundle.tar.gz
    publish_equal: false
`
	policy := `
package test
import future.keywords

q := true
`
	load := loadLoad(t, config, policy)

	{ // act: evaluate the policy via the v1 data API, provide input
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		in := `{"input": {"a": true}}`
		req, err := http.NewRequest("POST", "http://localhost:"+load.GetPort("8181/tcp")+"/v1/data/test/q", strings.NewReader(in))
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
		req, err := http.NewRequest("POST", "http://localhost:"+load.GetPort("8181/tcp")+"/v1/data/test/q", strings.NewReader(in))
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
	logs := collectDL(ctx, t, load.Container.ID, 3)
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

func TestDecisionLogsAllDiffsSampling(t *testing.T) {
	const count = 1000
	cleanupPrevious(t)
	ctx := context.Background()

	config := `
decision_logs:
  console: true
plugins:
  impact_analysis:
    sampling_rate: 0.1
    bundle_path: /load-bundle.tar.gz
    publish_equal: false
`
	policy := `
package test
import future.keywords

q := true
`
	load := loadLoad(t, config, policy)
	for i := 0; i < count; i++ { // act: evaluate the policy via the v1 data API, provide empty input, many times
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		in := `{"input": {}}`
		req, err := http.NewRequest("POST", "http://localhost:"+load.GetPort("8181/tcp")+"/v1/data/test/q", strings.NewReader(in))
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
	logs := retrieveDLs(ctx, t, load.Container.ID)
	act := len(logs) - count
	if act > count*0.2 || act <= 0 {
		t.Errorf("expected sample count to be ~10%% above %d, got %d", count, act)
	}
}

func loadLoad(t *testing.T, config, policy string) *dockertest.Resource {
	image := os.Getenv("IMAGE")
	if image == "" {
		image = defaultImage
	}

	img := strings.Split(image, ":")

	dir := t.TempDir()
	confPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(confPath, []byte(config), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}
	policyPath := filepath.Join(dir, "eval.rego")
	if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}
	bundlePath := filepath.Join(dir, "load-bundle.tar.gz")
	buf, err := os.ReadFile("testdata/load-bundle.tar.gz")
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if err := os.WriteFile(bundlePath, buf, 0x777); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	load, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "load-e2e",
		Repository: img[0],
		Tag:        img[1],
		Hostname:   "load-e2e",
		Env: []string{
			"STYRA_LOAD_LICENSE_TOKEN=" + os.Getenv("STYRA_LOAD_LICENSE_TOKEN"),
			"STYRA_LOAD_LICENSE_KEY=" + os.Getenv("STYRA_LOAD_LICENSE_KEY"),
		},
		Mounts: []string{
			confPath + ":/config.yml",
			policyPath + ":/eval.rego",
			bundlePath + ":/load-bundle.tar.gz",
		},
		ExposedPorts: []string{"8181/tcp"},
		Cmd:          strings.Split("run --server --addr :8181 --config-file /config.yml --log-level debug --disable-telemetry /eval.rego", " "),
	}, func(config *docker.HostConfig) {
		// config.AutoRemove = true
	})
	if err != nil {
		t.Fatalf("could not start %s: %s", image, err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(load); err != nil {
			t.Fatalf("could not purge load: %s", err)
		}
	})

	if err := dockerPool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		req, err := http.NewRequest("GET", "http://localhost:"+load.GetPort("8181/tcp")+"", nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			t.Logf("GET / err: %v (retrying)", err)
			return err
		}
		defer resp.Body.Close()
		return nil
	}); err != nil {
		t.Fatalf("could not connect to load: %s", err)
	}

	return load
}

// collectDL either returns `exp` decision log payloads, or calls t.Fatal
func collectDL(ctx context.Context, t *testing.T, container string, exp int) []payload {
	for i := 0; i <= 3; i++ {
		if i != 0 {
			time.Sleep(100 * time.Millisecond)
		}
		ms := retrieveDLs(ctx, t, container)
		if act := len(ms); act == exp {
			return ms
		} else if i == 3 {
			t.Fatalf("expected %d payload, got %d", exp, act)
		}
	}
	return nil
}

func retrieveDLs(ctx context.Context, t *testing.T, container string) []payload {
	buf := bytes.Buffer{}
	opts := docker.LogsOptions{
		Context:      ctx,
		Stderr:       true,
		Stdout:       true,
		RawTerminal:  true,
		Container:    container,
		OutputStream: &buf,
	}
	if err := dockerPool.Client.Logs(opts); err != nil {
		t.Fatalf("tail logs: %v", err)
	}
	var stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	if _, err := stdcopy.StdCopy(&stdout, &stderr, &buf); err != nil {
		t.Fatalf("demux logs: %v", err)
	}

	ms := []payload{}
	dec := json.NewDecoder(&stderr)
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

func cleanupPrevious(t *testing.T) {
	t.Helper()
	for _, n := range []string{"load-e2e"} {
		if err := dockerPool.RemoveContainerByName(n); err != nil {
			t.Fatalf("remove %s: %v", n, err)
		}
	}
}
