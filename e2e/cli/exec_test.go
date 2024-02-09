//go:build e2e

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	sdk_test "github.com/open-policy-agent/opa/sdk/test"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	"github.com/styrainc/enterprise-opa-private/pkg/builtins"
)

var (
	eopaHTTPPort int
	eopaGRPCPort int
)

func TestMain(m *testing.M) {
	builtins.Init() // sdk_test needs to know the builtins to build a bundle on-demand

	r := rand.New(rand.NewSource(2907))
	for {
		port := r.Intn(38181) + 1
		if utils.IsTCPPortBindable(port) {
			eopaHTTPPort = port
			break
		}
	}

	for {
		port := r.Intn(38181) + 1
		if utils.IsTCPPortBindable(port) {
			eopaGRPCPort = port
			break
		}
	}
	os.Exit(m.Run())
}

func TestExecSuccessWithBuiltin(t *testing.T) {
	args := []string{"exec", "--log-level", "debug", "--decision", "/test/p", "--no-license-fallback"} // need to append input
	policy := `package test
import future.keywords
q := sql.send({}) # checking that the bundle can be read if it uses a builtin
p if input.foo.bar == "quz"
`
	s := sdk_test.MustNewServer(sdk_test.MockBundle("/bundles/bundle.tar.gz", map[string]string{"test.rego": policy}))
	t.Cleanup(s.Stop)

	config := fmt.Sprintf(`services:
  s:
    url: %s
bundles:
  bundle.tar.gz:
    service: s
    resource: bundles/bundle.tar.gz
`, // no plugins
		s.URL())

	inputPath := tempFile(t, map[string]any{"foo": map[string]any{"bar": "quz"}})

	eopa, _ := eopaCmd(t, "", config, append(args, inputPath)...)
	stdout, err := eopa.Output()
	if err != nil {
		t.Fatal(err)
	}
	act := map[string]any{}
	if err := json.NewDecoder(bytes.NewReader(stdout)).Decode(&act); err != nil {
		t.Fatalf("parse stdout: %v", err)
	}
	exp := map[string]any{
		"result": []any{
			any(map[string]any{
				"path":   inputPath,
				"result": true,
			}),
		},
	}
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("unexpected output (-want, +got):\n%s", diff)
	}
}

// TestExecSuccessWithPlugin checks two things:
//  1. plugins can be used with `eopa exec`, by using the EOPA decision log plugin
//  2. the rego vm was used for evaluationg, since the decision log's metrics contain
//     the metrics you don't get from topdown.
func TestExecSuccessWithPlugin(t *testing.T) {
	args := []string{"exec", "--log-level", "debug", "--decision", "/test/p", "--no-license-fallback"} // need to append input
	policy := `package test
import future.keywords
p if input.foo.bar == "quz"
`
	s := sdk_test.MustNewServer(sdk_test.MockBundle("/bundles/bundle.tar.gz", map[string]string{"test.rego": policy}))
	t.Cleanup(s.Stop)

	config := fmt.Sprintf(`services:
  s:
    url: %s
bundles:
  bundle.tar.gz:
    service: s
    resource: bundles/bundle.tar.gz
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
      type: console
`,
		s.URL())

	inputPath := tempFile(t, map[string]any{"foo": map[string]any{"bar": "quz"}})

	eopa, _ := eopaCmd(t, "", config, append(args, inputPath)...)
	stdout, err := eopa.Output()
	if err != nil {
		t.Fatal(err)
	}

	dec := json.NewDecoder(bytes.NewReader(stdout))
	{
		act := map[string]any{}
		if err := dec.Decode(&act); err != nil {
			t.Fatalf("parse stdout: %v", err)
		}
		exp := map[string]any{
			"result": []any{
				any(map[string]any{
					"path":   inputPath,
					"result": true,
				}),
			},
		}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("unexpected output (-want, +got):\n%s", diff)
		}
	}

	// NOTE(sr): decision logs are  also sent to stdout, unlikely to be used like
	// this in practice, but good enough for our tests here.
	{
		act := payload{}
		if err := dec.Decode(&act); err != nil {
			t.Fatalf("parse stdout for DL: %v", err)
		}
		exp := payload{
			Bundles: map[string]any{"bundle.tar.gz": map[string]any{}},
			Input:   map[string]any{"foo": map[string]any{"bar": "quz"}},
			Result:  true,
			Path:    "/test/p",
			Labels:  payloadLabels{Version: os.Getenv("EOPA_VERSION")},
		}
		ignores := cmpopts.IgnoreFields(payload{}, "Timestamp", "Metrics", "DecisionID", "Labels.ID", "NDBC")
		if diff := cmp.Diff(exp, act, ignores); diff != "" {
			t.Errorf("unexpected output (-want, +got):\n%s", diff)
		}

		// check EOPA-specific metrics:
		met := act.Metrics
		for _, m := range []string{"counter_regovm_eval_instructions", "counter_regovm_virtual_cache_hits", "counter_regovm_virtual_cache_misses"} {
			if _, ok := met[m]; !ok {
				t.Errorf("expected rego_vm metrics %s, found none", m)
			}
		}
		if t.Failed() {
			t.Logf("all metrics: %v", met)
		}
	}
}

type payload struct {
	Bundles    map[string]any `json:"bundles"`
	Result     any            `json:"result"`
	Metrics    map[string]int `json:"metrics"`
	ID         int            `json:"req_id"`
	DecisionID string         `json:"decision_id"`
	Path       string         `json:"path"`
	Labels     payloadLabels  `json:"labels"`
	NDBC       map[string]any `json:"nd_builtin_cache"`
	Input      any            `json:"input"`
	Erased     []string       `json:"erased"`
	Masked     []string       `json:"masked"`
	Timestamp  time.Time      `json:"timestamp"`
}

type payloadLabels struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

func tempFile(t *testing.T, in any) string {
	t.Helper()
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.json")
	f, err := os.Create(inputPath)
	if err != nil {
		t.Fatalf("write input.json: %v", err)
	}
	if err := json.NewEncoder(f).Encode(in); err != nil {
		t.Fatalf("encode input.json: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close input.json: %v", err)
	}
	return inputPath
}
