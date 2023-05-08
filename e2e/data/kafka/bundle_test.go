//go:build e2e

package kafka

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ory/dockertest/docker"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/load-private/e2e/wait"
)

// Uses a httptest.Server for serving bundles from testdata/bundles.
// The `transform` bundle was build from testdata/bundles/source via
// `make transform` in testdata/bundles.
func TestTransformFromBundle(t *testing.T) {
	ctx := context.Background()
	cleanupPrevious(t)
	_ = testKafka(t, network(t))
	cl, err := kafkaClient()
	if err != nil {
		t.Fatal(err)
	}
	// message that's present before we start (should not be dropped)
	if err := cl.ProduceSync(ctx, &kgo.Record{
		Topic: "foo",
		Key:   []byte("one"),
		Value: []byte(`{"foo":"bar"}`),
	}).FirstErr(); err != nil {
		t.Fatalf("produce msg: %v", err)
	}

	load, loadOut := loadRun(t, config("transform", testserver.URL))
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, equals(`kafka plugin (path /kafka/messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	wait.ForLog(t, loadOut, equals(`Bundle loaded and activated successfully.`), 2*time.Second)

	statusOK := map[string]any{"state": "OK"}
	assertStatus(t, map[string]any{
		"bundle":    statusOK,
		"data":      statusOK,
		"discovery": statusOK,
		"status":    statusOK,
	})

	if err := cl.ProduceSync(ctx, &kgo.Record{
		Topic: "foo",
		Key:   []byte("two"),
		Value: []byte(`{"fox":"box"}`),
	}).FirstErr(); err != nil {
		t.Fatalf("produce msg: %v", err)
	}

	exp := map[string]any{
		"one": map[string]any{
			"headers": []any{},
			"value": map[string]any{
				"foo": "bar",
			},
		},
		"two": map[string]any{
			"headers": []any{},
			"value": map[string]any{
				"fox": "box",
			},
		},
	}

	if err := util.WaitFunc(func() bool {
		resp, err := http.Get("http://127.0.0.1:8181/v1/data/kafka/messages")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		act := map[string]any{}
		if err := json.Unmarshal(body, &act); err != nil {
			t.Fatal(err)
		}
		diff := cmp.Diff(exp, act["result"])
		return diff == ""
	}, 500*time.Millisecond, 5*time.Second); err != nil {
		t.Error(err)
	}
}

// The bundle used in this test declares no roots, so it owns all of 'data'.
func TestOverlapBundleWithoutRoots(t *testing.T) {
	load, loadOut := loadRun(t, config("no-roots", testserver.URL))
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, equals(`kafka plugin (path /kafka/messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	wait.ForLog(t, loadOut, equals(`data plugin: kafka path kafka/messages overlaps with bundle root []`), 2*time.Second)
	wait.ForLog(t, loadOut, equals(`Bundle loaded and activated successfully.`), 2*time.Second)

	statusOK := map[string]any{"state": "OK"}
	assertStatus(t, map[string]any{
		"bundle":    statusOK,
		"data":      map[string]any{"state": "ERROR"},
		"discovery": statusOK,
		"status":    statusOK,
	})
}

// The bundle used here declares the root "data.kafka.messages"
func TestOverlapBundleOverlappingRoots(t *testing.T) {
	load, loadOut := loadRun(t, config("overlap", testserver.URL))
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, equals(`kafka plugin (path /kafka/messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	wait.ForLog(t, loadOut, equals(`Bundle activation failed: path "/kafka/messages" is owned by plugin "kafka"`), 2*time.Second)

	statusOK := map[string]any{"state": "OK"}
	assertStatus(t, map[string]any{
		"bundle":    map[string]any{"state": "NOT_READY"},
		"data":      statusOK,
		"discovery": statusOK,
		"status":    statusOK,
	})
}

// The bundle used here declares the root "data.kafka", a prefix of "data.kafka.messages"
func TestOverlapBundlePrefixRoot(t *testing.T) {
	config := fmt.Sprintf(config("prefix", testserver.URL))
	load, loadOut := loadRun(t, config)
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, loadOut, equals(`kafka plugin (path /kafka/messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	wait.ForLog(t, loadOut, equals(`data plugin: kafka path kafka/messages overlaps with bundle root [transform kafka]`), 2*time.Second)
	wait.ForLog(t, loadOut, equals(`Bundle loaded and activated successfully.`), 2*time.Second)

	statusOK := map[string]any{"state": "OK"}
	assertStatus(t, map[string]any{
		"bundle":    statusOK,
		"data":      map[string]any{"state": "ERROR"},
		"discovery": statusOK,
		"status":    statusOK,
	})
}

var testserver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/status" {
		return // ignore status POSTs
	}
	http.FileServer(http.Dir("testdata")).ServeHTTP(w, r)
}))

func config(bndl, service string) string {
	return fmt.Sprintf(`
services:
- name: testserver
  url: %[2]s
bundles:
  %[1]s:
    service: testserver
status:
  service: testserver
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [localhost:9092]
      topics: [foo]
      rego_transform: "data.transform.transform"`, bndl, service)
}

func assertStatus(t *testing.T, exp map[string]any) {
	resp, err := http.Get("http://127.0.0.1:8181/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	type pluginStatus struct { // subset of the status payload we're interested in
		Plugins map[string]any
	}
	var act struct {
		Result pluginStatus
	}
	if err := json.NewDecoder(resp.Body).Decode(&act); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(exp, act.Result.Plugins); diff != "" {
		t.Errorf("unexpected status response (-want, +got):\n%s", diff)
	}
}

func network(t *testing.T) *docker.Network {
	network, err := dockerPool.Client.CreateNetwork(docker.CreateNetworkOptions{Name: "load_kafka_e2e"})
	if err != nil {
		t.Fatalf("network: %v", err)
	}
	t.Cleanup(func() {
		if err := dockerPool.Client.RemoveNetwork(network.ID); err != nil {
			t.Fatal(err)
		}
	})
	return network
}

func loadRun(t *testing.T, config string, extra ...string) (*exec.Cmd, *bytes.Buffer) {
	buf := bytes.Buffer{}
	dir := t.TempDir()
	args := []string{
		"run",
		"--server",
		"--addr", "localhost:8181",
		"--disable-telemetry",
	}
	if config != "" {
		configPath := filepath.Join(dir, "config.yml")
		if err := os.WriteFile(configPath, []byte(config), 0x777); err != nil {
			t.Fatalf("write config: %v", err)
		}
		args = append(args, "--config-file", configPath)
	}
	if len(extra) > 0 {
		args = append(args, extra...)
	}
	load := exec.Command(binary(), args...)
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
		if testing.Verbose() && t.Failed() {
			t.Logf("load output:\n%s", buf.String())
		}
	})

	return load, &buf
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "load"
	}
	return bin
}

func equals[T comparable](s T) func(T) bool {
	return func(t T) bool {
		return s == t
	}
}
