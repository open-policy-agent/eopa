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
	"strings"
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
	t.Setenv("EOPA_BUNDLE_SERVER", testserver.URL)

	tests := []struct {
		config         string
		bundleSizesExp any
		datasourcesExp any
	}{
		{
			config:         "testdata/bundle.yml",
			bundleSizesExp: []any{float64(113)},
		},
		{
			config: "testdata/nobundle.yml",
		},
		{
			config:         "testdata/disco.yml",
			bundleSizesExp: []any{float64(113)},
		},
		{
			config:         "testdata/datasources.yml",
			bundleSizesExp: []any{float64(113)},
			datasourcesExp: map[string]any{
				"http": float64(3),
				"git":  float64(1),
			},
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

			eopa, _, eopaErr := loadEnterpriseOPA(t, tc.config)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLogFields(t, eopaErr, func(m map[string]any) bool {
				return m["msg"] == "Enterprise OPA is out of date." &&
					m["release_notes"] == "Dummy Response" &&
					m["download_opa"] == "dummy-url" &&
					m["latest_version"] == "dummy"
			}, time.Second)

			if tc.bundleSizesExp != nil { // it's a test case with bundles
				wait.ForLog(t, eopaErr, func(s string) bool { return s == "Bundle loaded and activated successfully." }, 5*time.Second)
			} else {
				wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)
			}

			if err := eopa.Process.Signal(syscall.SIGUSR1); err != nil {
				t.Fatal(err)
			}

			reqs.Wait()
			if exp, act := 2, len(recv); exp != act {
				t.Fatalf("expected %d requests, got %d", exp, act)
			}

			exp := []string{
				"heap_usage_bytes",
				"id",
				"license",
				"min_compatible_version",
				"version",
			}
			if tc.bundleSizesExp != nil {
				exp = append(exp, "bundle_sizes")
			}
			if tc.datasourcesExp != nil {
				exp = append(exp, "datasources")
			}
			sort.Strings(exp)
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

			{
				exp := tc.bundleSizesExp
				act := recv[1]["bundle_sizes"]
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("bundle_sizes unexpected: (-want, +got):\n%s", diff)
				}
			}

			{
				exp := tc.datasourcesExp
				act := recv[1]["datasources"]
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("datasources unexpected: (-want, +got):\n%s", diff)
				}
			}

		})
	}
}

func loadEnterpriseOPA(t *testing.T, config string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	logLevel := "debug"

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
