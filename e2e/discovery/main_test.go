// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

var testserver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/status" {
		return // ignore status POSTs
	}
	http.FileServer(http.Dir("testdata")).ServeHTTP(w, r)
}))

func config(bndl, service string) string {
	return fmt.Sprintf(`
services:
- name: acmecorp
  url: %[2]s
discovery:
  name: example
  resource: %[1]s
  decision: config/discovery
`, bndl, service)
}

func TestDiscovery(t *testing.T) {
	for _, tc := range []struct {
		note   string
		bundle string
	}{
		{
			note:   "plain bundle",
			bundle: "disco.tar.gz",
		},
		{
			note:   "BJSON bundle",
			bundle: "disco.bjson.tar.gz",
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			config := config(tc.bundle, testserver.URL)
			eopa, eopaOut := eopaRun(t, config, eopaHTTPPort)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaOut, equals("Discovery update processed successfully."), 2*time.Second)

			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/v1/config", eopaHTTPPort))
			if err != nil {
				t.Fatal(err)
			}
			type c struct {
				Res struct {
					DefDec string `json:"default_decision"`
				} `json:"result"`
			}
			exp := "acmecorp/httpauthz/allow"
			var act c
			if err := json.NewDecoder(resp.Body).Decode(&act); err != nil {
				t.Fatal(err)
			}
			if act.Res.DefDec != exp {
				t.Errorf("unexpected default_decision: %s", act)
			}
		})
	}
}

func eopaRun(t *testing.T, config string, httpPort int) (*exec.Cmd, *bytes.Buffer) {
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
