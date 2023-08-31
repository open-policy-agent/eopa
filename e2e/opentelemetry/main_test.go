//go:build e2e

package opentelemetry

import (
	"bytes"
	"context"
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

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/ory/dockertest/v3/docker/pkg/stdcopy"

	"github.com/styrainc/enterprise-opa-private/e2e/retry"
	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

const defaultImage = "ko.local/enterprise-opa-private:edge" // built via `make build-local`

var dockerPool = func() *dockertest.Pool {
	p, err := dockertest.NewPool("")
	if err != nil {
		panic(err)
	}
	if err := p.Client.Ping(); err != nil {
		panic(err)
	}
	return p
}()

func TestSpansEmitted(t *testing.T) {
	cleanupPrevious(t)
	coll := collector(t)
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	config := fmt.Sprintf(`
distributed_tracing:
  type: grpc
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
`, ts.URL)
	policy := fmt.Sprintf(`
package test
import future.keywords.if

p if http.send({"url":"%[1]s", "method":"GET"})
`, ts.URL)

	eopa, _, eopaErr := loadEnterpriseOPA(t, config, policy)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	{ // act: send request
		req, err := http.NewRequest("POST", "http://localhost:38181/v1/data/test/p", nil)
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
		collectorOutput := output(t, coll)
		found := findAllOccurrences(collectorOutput, expectedLines)
		for _, k := range expectedLines {
			if act, exp := len(found[k]), 1; exp != act {
				t.Errorf("string %q expected %d occurrance(s), got %d", k, exp, act)
			}
		}
	})
}

func output(t *retry.R, coll *dockertest.Resource) []byte {
	collBuf := bytes.Buffer{}

	opts := docker.LogsOptions{
		Context:      context.Background(),
		Stderr:       true,
		Stdout:       true,
		Follow:       false,
		RawTerminal:  true,
		Container:    coll.Container.ID,
		OutputStream: &collBuf,
	}
	if err := dockerPool.Client.Logs(opts); err != nil {
		t.Fatal(err)
	}

	collErr := bytes.Buffer{}
	if _, err := stdcopy.StdCopy(io.Discard, &collErr, &collBuf); err != nil {
		t.Fatal(err)
	}
	return collErr.Bytes()
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

func loadEnterpriseOPA(t *testing.T, config, policy string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
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
		"--addr", "localhost:38181",
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
	)

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

func collector(t *testing.T) *dockertest.Resource {
	res, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "collector",
		Repository: "otel/opentelemetry-collector-contrib",
		Tag:        "0.81.0",
		Hostname:   "collector",
		PortBindings: map[docker.Port][]docker.PortBinding{
			"4317/tcp": {{HostIP: "localhost", HostPort: "4317/tcp"}},
		},
		ExposedPorts: []string{"4317/tcp"},
	})
	if err != nil {
		t.Fatalf("could not start collector: %s", err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(res); err != nil {
			t.Fatalf("could not purge collector: %s", err)
		}
	})

	return res
}

func cleanupPrevious(t *testing.T) {
	t.Helper()
	for _, n := range []string{"collector"} {
		if err := dockerPool.RemoveContainerByName(n); err != nil {
			t.Fatalf("remove %s: %v", n, err)
		}
	}
}
