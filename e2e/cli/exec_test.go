//go:build e2e

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/open-policy-agent/opa/v1/ast"
	sdk_test "github.com/open-policy-agent/opa/v1/sdk/test"
	"github.com/open-policy-agent/opa/v1/types"

	"github.com/open-policy-agent/eopa/e2e/utils"
)

var (
	eopaHTTPPort int
	eopaGRPCPort int
)

func TestMain(m *testing.M) {
	// sdk_test needs to know the builtin to build a bundle on-demand
	ast.RegisterBuiltin(&ast.Builtin{
		Name:        "sql.send",
		Description: "Returns query result rows to the given SQL query.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("query object"),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("query result rows"),
		),
		Nondeterministic: true,
	})

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

type result struct {
	Path   string
	Result any
}

type results struct {
	Result []result
}

func TestExecSuccessWithBuiltin(t *testing.T) {
	args := []string{"exec", "--log-level", "debug", "--decision", "/test/p"} // need to append input
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

	act := results{}
	dec := json.NewDecoder(bytes.NewReader(stdout))
	if err := dec.Decode(&act); err != nil {
		t.Fatalf("parse stdout: %v", err)
	}
	exp := results{
		Result: []result{
			{
				Path:   inputPath,
				Result: true,
			},
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
	args := []string{"exec", "--log-level", "debug", "--decision", "/test/p"} // need to append input
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
		act := results{}
		if err := dec.Decode(&act); err != nil {
			t.Fatalf("parse stdout: %v", err)
		}
		exp := results{
			Result: []result{
				{
					Path:   inputPath,
					Result: true,
				},
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
			Labels:  payloadLabels{},
		}
		ignores := cmpopts.IgnoreFields(payload{},
			"Timestamp",
			"Metrics",
			"DecisionID",
			"Labels.ID",
			"Labels.Version",
			"NDBC",
		)
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

func eopaCmd(t *testing.T, policy, config string, args ...string) (*exec.Cmd, *bytes.Buffer) {
	buf := bytes.Buffer{}
	dir := t.TempDir()
	if config != "" {
		configPath := filepath.Join(dir, "config.yml")
		if err := os.WriteFile(configPath, []byte(config), 0x777); err != nil {
			t.Fatalf("write config: %v", err)
		}
		args = append(args, "--config-file", configPath)
	}
	if policy != "" {
		policyPath := filepath.Join(dir, "eval.rego")
		if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
			t.Fatalf("write policy: %v", err)
		}
		args = append(args, policyPath)
	}
	eopa := exec.Command(binary(), args...)
	eopa.Stderr = &buf

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		_ = eopa.Process.Signal(os.Interrupt)
		eopa.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("eopa output:\n%s", buf.String())
		}
	})

	return eopa, &buf
}
