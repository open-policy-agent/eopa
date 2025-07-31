// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

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
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/open-policy-agent/eopa/e2e/wait"
)

// Uses a httptest.Server for serving bundles from testdata/bundles.
// The `transform` bundle was build from testdata/bundles/source via
// `make transform` in testdata/bundles.
func TestTransformFromBundle(t *testing.T) {
	ctx := context.Background()

	broker, tx := testKafka(t, ctx)
	t.Cleanup(func() { tx.Terminate(ctx) })

	cl, err := kafkaClient(broker)
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

	eopa, eopaOut := eopaRun(t, config("transform", testserver.URL, broker), "", eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, equals(`kafka plugin (data.kafka.messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	wait.ForLog(t, eopaOut, equals(`Bundle loaded and activated successfully.`), 2*time.Second)

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

	if err := wait.Func(func() bool {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/data/kafka/messages", eopaHTTPPort))
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
	eopa, eopaOut := eopaRun(t, config("no-roots", testserver.URL, "127.0.0.1:9191"), "", eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, equals(`kafka plugin (data.kafka.messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	wait.ForLog(t, eopaOut, equals(`kafka plugin: data.kafka.messages overlaps with bundle root []`), 2*time.Second)
	wait.ForLog(t, eopaOut, equals(`Bundle loaded and activated successfully.`), 2*time.Second)

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
	eopa, eopaOut := eopaRun(t, config("overlap", testserver.URL, "127.0.0.1:9191"), "", eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, equals(`kafka plugin (data.kafka.messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	wait.ForLog(t, eopaOut, equals(`Bundle activation failed: path "/kafka/messages" is owned by plugin "kafka"`), 2*time.Second)

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
	config := config("prefix", testserver.URL, "127.0.0.1:9191")
	eopa, eopaOut := eopaRun(t, config, "", eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, equals(`kafka plugin (data.kafka.messages): transform rule "data.transform.transform" does not exist yet`), 2*time.Second)
	wait.ForLog(t, eopaOut, equals(`kafka plugin: data.kafka.messages overlaps with bundle root [transform kafka]`), 2*time.Second)
	wait.ForLog(t, eopaOut, equals(`Bundle loaded and activated successfully.`), 2*time.Second)

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

func config(bndl, service, broker string) string {
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
      urls: [%[3]s]
      topics: [foo]
      rego_transform: "data.transform.transform"`, bndl, service, broker)
}

func assertStatus(t *testing.T, exp map[string]any) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/status", eopaHTTPPort))
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

func eopaRun(t *testing.T, config, policy string, httpPort int, extra ...string) (*exec.Cmd, *bytes.Buffer) {
	buf := bytes.Buffer{}
	dir := t.TempDir()
	args := []string{
		"run",
		"--server",
		"--addr", fmt.Sprintf("localhost:%d", httpPort),
		"--disable-telemetry",
		"--log-level", "debug",
	}
	if config != "" {
		configPath := filepath.Join(dir, "config.yml")
		if err := os.WriteFile(configPath, []byte(config), 0x777); err != nil {
			t.Fatalf("write config: %v", err)
		}
		args = append(args, "--config-file", configPath)
	}
	if policy != "" {
		policyPath := filepath.Join(dir, "policy.rego")
		if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
			t.Fatalf("write policy: %v", err)
		}
		args = append(args, policyPath)
	}
	if len(extra) > 0 {
		args = append(args, extra...)
	}
	eopa := exec.Command(binary(), args...)
	eopa.Stderr = &buf

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		if err := eopa.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		eopa.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("enterprise OPA output:\n%s", buf.String())
		}
	})

	return eopa, &buf
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "eopa"
	}
	return bin
}

func equals[T comparable](s T) func(T) bool {
	return func(t T) bool {
		return s == t
	}
}
