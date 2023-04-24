//go:build e2e

package kafka

import (
	"bufio"
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
	"github.com/open-policy-agent/opa/util"
	"github.com/ory/dockertest/docker"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Uses a httptest.Server for serving bundles from testdata/bundles.
// The `transform` bundle was build from testdata/bundles/source via
// `make transform` in testdata/bundles.
func TestTransformFromBundle(t *testing.T) {
	ctx := context.Background()
	configFmt := `
services:
- name: bundles
  url: %[1]s
bundles:
  transform:
    service: bundles
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [localhost:9092]
      topics: [foo]
      rego_transform: "data.transform.transform"`

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

	ts := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	config := fmt.Sprintf(configFmt, ts.URL)
	load, loadOut := loadRun(t, config)
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	waitForLog(ctx, t, loadOut, 1, equals(`kafka plugin (path /kafka/messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	waitForLog(ctx, t, loadOut, 1, equals(`Bundle loaded and activated successfully.`), 2*time.Second)

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

func loadRun(t *testing.T, config string) (*exec.Cmd, *bytes.Buffer) {
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

func waitForLog(ctx context.Context, t *testing.T, buf *bytes.Buffer, exp int, assert func(string) bool, dur time.Duration) {
	t.Helper()
	for i := 0; i <= 3; i++ {
		if retrieveMsg(ctx, t, buf, assert) {
			return
		}
		time.Sleep(dur)
	}
	t.Fatalf("timeout waiting for log")
}

func retrieveMsg(ctx context.Context, t *testing.T, buf *bytes.Buffer, assert func(string) bool) bool {
	t.Helper()
	b := bytes.NewReader(buf.Bytes())
	scanner := bufio.NewScanner(b)
	for scanner.Scan() {
		line := scanner.Bytes()
		var m struct {
			Msg string
		}
		if err := json.NewDecoder(bytes.NewReader(line)).Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode console logs: %v", err)
		}
		if assert(m.Msg) {
			return true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return false
}

func equals[T comparable](s T) func(T) bool {
	return func(t T) bool {
		return s == t
	}
}
