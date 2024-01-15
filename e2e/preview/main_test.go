//go:build e2e

package preview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

var eopaHTTPPort int

func TestMain(m *testing.M) {
	r := rand.New(rand.NewSource(2912))
	for {
		port := r.Intn(38181) + 1
		if utils.IsTCPPortBindable(port) {
			eopaHTTPPort = port
			break
		}
	}

	os.Exit(m.Run())
}

func TestHelloWorld(t *testing.T) {
	policy := `
package test

hello := "world"
`
	eopa, eopaOut := loadEnterpriseOPA(t, policy, eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v0/preview/test", eopaHTTPPort), nil)
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
	res := struct {
		Result map[string]any `json:"result"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	exp := map[string]any{"hello": "world"}
	act := res.Result
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("unexpected response (-want, +got):\n%s", diff)
	}
}

func TestModules(t *testing.T) {
	policy := `
package test

hello := "world"
`
	eopa, eopaOut := loadEnterpriseOPA(t, policy, eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	body := `input:
  z: true
rego_modules:
  hello.rego: |
    package x.y
    import future.keywords.if
    z := input if input.z
`
	req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v0/preview/x/y/z", eopaHTTPPort), strings.NewReader(body))
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if exp, act := 200, resp.StatusCode; exp != act {
		t.Fatalf("expected status %d, got %d", exp, act)
	}
	res := struct {
		Result map[string]any `json:"result"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	exp := map[string]any{"z": true}
	act := res.Result
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("unexpected response (-want, +got):\n%s", diff)
	}
}

func TestLibrary(t *testing.T) {
	policy := `
package test

hello := "world"
`
	eopa, eopaOut := loadEnterpriseOPA(t, policy, eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	// NOTE(sr): We're overriding anything that _could_ use opa.runtime().env,
	// since that isn't wired up for the Preview API. Also, we're happy enough if
	// the preview run errors in the right way -- it means the library code was
	// available and evaluated.
	body := `
rego_modules:
  hello.rego: |
    package x.y
    import data.system.eopa.utils.vault.v1.env as vault
    vault_secret(path) := res {
      res := vault.secret(path)
        with vault.override.address as "http://127.0.0.1:8200"
        with vault.override.token as "toktok"
    }
    z := vault_secret("secret/path")
`
	req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v0/preview/x/y/z?strict-builtin-errors", eopaHTTPPort), strings.NewReader(body))
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := utils.StdlibHTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if exp, act := 500, resp.StatusCode; exp != act {
		t.Fatalf("expected status %d, got %d", exp, act)
	}
	res := struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}

	{
		exp := "error(s) occurred while evaluating query"
		act := res.Message
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Fatalf("unexpected message (-want, +got):\n%s", diff)
		}
		if exp, act := 1, len(res.Errors); act != exp {
			t.Fatalf("expected %d errors, got %d", exp, act)
		}
	}

	{
		exp := `vault.send: error encountered while reading secret at secret/data/path: Get "http://127.0.0.1:8200/v1/secret/data/path": dial tcp 127.0.0.1:8200: connect: connection refused`
		act := res.Errors[0].Message
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Fatalf("unexpected error message (-want, +got):\n%s", diff)
		}
	}
}

func loadEnterpriseOPA(t *testing.T, policy string, httpPort int) (*exec.Cmd, *bytes.Buffer) {
	buf := bytes.Buffer{}
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "eval.rego")
	if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	args := []string{
		"run",
		"--server",
		"--addr", fmt.Sprintf("localhost:%d", httpPort),
		"--log-level", "debug",
		"--disable-telemetry",
	}
	eopa := exec.Command(binary(), append(args, policyPath)...)
	eopa.Stderr = &buf
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
			t.Logf("eopa output:\n%s", buf.String())
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

// Tries an open port 3x times with short delays between each time to ensure the port is really free.
func isTCPPortOpen(port int) bool {
	portOpen := true
	for i := 0; i < 3; i++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
		}
		portOpen = portOpen && err == nil
		time.Sleep(time.Millisecond) // Adjust the delay as needed
	}
	return portOpen
}
