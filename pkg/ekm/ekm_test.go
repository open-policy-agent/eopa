package ekm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	hcvault "github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	testcontainervault "github.com/testcontainers/testcontainers-go/modules/vault"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/topdown/builtins"

	//"github.com/open-policy-agent/opa/topdown/cache"
	"github.com/open-policy-agent/opa/compile"
	"github.com/open-policy-agent/opa/config"
	"github.com/open-policy-agent/opa/ir"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/metrics"
	"github.com/open-policy-agent/opa/plugins"

	bjson "github.com/styrainc/load-private/pkg/json"
	"github.com/styrainc/load-private/pkg/vm"
)

const (
	testpath = "/test"
	token    = "dev-only-token"
)

func createVaultTestCluster(t *testing.T, url string) (*testcontainervault.VaultContainer, *hcvault.Client) {
	t.Helper()

	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithImage("hashicorp/vault:1.13.0"),
		testcontainervault.WithToken(token),
		testcontainervault.WithInitCommand("secrets enable -version=2 -path=kv kv"),
	}

	vault, err := testcontainervault.RunContainer(context.Background(), opts...)
	if err != nil {
		t.Fatal(err)
	}

	address, err := vault.HttpHostAddress(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	vaultCfg := hcvault.DefaultConfig()
	vaultCfg.Address = address
	vaultClient, err := hcvault.NewClient(vaultCfg)
	if err != nil {
		t.Fatal(err)
	}
	vaultClient.SetToken(token)

	vlogical := vaultClient.Logical()

	test := "abc"
	if err := setKey(vlogical, "kv/data/license:data/key", map[string]string{"key": test}); err != nil {
		t.Error(err)
	}

	value, err := lookupKey(vlogical, "kv/data/license:data/key")
	if err != nil {
		t.Error(err)
	}
	if value != test {
		t.Errorf("Invalid lookup: got %v, expected %v", value, test)
	}

	if err := setKey(vlogical, "kv/data/sign/:data/private_key", map[string]string{"private_key": "private_key1"}); err != nil {
		t.Error(err)
	}
	if err := setKey(vlogical, "kv/data/acmecorp/bearer:data/token", map[string]string{"token": "token1"}); err != nil {
		t.Error(err)
	}
	if err := setKey(vlogical, "kv/data/acmecorp:data/url", map[string]string{"url": address}); err != nil {
		t.Error(err)
	}
	if err := setKey(vlogical, "kv/data/tls/bearer:data/token", map[string]string{"url": url + testpath, "token": "token_good", "scheme": "Bearer"}); err != nil {
		t.Error(err)
	}

	_, err = lookupKey(vlogical, "kv/data/acmecorp/bearer:data/token")
	if err != nil {
		t.Error(err)
	}
	_, err = lookupKey(vlogical, "kv/data/acmecorp:data/url")
	if err != nil {
		t.Error(err)
	}
	_, err = lookupKey(vlogical, "kv/data/tls/bearer:data/token")
	if err != nil {
		t.Error(err)
	}

	return vault, vaultClient
}

func createHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Log(r.Method, r.URL.String())
		if r.Method != "GET" {
			t.Fatalf("Invalid method: got %v, expected GET", r.Method)
		}
		if r.URL.Path != testpath {
			t.Fatalf("Invalid path: got %v, expected %v", r.URL.Path, testpath)
		}
		h, ok := r.Header["Authorization"]
		if !ok {
			t.Fatal("Missing headers")
		}
		switch h[0] {
		case "Bearer token_good":
			w.Write([]byte("Success"))
		case "Bearer token_bad":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("500 - Something bad happened!"))
			return
		default:
			t.Fatal("Invalid authorization", h[0])
		}
	}))
	return srv
}

func setKey(vlogical *hcvault.Logical, p string, value map[string]string) error {
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
	secretData := map[string]any{
		f[0]: value,
	}

	_, err := vlogical.Write(path, secretData)
	return err
}

func writeFile(file string, buffer string) error {
	err := os.WriteFile(file, []byte(buffer), 0666)
	if err != nil {
		return fmt.Errorf("could not write %v: %w", file, err)
	}
	return nil
}

