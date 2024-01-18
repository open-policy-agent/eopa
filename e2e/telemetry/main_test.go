//go:build e2e

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/exp/maps"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

var testserver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/status" {
		return // ignore status POSTs
	}
	http.FileServer(http.Dir("testdata")).ServeHTTP(w, r)
}))

func TestTelemetry(t *testing.T) {
	tests := []struct {
		config string
	}{
		{
			config: "testdata/bundle.yml",
		},
		{
			config: "testdata/disco.yml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.config, func(t *testing.T) {

			reqs := sync.WaitGroup{}
			reqs.Add(2)

			recv := make([]map[string]any, 0, 2)
			m := http.NewServeMux()
			m.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				bs, _ := httputil.DumpRequest(r, true)
				t.Log("received", string(bs))
				json.NewEncoder(w).Encode(map[string]any{
					"latest": map[string]any{
						"release_notes":  "Dummy Response",
						"latest_release": "vdummy",
						"download":       "dummy-url",
						"opa_up_to_date": false,
					},
				})
				var req map[string]any
				json.NewDecoder(r.Body).Decode(&req)
				recv = append(recv, req)
				reqs.Done()
			}))
			srv := &http.Server{
				Addr:    "127.0.0.1:9191",
				Handler: m,
			}
			go srv.ListenAndServe()
			t.Cleanup(func() { srv.Shutdown(context.Background()) })
			t.Setenv("EOPA_BUNDLE_SERVER", testserver.URL)

			eopa, _, eopaErr := loadEnterpriseOPA(t, tc.config)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLogFields(t, eopaErr, func(m map[string]any) bool {
				return m["msg"] == "Enterprise OPA is out of date." &&
					m["release_notes"] == "Dummy Response" &&
					m["download_opa"] == "dummy-url" &&
					m["latest_version"] == "dummy"
			}, 5*time.Second)

			if err := eopa.Process.Signal(syscall.SIGUSR1); err != nil {
				t.Fatal(err)
			}

			reqs.Wait()
			if exp, act := 2, len(recv); exp != act {
				t.Fatalf("expected %d requests, got %d", exp, act)
			}

			exp := []string{
				"bundle_sizes",
				"heap_usage_bytes",
				"id",
				"license",
				"min_compatible_version",
				"version",
			}
			act := maps.Keys(recv[1])
			sort.Strings(act)
			if diff := cmp.Diff(exp, act); diff != "" {
				t.Errorf("unexpected keys (-want, +got):\n%s", diff)
			}
			for _, k := range exp {
				if recv[1][k] == "" {
					t.Errorf("expected value of %s to be non-empty", k)
				}
			}

			sizes := recv[1]["bundle_sizes"].([]any)
			if exp, act := 1, len(sizes); exp != act {
				t.Errorf("bundle_sizes, expected %d entries, got %d", exp, act)
			}
			if exp, act := float64(113), sizes[0]; exp != act {
				t.Errorf("bundle_sizes, expected %v (%[1]T), got %v (%[2]T)", exp, act)
			}
		})
	}
}

func loadEnterpriseOPA(t *testing.T, config string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	logLevel := "info" // log level of telemetry response

	stdout, stderr := bytes.Buffer{}, bytes.Buffer{}

	args := []string{
		"run",
		"--server",
		"--addr", "127.0.0.1:0", // NB: we'll never connect to this anyways
		"--log-level", logLevel,
		"--config-file", config,
	}
	eopa := exec.Command(binary(), args...)
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
