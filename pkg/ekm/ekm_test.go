package ekm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	hcvault "github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	testcontainervault "github.com/testcontainers/testcontainers-go/modules/vault"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/compile"
	"github.com/open-policy-agent/opa/v1/config"
	"github.com/open-policy-agent/opa/v1/ir"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

const (
	testpath = "/test"
	token    = "dev-only-token"
)

func createVaultTestCluster(t *testing.T, url string) (*testcontainervault.VaultContainer, *hcvault.Client) {
	t.Helper()

	opts := []testcontainers.ContainerCustomizer{
		testcontainervault.WithToken(token),
		testcontainervault.WithInitCommand("secrets enable -version=2 -path=kv kv"),
	}

	vault, err := testcontainervault.Run(context.Background(), "hashicorp/vault:1.15.4", opts...)
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
	if err := setKey(vlogical, "kv/data/tls/bearer:data/token", map[string]string{"url": url + testpath, "token": "token_good", "scheme": "Bearer", "content-type": "application/json"}); err != nil {
		t.Error(err)
	}
	if err := setKey(vlogical, "kv/data/kafka/sasl:data", map[string]string{"username": "USERNAME", "password": "password", "scram": "scram", "bits": "256"}); err != nil {
		t.Error(err)
	}
	if err := setKey(vlogical, "kv/data/http-dl/authn:data/bearer", map[string]string{"bearer": "token_good"}); err != nil {
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
	_, err = lookupKey(vlogical, "kv/data/tls/bearer:data/content-type")
	if err != nil {
		t.Error(err)
	}
	_, err = lookupKey(vlogical, "kv/data/kafka/sasl:data/username")
	if err != nil {
		t.Error(err)
	}
	_, err = lookupKey(vlogical, "kv/data/kafka/sasl:data/password")
	if err != nil {
		t.Error(err)
	}
	_, err = lookupKey(vlogical, "kv/data/http-dl/authn:data/bearer")
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
	err := os.WriteFile(file, []byte(buffer), 0o666)
	if err != nil {
		return fmt.Errorf("could not write %v: %w", file, err)
	}
	return nil
}

func TestEKM(t *testing.T) {
	t.Skip("this flakey test blocks all our work. skipping as a workaround. FIXME")

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

	t.Run("config replacements", func(t *testing.T) {
		t.Run("services", func(t *testing.T) {
			conf := config.Config{
				Extra: map[string]json.RawMessage{"ekm": []byte(`{"vault":
					{
					  "url": "` + address + `",
					  "access_type": "token",
					  "token_file": "token_file",
					  "services": {
					    "acmecorp.url": "kv/data/acmecorp:data/url",
					    "acmecorp.credentials.bearer.token": "kv/data/acmecorp/bearer:data/token"
					  }
					}}`)},
				Services: []byte(`{"acmecorp": {"credentials": {"bearer": {"token": "bear"} } } }`),
			}
			e := NewEKM()
			e.SetLogger(logging.New())
			cnf, err := e.OnConfig(context.Background(), &conf)
			if err != nil {
				t.Fatal(err)
			}

			compareConfigs(t, cnf.Services, `{"acmecorp":{"credentials":{"bearer":{"token":"token1"}},"url":"`+address+`"}}`)
		})

		t.Run("keys", func(t *testing.T) {
			conf := config.Config{
				Extra: map[string]json.RawMessage{"ekm": []byte(`{"vault":
					{
					  "url": "` + address + `",
					  "access_type": "token",
					  "token_file": "token_file",
					  "keys": {
					    "jwt_signing.key": "kv/data/sign:data/private_key"
					  }
					}}`)},
				Keys: []byte(`{"jwt_signing": {"key": "test"} }`),
			}
			e := NewEKM()
			e.SetLogger(logging.New())
			cnf, err := e.OnConfig(context.Background(), &conf)
			if err != nil {
				t.Fatal(err)
			}
			compareConfigs(t, cnf.Keys, `{"jwt_signing":{"key":"private_key1"}}`)
		})
	})

	t.Run("v2", func(t *testing.T) {
		t.Run("services", func(t *testing.T) {
			conf := config.Config{
				Extra: map[string]json.RawMessage{"ekm": []byte(`{"vault":
					{
					  "url": "` + address + `",
					  "access_type": "token",
					  "token_file": "token_file"
					}}`)},
				Services: []byte(`{"acmecorp": {"url": "${vault(kv/data/acmecorp:data/url)}", "credentials": {"bearer": {"token": "${vault(kv/data/acmecorp/bearer:data/token)}"} } } }`),
			}
			e := NewEKM()
			e.SetLogger(logging.New())
			cnf, err := e.OnConfig(context.Background(), &conf)
			if err != nil {
				t.Fatal(err)
			}

			compareConfigs(t, cnf.Services, `{"acmecorp":{"credentials":{"bearer":{"token":"token1"}},"url":"`+address+`"}}`)
		})

		t.Run("keys", func(t *testing.T) {
			conf := config.Config{
				Extra: map[string]json.RawMessage{"ekm": []byte(`{"vault":
					{
					  "url": "` + address + `",
					  "access_type": "token",
					  "token_file": "token_file"
					}}`)},
				Keys: []byte(`{"jwt_signing": {"key": "${vault(kv/data/sign:data/private_key)}"} }`),
			}
			e := NewEKM()
			e.SetLogger(logging.New())
			cnf, err := e.OnConfig(context.Background(), &conf)
			if err != nil {
				t.Fatal(err)
			}
			compareConfigs(t, cnf.Keys, `{"jwt_signing":{"key":"private_key1"}}`)
		})

		t.Run("plugins", func(t *testing.T) {
			conf := config.Config{
				Extra: map[string]json.RawMessage{"ekm": []byte(`{"vault":
					{
					  "url": "` + address + `",
					  "access_type": "token",
					  "token_file": "token_file"
					}}`)},
				// NOTE(sr): We're testing three things:
				// full replacement (sasl_password)
				// single substring replacement (sasl_username)
				// multiple substring replacement (sasl_mechanism) -- unlikely, but let's support it
				Plugins: map[string]json.RawMessage{
					"data": []byte(`{"kafka.messages": {
					  "urls": ["kafka.broker:9092"],
					  "sasl_mechanism": "${vault(kv/data/kafka/sasl:data/scram)}-sha-${vault(kv/data/kafka/sasl:data/bits)}",
					  "sasl_username": "${vault(kv/data/kafka/sasl:data/username)}@styra.com",
					  "sasl_password": "${vault(kv/data/kafka/sasl:data/password)}"
					 }}`),
					"eopa_dl": []byte(`{
						"outputs": [
							{
							  "type": "http",
							  "headers": {
							    "Authorization": "bearer ${vault(kv/data/http-dl/authn:data/bearer)}"
					          }
					        }
						]
					}`),
				},
			}
			e := NewEKM()
			e.SetLogger(logging.New())
			cnf, err := e.OnConfig(context.Background(), &conf)
			if err != nil {
				t.Fatal(err)
			}
			compareConfigs(t, cnf.Plugins["data"],
				`{"kafka.messages":{"sasl_mechanism":"scram-sha-256","sasl_password":"password","sasl_username":"USERNAME@styra.com","urls":["kafka.broker:9092"]}}`)
			compareConfigs(t, cnf.Plugins["eopa_dl"],
				`{"outputs": [{"type": "http", "headers": {"Authorization": "bearer token_good"}}]}`)
		})
	})

	t.Run("http.send", func(t *testing.T) {
		conf := config.Config{
			Extra: map[string]json.RawMessage{"ekm": []byte(`{"vault":
					{
					  "url": "` + address + `",
					  "access_type": "token",
					  "token_file": "token_file",
					  "httpsend": {
					    "https://www.acmecorp.com": {
					      "url": "kv/data/tls/bearer:data/url",
					      "headers": {
					        "Authorization": {
					          "scheme": "kv/data/tls/bearer:data/scheme",
					          "bearer": "kv/data/tls/bearer:data/token"
					        },
					        "Content-Type": "kv/data/tls/bearer:data/content-type"
					      }
					    }
					  }
					}}`)},
		}
		e := NewEKM()
		e.SetLogger(logging.New())
		_, err := e.OnConfig(context.Background(), &conf)
		if err != nil {
			t.Fatal(err)
		}

		const simpleRego = `package test
default allow := false
allow if {
  resp = http.send({"headers": {"Content-Type": "application/text"}, "method": "get", "url": "https://www.acmecorp.com", "raise_error": true})
  resp.raw_body == "Success"
}`

		bundle := createBundle(t, simpleRego)

		t.Run("success", func(t *testing.T) {
			const simpleQuery = "test/allow"
			const simpleResult = `{{"result": true}}`
			policy := setup(t, bundle, simpleQuery)
			testCompiler(t, policy, "{}", simpleQuery, simpleResult, bundle.Data)
		})

		// change the token
		if err := setKey(vlogical, "kv/data/tls/bearer:data/token", map[string]string{"url": srv.URL + testpath, "token": "token_bad", "scheme": "Bearer", "content-type": "application/json"}); err != nil {
			t.Error(err)
		}

		t.Run("badtoken", func(t *testing.T) {
			const simpleQuery = "test/allow"
			const simpleResult = `{{"result": false}}`
			policy := setup(t, bundle, simpleQuery)
			testCompiler(t, policy, "{}", simpleQuery, simpleResult, bundle.Data)
		})
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

func compareConfigs(t *testing.T, act json.RawMessage, exp string) {
	t.Helper()
	var a0, e0 map[string]any
	if err := json.Unmarshal(act, &a0); err != nil {
		t.Fatalf("unmarshal 'act': %v", err)
	}
	if err := json.Unmarshal([]byte(exp), &e0); err != nil {
		t.Fatalf("unmarshal 'exp': %v", err)
	}
	if diff := cmp.Diff(e0, a0); diff != "" {
		t.Errorf("unexpected diff: (-want, +got):\n%s", diff)
	}
}
