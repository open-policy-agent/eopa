package ekm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	kv "github.com/hashicorp/vault-plugin-secrets-kv"
	vault "github.com/hashicorp/vault/api"
	vaulthttp "github.com/hashicorp/vault/http"
	hclogging "github.com/hashicorp/vault/sdk/helper/logging"
	"github.com/hashicorp/vault/sdk/logical"
	hashivault "github.com/hashicorp/vault/vault"

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

const testpath = "/test"

func createVaultTestCluster(t *testing.T, url string) *hashivault.TestCluster {
	t.Helper()

	coreConfig := &hashivault.CoreConfig{
		LogicalBackends: map[string]logical.Factory{
			"kv": kv.Factory,
		},
	}
	cluster := hashivault.NewTestCluster(t, coreConfig, &hashivault.TestClusterOptions{
		HandlerFunc: vaulthttp.Handler,
		NumCores:    1,
		Logger:      hclogging.NewVaultLogger(hclog.Info),
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

	vaultClient := cluster.Cores[0].Client
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
	if err := setKey(vlogical, "kv/data/acmecorp:data/url", map[string]string{"url": "http://127.0.0.1:9000"}); err != nil {
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

	return cluster
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

func setKey(vlogical *vault.Logical, p string, value map[string]string) error {
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

func TestEKM(t *testing.T) {
	// create http server
	srv := createHTTPServer(t)
	defer srv.Close()

	cluster := createVaultTestCluster(t, srv.URL)
	defer cluster.Cleanup()
	vaultClient := cluster.Cores[0].Client
	vlogical := vaultClient.Logical()

	rootToken := vaultClient.Token()
	addr := cluster.Cores[0].Listeners[0].Address.String()

	t.Run("EKM", func(t *testing.T) {
		conf := config.Config{
			EKM:      []byte(`{"vault": {"license": {"key": "kv/data/license:data/key"}, "rootca": "` + strings.ReplaceAll(string(cluster.CACertPEM), "\n", "\\n") + `", "url": "https://` + addr + `", "access_type": "token", "token": "` + rootToken + `", "keys": {"jwt_signing.key": "kv/data/sign:data/private_key"}, "services": {"acmecorp.url": "kv/data/acmecorp:data/url", "acmecorp.credentials.bearer.token": "kv/data/acmecorp/bearer:data/token"}, "httpsend": {"https://www.acmecorp.com": {"url": "kv/data/tls/bearer:data/url", "bearer": "kv/data/tls/bearer:data/token", "scheme": "kv/data/tls/bearer:data/scheme"} } } }`),
			Services: []byte(`{"acmecorp": {"credentials": {"bearer": {"token": "bear"} } } }`),
			Keys:     []byte(`{"jwt_signing": {"key": "test"} }`),
		}

		e := NewEKM(nil, nil)
		cnf, err := e.ProcessEKM(plugins.EkmPlugins, logging.Get(), &conf)
		if err != nil {
			t.Error(err)
		}
		if !bytes.Equal(cnf.Services, []byte(`{"acmecorp":{"credentials":{"bearer":{"token":"token1"}},"url":"http://127.0.0.1:9000"}}`)) {
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
