//go:build e2e

package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	kv "github.com/hashicorp/vault-plugin-secrets-kv"
	vault "github.com/hashicorp/vault/api"
	vaulthttp "github.com/hashicorp/vault/http"
	"github.com/hashicorp/vault/sdk/helper/logging"
	"github.com/hashicorp/vault/sdk/logical"
	hashivault "github.com/hashicorp/vault/vault"
)

const (
	waitIterations = 12
)

var testserver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/status" {
		return // ignore status POSTs
	}
	http.FileServer(http.Dir("testdata")).ServeHTTP(w, r)
}))

func TestEKM(t *testing.T) {
	ctx := context.Background()

	mime.AddExtensionType(".gz", "application/gzip")

	cluster := startVaultServer(t)
	defer cluster.Cleanup()

	load, loadOut := loadRun(t)
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	waitForLog(ctx, t, loadOut, func(s string) bool { return strings.Contains(s, "Discovery update processed successfully") }, time.Second)
}

func createVaultTestCluster(t *testing.T) *hashivault.TestCluster {
	t.Helper()

	coreConfig := &hashivault.CoreConfig{
		LogicalBackends: map[string]logical.Factory{
			"kv": kv.Factory,
		},
	}
	cluster := hashivault.NewTestCluster(t, coreConfig, &hashivault.TestClusterOptions{
		HandlerFunc:       vaulthttp.Handler,
		NumCores:          1,
		Logger:            logging.NewVaultLogger(hclog.Info),
		BaseListenAddress: "127.0.0.1:9001",
	})
	cluster.Start()

	// Create KV V2 mount
	if err := cluster.Cores[0].Client.Sys().Mount("kv", &vault.MountInput{
		Type: "kv-v2",
		Options: map[string]string{
			"version": "2",
		},
	}); err != nil {
		t.Fatal(err)
	}
	return cluster
}

func setKey(logical *vault.Logical, p string, value map[string]any) error {
	p = strings.TrimSpace(p)
	arr := strings.Split(p, ":")
	if len(arr) != 2 {
		return fmt.Errorf("Invalid path: %v", p)
	}

	path := arr[0]
	field := arr[1]

	f := strings.Split(field, "/")
	if len(arr) != 2 {
		return fmt.Errorf("Invalid field: %v", f)
	}
	secretData := map[string]any{f[0]: value}

	_, err := logical.Write(path, secretData)
	return err
}

func startVaultServer(t *testing.T) *hashivault.TestCluster {
	cluster := createVaultTestCluster(t)
	if cluster == nil {
		t.Fatal("vault setup failed")
	}
	vaultClient := cluster.Cores[0].Client
	logical := vaultClient.Logical()

	// initialize database
	if err := setKey(logical, "kv/data/acmecorp/bearer:data/token", map[string]any{"token": "token1", "scheme": "Bearer"}); err != nil {
		t.Fatal(err)
	}
	if err := setKey(logical, "kv/data/acmecorp:data/url", map[string]any{"url": testserver.URL}); err != nil {
		t.Fatal(err)
	}
	if err := setKey(logical, "kv/data/license:data/key", map[string]any{"key": os.Getenv("STYRA_LOAD_LICENSE_KEY")}); err != nil {
		t.Fatal(err)
	}
	dat, err := os.ReadFile("testdata/public_key.pem")
	if err != nil {
		t.Fatal(err)
	}
	if err := setKey(logical, "kv/data/discovery/rsa:data/key", map[string]any{"key": string(dat)}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VAULT_TOKEN", cluster.RootToken)
	return cluster
}

func loadRun(t *testing.T, extraArgs ...string) (*exec.Cmd, *bytes.Buffer) {
	logLevel := "debug"
	buf := bytes.Buffer{}

	args := []string{
		"run",
		"--server",
		"--log-level", logLevel,
		"--config-file", "ekm.yaml",
		"--disable-telemetry",
	}
	args = append(args, extraArgs...)
	load := exec.Command(binary(), args...)
	load.Stderr = &buf
	load.Env = append(load.Environ(),
		"STYRA_LOAD_LICENSE_TOKEN="+os.Getenv("STYRA_LOAD_LICENSE_TOKEN"),
		"STYRA_LOAD_LICENSE_KEY="+os.Getenv("STYRA_LOAD_LICENSE_KEY"),
	)

	t.Cleanup(func() {
		if load.Process == nil {
			return
		}
		if err := load.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		load.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("load output:\n%s", buf.String())
		}
	})
	return load, &buf
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "load"
	}
	return bin
}

func waitForLog(ctx context.Context, t *testing.T, buf *bytes.Buffer, assert func(string) bool, dur time.Duration) {
	t.Helper()
	for i := 0; i <= 5; i++ {
		if retrieveMsg(ctx, t, buf, assert) {
			return
		}
		time.Sleep(dur)
	}
	t.Fatalf("timeout waiting for log")
}

func retrieveMsg(ctx context.Context, t *testing.T, buf *bytes.Buffer, assert func(string) bool) bool {
	t.Helper()
	b := bytes.NewReader(buf.Bytes())
	scanner := bufio.NewScanner(b)
	for scanner.Scan() {
		line := scanner.Bytes()
		var m struct {
			Msg string
		}
		if err := json.NewDecoder(bytes.NewReader(line)).Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode console logs: %v", err)
		}
		t.Log(m.Msg)
		if assert(m.Msg) {
			return true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return false
}

func equals[T comparable](s T) func(T) bool {
	return func(t T) bool {
		return s == t
	}
}
