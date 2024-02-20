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
	"regexp"
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

// NOTE! To run these services, you'll need to build eopa with
//
//	make EOPA_TELEMETRY_URL=http://127.0.0.1:9191 eopa
//
// as otherwise the signal handler will not be set up, and the
// tests will fail to trigger a telemetry report. It looks like
// the ordinary
//
//	timeout waiting for result: log not found
func TestTelemetry(t *testing.T) {
	t.Setenv("EOPA_BUNDLE_SERVER", testserver.URL)

	tests := []struct {
		config         string
		bundlesExp     any
		datasourcesExp any
	}{
		{
			config:     "testdata/bundle.yml",
			bundlesExp: []any{map[string]any{"size": float64(113), "type": "snapshot", "format": "json"}},
		},
		{
			config:     "testdata/bjson-bundle.yml",
			bundlesExp: []any{map[string]any{"size": float64(118), "type": "snapshot", "format": "bjson"}},
		},
		{
			config: "testdata/nobundle.yml",
		},
		{
			config:     "testdata/disco.yml",
			bundlesExp: []any{map[string]any{"size": float64(113), "type": "snapshot", "format": "json"}},
		},
		{
			config:     "testdata/datasources.yml",
			bundlesExp: []any{map[string]any{"size": float64(113), "type": "snapshot", "format": "json"}},
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
			var lastUseragent string
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
				lastUseragent = r.Header.Get("user-agent")
				reqs.Done()
			}))
			srv := &http.Server{
				Addr:    "127.0.0.1:9191",
				Handler: m,
			}
			go srv.ListenAndServe()
			t.Cleanup(func() { srv.Shutdown(context.Background()) })

			eopa, _, eopaErr := loadEnterpriseOPA(t, tc.config, nil)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLogFields(t, eopaErr, func(m map[string]any) bool {
				return m["msg"] == "Enterprise OPA is out of date." &&
					m["release_notes"] == "Dummy Response" &&
					m["download_opa"] == "dummy-url" &&
					m["latest_version"] == "dummy"
			}, time.Second)

			if tc.bundlesExp != nil { // it's a test case with bundles
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
			if tc.bundlesExp != nil {
				exp = append(exp, "bundles")
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
				exp := tc.bundlesExp
				act := recv[1]["bundles"]
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("bundles unexpected: (-want, +got):\n%s", diff)
				}
			}

			{
				exp := tc.datasourcesExp
				act := recv[1]["datasources"]
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("datasources unexpected: (-want, +got):\n%s", diff)
				}
			}

			{
				re := regexp.MustCompile(`^Enterprise OPA/([0-9]+\.[0-9]+\.[0-9]+) Open Policy Agent/[0-9.]+ \([a-z]+, [a-z0-9-_]+\)$`)
				match := re.FindStringSubmatch(lastUseragent)
				if len(match) != 2 {
					t.Fatalf("user-agent unexpected: %s", lastUseragent)
				}
				exp := match[1]
				act := recv[1]["version"]
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("version unexpected: (-want, +got):\n%s", diff)
				}
			}
		})
	}
}

// This tests the telemetry additions for "OPA fallback mode". The setup and assertions
// are sufficiently different from the tests above to warrant its own test.
func TestTelemetryFallback(t *testing.T) {

	recv := make([]map[string]any, 0, 1)
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
	}))
	srv := &http.Server{
		Addr:    "127.0.0.1:9191",
		Handler: m,
	}
	go srv.ListenAndServe()
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	eopa, _, eopaErr := loadEnterpriseOPA(t, "testdata/nobundle.yml", filter)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLogFields(t, eopaErr, func(m map[string]any) bool {
		return m["msg"] == "OPA is out of date." &&
			m["release_notes"] == "Dummy Response" &&
			m["download_opa"] == "dummy-url" &&
			m["latest_version"] == "dummy"
	}, time.Second)

	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	if exp, act := 1, len(recv); exp != act {
		t.Fatalf("expected %d requests, got %d", exp, act)
	}

	{
		exp := []string{
			"heap_usage_bytes",
			"id",
			"version",
			"opa_fallback",
		}
		sort.Strings(exp)
		act := maps.Keys(recv[0])
		sort.Strings(act)
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("unexpected keys (-want, +got):\n%s", diff)
		}
	}

	{
		exp, act := true, recv[0]["opa_fallback"]
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("unexpected opa_fallback value (-want, +got):\n%s", diff)

		}
	}
}

func loadEnterpriseOPA(t *testing.T, config string, filter func([]string) []string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
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
	if filter == nil {
		filter = func(x []string) []string {
			return x
		}
	}
	eopa.Env = filter(append(eopa.Environ(),
		"EOPA_LICENSE_TOKEN="+os.Getenv("EOPA_LICENSE_TOKEN"),
		"EOPA_LICENSE_KEY="+os.Getenv("EOPA_LICENSE_KEY"),
	))

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

func filter(in []string) []string {
	out := []string{}
	for i := range in {
		if !strings.HasPrefix(in[i], "EOPA") {
			out = append(out, in[i])
		}
	}
	return out
}
