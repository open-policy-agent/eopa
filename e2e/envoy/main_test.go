//go:build e2e

// package envoy is for testing Enterprise OPA as container, running as server,
// interacting with Envoy as an external authorizer using the opa-envoy-plugin.
package envoy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ory/dockertest"
	"github.com/ory/dockertest/docker"
)

const defaultImage = "ko.local/enterprise-opa-private:edge" // built via `make build-local`

var dockerPool = func() *dockertest.Pool {
	p, err := dockertest.NewPool("")
	if err != nil {
		panic(err)
	}

	if err = p.Client.Ping(); err != nil {
		panic(err)
	}
	return p
}()

func TestSimple(t *testing.T) {
	cleanupPrevious(t)
	image := os.Getenv("IMAGE")
	if image == "" {
		image = defaultImage
	}

	network, err := dockerPool.Client.CreateNetwork(docker.CreateNetworkOptions{Name: "eopa_envoy_e2e"})
	if err != nil {
		t.Fatalf("network: %v", err)
	}
	t.Cleanup(func() {
		if err := dockerPool.Client.RemoveNetwork(network.ID); err != nil {
			t.Fatal(err)
		}
	})

	// Start up containers.
	_ = loadEnterpriseOPA(t, map[string]string{
		"./yages.pb":    "/yages.pb",
		"./policy.rego": "/policy.rego",
		"./opa.yaml":    "/opa.yaml",
	}, image, network)
	_ = testEnvoy(t, network)
	_ = testTestsrv(t, network)

	// Try to get the simplest query to go through Envoy, authorized by the Envoy plugin (EOPA), and the
	// "upstream" service, YAGES.
	if err := grpcurl(t, "-protoset ./yages.pb -plaintext localhost:51051 yages.Echo.Ping"); err != nil {
		t.Fatalf("Ping stderr: %s", string(err.(*exec.ExitError).Stderr))
	}

	// Try a query that is only permitted if the request payload matches.
	if err := grpcurl(t, `-protoset ./yages.pb -d {"text":"Maddaddam"} -plaintext localhost:51051 yages.Echo.Reverse`); err != nil {
		t.Fatalf("Reverse stderr: %s", string(err.(*exec.ExitError).Stderr))
	}

	// If we reach this, none of our calls failed with a Permission Denied.
	// We've been making all requests through Envoy, and it is configured
	// to deny requests on ext_authz failures, so this means we're OK!
}

func loadEnterpriseOPA(t *testing.T, tempfileMounts map[string]string, image string, network *docker.Network) *dockertest.Resource {
	img := strings.Split(image, ":")

	dir := t.TempDir()
	mounts := make([]string, 0, len(tempfileMounts))
	for k, v := range tempfileMounts {
		tpath := filepath.Join(dir, v)
		fdata, err := os.ReadFile(k)
		if err != nil {
			t.Fatalf("could not read file: %v", err)
		}
		if err := os.WriteFile(tpath, fdata, 0x777); err != nil {
			t.Fatalf("write file: %v", err)
		}
		mounts = append(mounts, tpath+":"+v)
	}

	eopa, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "eopa-e2e",
		Repository: img[0],
		Tag:        img[1],
		Hostname:   "eopa-e2e",
		NetworkID:  network.ID,
		Env: []string{
			"EOPA_LICENSE_TOKEN=" + os.Getenv("EOPA_LICENSE_TOKEN"),
			"EOPA_LICENSE_KEY=" + os.Getenv("EOPA_LICENSE_KEY"),
		},
		Mounts: mounts,
		PortBindings: map[docker.Port][]docker.PortBinding{
			"9191/tcp": {{HostIP: "localhost", HostPort: "9191/tcp"}}, // gRPC
			"8181/tcp": {{HostIP: "localhost", HostPort: "8181/tcp"}}, // HTTP
		},
		ExposedPorts: []string{"9191/tcp", "8181/tcp"},
		Cmd:          strings.Split(`run --server --addr :8181 --log-level debug --disable-telemetry --config-file=/opa.yaml /policy.rego`, " "),
	})
	if err != nil {
		t.Fatalf("could not start %s: %s", image, err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(eopa); err != nil {
			t.Fatalf("could not purge eopa: %s", err)
		}
	})

	if err := dockerPool.Retry(func() error {
		return grpcurl(t, "-plaintext localhost:9191 list") // reflection on the gRPC plugin API
	}); err != nil {
		t.Fatalf("could not connect to test srv: %v", err)
	}

	return eopa
}

func testEnvoy(t *testing.T, network *docker.Network) *dockertest.Resource {
	dir := t.TempDir()
	cfg := "./envoy.yaml"
	tpath := filepath.Join(dir, cfg)
	fdata, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("could not read file: %v", err)
	}
	if err := os.WriteFile(tpath, fdata, 0x777); err != nil {
		t.Fatalf("write file: %v", err)
	}

	envoyResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "envoy-e2e",
		Repository: "envoyproxy/envoy",
		Tag:        "v1.26-latest",
		NetworkID:  network.ID,
		Hostname:   "envoy-e2e",
		Env:        []string{},
		Mounts: []string{
			tpath + ":/etc/envoy/envoy.yaml",
		},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"9901/tcp":  {{HostIP: "localhost", HostPort: "9901/tcp"}},
			"51051/tcp": {{HostIP: "localhost", HostPort: "51051/tcp"}},
		},
		ExposedPorts: []string{"9901/tcp", "51051/tcp"},
		Cmd:          []string{"envoy", "-c", "/etc/envoy/envoy.yaml", "--component-log-level", "ext_authz:trace"},
	})
	if err != nil {
		t.Fatalf("could not start envoy: %s", err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(envoyResource); err != nil {
			t.Fatalf("could not purge envoyResource: %s", err)
		}
	})

	return envoyResource
}

// Runs a simple echo service, see https://mhausenblas.info/yages/
// Note(philip): This has nothing to do with our gRPC API, we're just using
// Envoy's gRPC proxying capabilities for the demo.
func testTestsrv(t *testing.T, network *docker.Network) *dockertest.Resource {
	// Build and run the given Dockerfile.
	resource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "testsrv-e2e",
		Hostname:   "testsrv-e2e",
		NetworkID:  network.ID,
		Repository: "golang",
		Tag:        "latest",
		Cmd:        []string{"go", "run", "github.com/mhausenblas/yages@latest"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"9000/tcp": {{HostIP: "localhost", HostPort: "9000/tcp"}},
		},
		ExposedPorts: []string{"9000/tcp"},
	})
	if err != nil {
		t.Fatalf("Could not start resource: %v", err)
	}

	if err := dockerPool.Retry(func() error {
		return grpcurl(t, "-plaintext localhost:9000 yages.Echo.Ping") // direct call to the service, not through envoy
	}); err != nil {
		t.Fatalf("could not connect to test srv: %v", err)
	}

	t.Cleanup(func() {
		if err = dockerPool.Purge(resource); err != nil {
			t.Fatalf("Could not purge resource: %s", err)
		}
	})

	return resource
}

func cleanupPrevious(t *testing.T) {
	t.Helper()
	for _, n := range []string{"eopa-e2e", "envoy-e2e", "testsrv-e2e"} {
		if err := dockerPool.RemoveContainerByName(n); err != nil {
			t.Fatalf("remove %s: %v", n, err)
		}
	}
}

func grpcurl(t *testing.T, args string) error {
	t.Helper()
	stdout, err := exec.Command("grpcurl", strings.Split(args, " ")...).Output()
	t.Logf("grpcurl %s: %s", args, string(stdout))
	return err
}
