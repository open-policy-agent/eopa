// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package opentelemetry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tc_log "github.com/testcontainers/testcontainers-go/log"
	tc_wait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/open-policy-agent/eopa/e2e/retry"
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

func TestSpansEmitted(t *testing.T) {
	ctx := context.Background()
	coll, collectorPort := collector(t, ctx)
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	config := fmt.Sprintf(`
distributed_tracing:
  type: grpc
  address: "127.0.0.1:%[2]s"
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    buffer:
      type: unbuffered # handy for testing
    output:
    - type: console
    - type: http
      url: "%[1]s"
`, ts.URL, collectorPort)
	policy := fmt.Sprintf(`
package test
import future.keywords.if

p if http.send({"url":"%[1]s", "method":"GET"})
`, ts.URL)

	eopa, _, eopaErr := loadEnterpriseOPA(t, config, policy, eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	{ // act: send request
		req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/p", eopaHTTPPort), nil)
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

	expectedLines := []string{
		`service.name: Str(opa)`,
		`Name           : v1/data`,
		`InstrumentationScope rego_target_vm`,
		`Name           : HTTP GET`,
		`InstrumentationScope benthos`,
		`Name           : mapping`,
		`Name           : output_stdout`,
		`Name           : output_http_client`,
		`Name           : http_request`,
	}

	retry.Run(t, func(t *retry.R) {
		collectorOutput := output(t, ctx, coll)
		found := findAllOccurrences(collectorOutput, expectedLines)
		for _, k := range expectedLines {
			if act, exp := len(found[k]), 1; exp != act {
				t.Errorf("string %q expected %d occurrance(s), got %d", k, exp, act)
			}
		}
	})
}

func output(t *retry.R, ctx context.Context, coll testcontainers.Container) []byte {
	r, err := coll.Logs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func findAllOccurrences(data []byte, searches []string) map[string][]int { // https://stackoverflow.com/a/52684989/993018
	results := make(map[string][]int)
	for _, search := range searches {
		searchData := data
		term := []byte(search)
		for x, d := bytes.Index(searchData, term), 0; x > -1; x, d = bytes.Index(searchData, term), d+x+1 {
			results[search] = append(results[search], x+d)
			searchData = searchData[x+1:]
		}
	}
	return results
}

func loadEnterpriseOPA(t *testing.T, config, policy string, httpPort int) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	logLevel := "debug" // Needed for checking if server is ready

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

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "eopa"
	}
	return bin
}

func collector(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image:        "otel/opentelemetry-collector-contrib:0.81.0",
		ExposedPorts: []string{"4317/tcp"},
		WaitingFor:   tc_wait.ForLog("Everything is ready. Begin running and processing data."),
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Logger:           tc_log.TestLogger(t),
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
	}

	mappedPort, err := c.MappedPort(ctx, "4317/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return c, mappedPort.Port()
}
