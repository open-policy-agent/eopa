//go:build e2e

// package envoy is for testing Enterprise OPA as container, running as server,
// interacting with Envoy as an external authorizer using the opa-envoy-plugin.
package envoy

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/open-policy-agent/opa/v1/util"
	"github.com/testcontainers/testcontainers-go"
	tc_wait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

//go:embed envoy.yaml
var envoyConfigFmt []byte
var eopaEnvoyGRPCPort, eopaGRPCPort int

var ci = os.Getenv("CI") != ""

func freePort(first int) int {
	r := rand.New(rand.NewSource(2908))
	for {
		port := r.Intn(first) + 1
		if utils.IsTCPPortBindable(port) {
			return port
		}
	}
	panic("unreachable")
}

func TestMain(m *testing.M) {
	eopaEnvoyGRPCPort = freePort(38181)
	eopaGRPCPort = freePort(eopaEnvoyGRPCPort)

	os.Exit(m.Run())
}

func TestSimple(t *testing.T) {
	ctx := context.Background()
	const config = `plugins:
  envoy_ext_authz_grpc:
    addr: "%[1]s:%[2]d"
    path: envoy/authz/allow
    dry-run: false
    enable-reflection: true
    proto-descriptor: data.pb
  grpc:
    addr: "%[1]s:%[3]d"
decision_logs:
  console: true
`
	bind := "127.0.0.1"
	if ci {
		bind = "0.0.0.0"
	}

	const policy = `package envoy.authz

import rego.v1

default allow := false

allow if {
	input.parsed_path = ["eopa.data.v1.DataService", "GetData"]
	input.parsed_body = {
		"input": {"document": {"foo": "bar"}},
		"path": "/what/ever",
	}
}
`
	// Start up containers.
	eopa, _, eopaErr := loadEnterpriseOPA(t, "data.pb", fmt.Sprintf(config, bind, eopaEnvoyGRPCPort, eopaGRPCPort), policy, eopaEnvoyGRPCPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, 5*time.Second)

	eopaHost := "host.docker.internal"
	if ci {
		eopaHost = "172.17.0.1"
	}
	envoy, envoyEndpoint := testEnvoy(t, ctx, eopaHost, eopaEnvoyGRPCPort, eopaGRPCPort)
	t.Cleanup(func() { envoy.Terminate(ctx) })

	// Try to get the query to go through Envoy, authorized by the Envoy plugin (EOPA), and the
	// "upstream" service, EOPA's Data gRPC service.
	if err := grpcurl(t,
		fmt.Sprintf(`-protoset ./data.pb -d {"path":"/what/ever","input":{"document":{"foo":"bar"}}} -plaintext %s eopa.data.v1.DataService/GetData`, envoyEndpoint)); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("GetData stderr: %s", string(ee.Stderr))
		} else {
			t.Fatal(err)
		}
	}

	// If we reach this, none of our calls failed with a Permission Denied.
	// We've been making all requests through Envoy, and it is configured
	// to deny requests on ext_authz failures, so this means we're OK!
}

func testEnvoy(t *testing.T, ctx context.Context, eopaHost string, eopaEnvoyPort, eopaPort int) (testcontainers.Container, string) {
	dir := t.TempDir()
	cfg := "./envoy.yaml"
	tpath := filepath.Join(dir, cfg)
	config := fmt.Sprintf(string(envoyConfigFmt), eopaHost, eopaEnvoyPort, eopaGRPCPort)
	if err := os.WriteFile(tpath, []byte(config), 0x777); err != nil {
		t.Fatalf("write file: %v", err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "envoyproxy/envoy:v1.31-latest",
		ExposedPorts: []string{"51051/tcp"},
		Cmd:          []string{"envoy", "-c", "/etc/envoy/envoy.yaml", "--component-log-level", "ext_authz:trace"},
		WaitingFor:   tc_wait.ForListeningPort(nat.Port("51051/tcp")),
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      tpath,
				ContainerFilePath: "/etc/envoy/envoy.yaml",
				FileMode:          700,
			},
		},
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Logger:           testcontainers.TestLogger(t),
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
	}

	port, err := c.MappedPort(ctx, "51051/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return c, fmt.Sprintf("127.0.0.1:%s", port.Port())
}

func grpcurl(t *testing.T, args string) error {
	t.Helper()
	return util.WaitFunc(func() bool {
		stdout, err := exec.Command("grpcurl", strings.Split(args, " ")...).Output()
		t.Logf("grpcurl %s: %s (err: %v)", args, string(stdout), err)
		return err == nil
	}, 250*time.Millisecond, 5*time.Second)
}

func loadEnterpriseOPA(t *testing.T, protobuf, config, policy string, httpPort int) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
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
		"--addr", "localhost:0", // NOTE(sr): HTTP API isn't used for this test
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