func TestEKM(t *testing.T) {
	// create http server
	srv := createHTTPServer(t)
	defer srv.Close()

	vault, vaultClient := createVaultTestCluster(t, srv.URL)
	defer vault.Terminate(context.Background())
	vlogical := vaultClient.Logical()

	address, _ := vault.HttpHostAddress(context.Background())

	err := writeFile("token_file", token)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove("token_file")

	t.Run("EKM", func(t *testing.T) {
		conf := config.Config{
			EKM:      []byte(`{"vault": {"license": {"key": "kv/data/license:data/key"}, "url": "` + address + `", "access_type": "token", "token_file": "token_file", "keys": {"jwt_signing.key": "kv/data/sign:data/private_key"}, "services": {"acmecorp.url": "kv/data/acmecorp:data/url", "acmecorp.credentials.bearer.token": "kv/data/acmecorp/bearer:data/token"}, "httpsend": {"https://www.acmecorp.com": {"url": "kv/data/tls/bearer:data/url", "header_bearer": "kv/data/tls/bearer:data/token", "header_scheme": "kv/data/tls/bearer:data/scheme"} } } }`),
			Services: []byte(`{"acmecorp": {"credentials": {"bearer": {"token": "bear"} } } }`),
			Keys:     []byte(`{"jwt_signing": {"key": "test"} }`),
		}

		e := NewEKM(nil, nil)
		cnf, err := e.ProcessEKM(plugins.EkmPlugins, logging.Get(), &conf)
		if err != nil {
			t.Error(err)
		}
		if !bytes.Equal(cnf.Services, []byte(`{"acmecorp":{"credentials":{"bearer":{"token":"token1"}},"url":"`+address+`"}}`)) {
			t.Errorf("invalid services: got %v", string(cnf.Services))
		}

		if !bytes.Equal(cnf.Keys, []byte(`{"jwt_signing":{"key":"private_key1"}}`)) {
			t.Errorf("invalid keys: got %v", string(cnf.Keys))
		}
	})

	// test http.send and EKM
	const simpleRego = `package test
default allow := false
allow {
  resp = http.send({"headers": {"Content-Type": "application/text"}, "method": "get", "url": "https://www.acmecorp.com", "raise_error": true})
  resp.raw_body == "Success"
}`

	bundle := createBundle(t, simpleRego)

	t.Run("httpsend.success", func(t *testing.T) {
		const simpleQuery = "test/allow"
		const simpleResult = `{{"result": true}}`
		policy := setup(t, bundle, simpleQuery)
		testCompiler(t, policy, "{}", simpleQuery, simpleResult, bundle.Data)
	})

	// change the token
	if err := setKey(vlogical, "kv/data/tls/bearer:data/token", map[string]string{"url": srv.URL + testpath, "token": "token_bad", "scheme": "Bearer"}); err != nil {
		t.Error(err)
	}

	t.Run("httpsend.badtoken", func(t *testing.T) {
		const simpleQuery = "test/allow"
		const simpleResult = `{{"result": false}}`
		policy := setup(t, bundle, simpleQuery)
		testCompiler(t, policy, "{}", simpleQuery, simpleResult, bundle.Data)
	})
}

func setup(tb testing.TB, b *bundle.Bundle, query string) ir.Policy {
	// use OPA to extract IR
	compiler := compile.New().WithTarget(compile.TargetPlan).WithBundle(b).WithEntrypoints(query)
	if err := compiler.Build(context.Background()); err != nil {
		tb.Fatal(err)
	}

	bundle := compiler.Bundle()
	var policy ir.Policy

	if err := json.Unmarshal(bundle.PlanModules[0].Raw, &policy); err != nil {
		tb.Fatal(err)
	}
	return policy
}

func createBundle(_ testing.TB, rego string) *bundle.Bundle {
	b := &bundle.Bundle{
		Modules: []bundle.ModuleFile{
			{
				URL:    "/url",
				Path:   "/foo.rego",
				Raw:    []byte(rego),
				Parsed: ast.MustParseModule(rego),
			},
		},
	}
	return b
}

func testCompiler(tb testing.TB, policy ir.Policy, input string, query string, result string, data any) {
	executable, err := vm.NewCompiler().WithPolicy(&policy).Compile()
	if err != nil {
		tb.Fatal(err)
	}

	_, ctx := vm.WithStatistics(context.Background())

	var inp interface{} = ast.MustParseTerm(input)

	bdata := bjson.MustNew(data)
	nvm := vm.NewVM().WithExecutable(executable).WithDataJSON(bdata)
	v, err := nvm.Eval(ctx, query, vm.EvalOpts{
		Cache:   builtins.Cache{},
		Input:   &inp,
		Time:    time.Now(),
		Metrics: metrics.New(),
	})
	if err != nil {
		tb.Fatal(err)
	}
	if result == "" {
		return
	}

	matchResult(tb, result, v)
}

func matchResult(tb testing.TB, result string, v vm.Value) {
	x, ok := v.(ast.Value)
	if !ok {
		tb.Fatalf("invalid conversion to ast.Value")
	}
	t := ast.MustParseTerm(result)
	if x.Compare(t.Value) != 0 {
		tb.Fatalf("got %v wanted %v\n", v, result)
	}
}
