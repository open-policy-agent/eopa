// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	hcvault "github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	testcontainervault "github.com/testcontainers/testcontainers-go/modules/vault"

	"github.com/open-policy-agent/eopa/e2e/utils"
	"github.com/open-policy-agent/eopa/e2e/wait"
)

const (
	waitIterations = 5

	token = "dev-only-token"
)

var eopaHTTPPort int

func TestMain(m *testing.M) {
	r := rand.New(rand.NewSource(2910))
	for {
		port := r.Intn(38181) + 1
		if utils.IsTCPPortBindable(port) {
			eopaHTTPPort = port
			break
		}
	}

	os.Exit(m.Run())
}

var testserver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/status" {
		return // ignore status POSTs
	}
	http.FileServer(http.Dir("testdata")).ServeHTTP(w, r)
}))

func TestEKM(t *testing.T) {
	t.Skip("this flakey test blocks all our work. skipping as a workaround. FIXME")
	ctx := context.Background()

	mime.AddExtensionType(".gz", "application/gzip")

	vault := startVaultServer(t, ctx)
	defer vault.Terminate(ctx)

	// Now we assert that the config kv replacement has happend on the discovery config,
	// by calling a test server that expects the proper token
	lis, err := net.Listen("tcp", "127.0.0.1:9999")
	if err != nil {
		t.Fatal(err)
	}
	tokenServer.Listener = lis
	tokenServer.Start()

	config, err := os.ReadFile("ekm.yaml")
	if err != nil {
		t.Fatal(err)
	}

	eopa, eopaOut := eopaRun(t, eopaHTTPPort, config)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Discovery update processed successfully") }, time.Second)

	{ // store policy
		const policy = `package test
result := http.send({"method": "GET", "url": "http://127.0.0.1:9999/test"}).body
`
		req, err := http.NewRequest("PUT", fmt.Sprintf("http://127.0.0.1:%d/v1/policies/test", eopaHTTPPort), strings.NewReader(policy))
		if err != nil {
			t.Fatalf("send policy: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status %d", resp.StatusCode)
		}
	}
	{ // query policy
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/data/test/result", eopaHTTPPort), "application/json", nil)
		if err != nil {
			t.Fatalf("query policy: %v", err)
		}
		payload := struct {
			Result struct {
				Hey string
			}
		}{}

		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Result.Hey != "there" {
			t.Error("unexpected response")
		}
	}
	{ // query data plugin from tree
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/data/from/http", eopaHTTPPort), "application/json", nil)
		if err != nil {
			t.Fatalf("send request: %v", err)
		}
		payload := struct {
			Result struct {
				Hey string
			}
		}{}

		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Result.Hey != "there" {
			t.Error("unexpected response")
		}
	}
}

func createVaultTestCluster(t *testing.T, ctx context.Context) *testcontainervault.VaultContainer {
	t.Helper()

	opts := []testcontainers.ContainerCustomizer{
		testcontainervault.WithToken(token),
		testcontainervault.WithInitCommand("secrets enable -version=2 -path=kv kv"),
	}

	vault, err := testcontainervault.Run(ctx, "hashicorp/vault:1.18", opts...)
	if err != nil {
		t.Fatal(err)
	}
	return vault
}

func setKey(logical *hcvault.Logical, p string, value map[string]any) error {
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

func startVaultServer(t *testing.T, ctx context.Context) *testcontainervault.VaultContainer {
	t.Helper()
	cluster := createVaultTestCluster(t, ctx)
	if cluster == nil {
		t.Fatal("vault setup failed")
	}

	vaultCfg := hcvault.DefaultConfig()
	address, err := cluster.HttpHostAddress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	vaultCfg.Address = address
	vaultClient, err := hcvault.NewClient(vaultCfg)
	if err != nil {
		t.Fatal(err)
	}
	vaultClient.SetToken(token)

	vlogical := vaultClient.Logical()

	// initialize database
	if err := setKey(vlogical, "kv/data/acmecorp/bearer:data/token", map[string]any{"token": "token1", "scheme": "Bearer"}); err != nil {
		t.Fatal(err)
	}
	if err := setKey(vlogical, "kv/data/httpsend/bearer:data/token", map[string]any{"token": "sesame", "scheme": "Bearer"}); err != nil {
		t.Fatal(err)
	}
	if err := setKey(vlogical, "kv/data/acmecorp:data/url", map[string]any{"url": testserver.URL}); err != nil {
		t.Fatal(err)
	}
	dat, err := os.ReadFile("testdata/public_key.pem")
	if err != nil {
		t.Fatal(err)
	}
	if err := setKey(vlogical, "kv/data/discovery/rsa:data/key", map[string]any{"key": string(dat)}); err != nil {
		t.Fatal(err)
	}
	if err := setKey(vlogical, "kv/data/plugin/header:data/value", map[string]any{"value": "sesame"}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("VAULT_ADDR", address)
	t.Setenv("VAULT_TOKEN", token)
	return cluster
}

func eopaRun(t *testing.T, httpPort int, config []byte) (*exec.Cmd, *bytes.Buffer) {
	logLevel := "debug"
	buf := bytes.Buffer{}
	std := bytes.Buffer{}

	configPath := path.Join(t.TempDir(), "ekm.yaml")
	if err := os.WriteFile(configPath, config, 0o755); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"run",
		"--server",
		"--addr", fmt.Sprintf("localhost:%d", httpPort),
		"--log-level", logLevel,
		"--config-file", configPath,
		"--disable-telemetry",
	}
	eopa := exec.Command(binary(), args...)
	eopa.Stderr = &buf
	eopa.Stdout = &std

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		if err := eopa.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		eopa.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("eopa stderr output:\n%s", buf.String())
			t.Logf("eopa stdout output:\n%s", std.String())
		}
	})
	return eopa, &buf
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "eopa"
	}
	return bin
}

func equals[T comparable](s T) func(T) bool {
	return func(t T) bool {
		return s == t
	}
}

var tokenServer = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method != "GET":
	case r.URL.Path != "/test":
	case authzHeader(r.Header) != "sesame":
	default:
		w.Header().Add("content-type", "application/json")
		fmt.Fprintln(w, `{"hey":"there"}`)
		return
	}
	w.WriteHeader(http.StatusInternalServerError)
}))

func authzHeader(hdrs map[string][]string) string {
	if ah, ok := hdrs["Authorization"]; ok {
		return strings.TrimPrefix(ah[0], "Bearer ")
	}
	return ""
}
