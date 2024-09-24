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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	tc_wait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

//go:embed envoy.yaml
var envoyConfigFmt []byte
var eopaHTTPPort int

var ci = os.Getenv("CI") != ""

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

func TestSimple(t *testing.T) {
	if ci {
		t.Skipf("%s is flaky in CI", t.Name())
	}
	ctx := context.Background()
	const config = `plugins:
  envoy_ext_authz_grpc:
    addr: "%[1]s:%[2]d"
    path: envoy/authz/allow
    dry-run: false
    enable-reflection: true
    proto-descriptor: yages.pb
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

allow if input.parsed_path = ["yages.Echo", "Ping"] # Ping is OK

allow if {
  input.parsed_path = ["yages.Echo", "Reverse"]
  input.parsed_body = {
    "text": "Maddaddam"
  }
}
`
	// Start up containers.
	eopa, _, eopaErr := loadEnterpriseOPA(t, "yages.pb", fmt.Sprintf(config, bind, eopaHTTPPort), policy, eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, 5*time.Second)

	ts, tsHost, tsPort := testTestsrv(t, ctx)
	t.Cleanup(func() { ts.Terminate(ctx) })

	eopaHost := "host.docker.internal"
	if ci {
		eopaHost = "172.17.0.1"
	}
	envoy, envoyEndpoint := testEnvoy(t, ctx, eopaHost, eopaHTTPPort, tsHost, tsPort)
	t.Cleanup(func() { envoy.Terminate(ctx) })

	// Try to get the simplest query to go through Envoy, authorized by the Envoy plugin (EOPA), and the
	// "upstream" service, YAGES.
	if err := grpcurl(t,
		fmt.Sprintf("-protoset ./yages.pb -plaintext %s yages.Echo.Ping", envoyEndpoint)); err != nil {
		t.Fatalf("Ping stderr: %s", string(err.(*exec.ExitError).Stderr))
	}

	// Try a query that is only permitted if the request payload matches.
	if err := grpcurl(t,
		fmt.Sprintf(`-protoset ./yages.pb -d {"text":"Maddaddam"} -plaintext %s yages.Echo.Reverse`, envoyEndpoint)); err != nil {
		t.Fatalf("Reverse stderr: %s", string(err.(*exec.ExitError).Stderr))
	}

	// If we reach this, none of our calls failed with a Permission Denied.
	// We've been making all requests through Envoy, and it is configured
	// to deny requests on ext_authz failures, so this means we're OK!
}

func testEnvoy(t *testing.T, ctx context.Context, eopaHost string, eopaPort int, tsHost string, tsPort int) (testcontainers.Container, string) {
	dir := t.TempDir()
	cfg := "./envoy.yaml"
	tpath := filepath.Join(dir, cfg)
	config := fmt.Sprintf(string(envoyConfigFmt), tsHost, tsPort, eopaHost, eopaPort)
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

// Runs a simple echo service, see https://mhausenblas.info/yages/
// Note(philip): This has nothing to do with our gRPC API, we're just using
// Envoy's gRPC proxying capabilities for the demo.
func testTestsrv(t *testing.T, ctx context.Context) (testcontainers.Container, string, int) {
	req := testcontainers.ContainerRequest{
		Image:        "golang:1.21",
		ExposedPorts: []string{"9000/tcp"},
		WaitingFor:   tc_wait.ForExposedPort(),
		Cmd:          []string{"go", "run", "github.com/mhausenblas/yages@latest"},
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Logger:           testcontainers.TestLogger(t),
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ip := "host.docker.internal"
	if ci {
		ip = "172.17.0.1"
	}
	port, err := c.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatal(err)
	}
	iport, err := strconv.Atoi(port.Port())
	if err != nil {
		t.Fatal(err)
	}
	return c, ip, iport
}

func grpcurl(t *testing.T, args string) error {
	t.Helper()
	stdout, err := exec.Command("grpcurl", strings.Split(args, " ")...).Output()
	t.Logf("grpcurl %s: %s", args, string(stdout))
	return err
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
