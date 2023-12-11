//go:build e2e

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/exp/maps"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

func TestTelemetry(t *testing.T) {
	var recv map[string]any
	m := http.NewServeMux()
	m.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"latest": map[string]any{
				"release_notes":  "Dummy Response",
				"latest_release": "vdummy",
				"download":       "dummy-url",
				"opa_up_to_date": false,
			},
		})
		json.NewDecoder(r.Body).Decode(&recv)
	}))
	srv := &http.Server{
		Addr:    "127.0.0.1:9191",
		Handler: m,
	}
	go srv.ListenAndServe()
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	eopa, _, eopaErr := loadEnterpriseOPA(t)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLogFields(t, eopaErr, func(m map[string]any) bool {
		return m["msg"] == "Enterprise OPA is out of date." &&
			m["release_notes"] == "Dummy Response" &&
			m["download_opa"] == "dummy-url" &&
			m["latest_version"] == "dummy"
	}, time.Second)

	exp := []string{
		"heap_usage_bytes",
		"id",
		"license",
		"version",
	}
	act := maps.Keys(recv)
	sort.Strings(act)
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("unexpected keys (-want, +got):\n%s", diff)
	}
	for _, k := range exp {
		if recv[k] == "" {
			t.Errorf("expected value of %s to be non-empty", k)
		}
	}
}

func loadEnterpriseOPA(t *testing.T) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	logLevel := "info" // log level of telemetry response

	stdout, stderr := bytes.Buffer{}, bytes.Buffer{}

	args := []string{
		"run",
		"--server",
		"--addr", "127.0.0.1:0", // NB: we'll never connect to this anyways
		"--log-level", logLevel,
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
