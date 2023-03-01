package data_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	goldap "github.com/go-ldap/ldap/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/load-private/pkg/plugins/data"
	"github.com/styrainc/load-private/pkg/plugins/data/git"
	"github.com/styrainc/load-private/pkg/plugins/data/http"
	"github.com/styrainc/load-private/pkg/plugins/data/kafka"
	"github.com/styrainc/load-private/pkg/plugins/data/ldap"
	"github.com/styrainc/load-private/pkg/plugins/data/okta"
	"github.com/styrainc/load-private/pkg/plugins/data/s3"
	inmem "github.com/styrainc/load-private/pkg/store"
)

func isConfig[T any](tb testing.TB, pluginType string, path string, exp T) func(testing.TB, any, error) {
	opt := cmpopts.IgnoreUnexported(exp)
	diff := func(x, y any) string {
		return cmp.Diff(x, y, opt)
	}
	raw, err := json.Marshal(exp)
	if err != nil {
		tb.Fatal(err)
	}
	var tmp map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tmp); err != nil {
		tb.Fatal(err)
	}
	tmp["type"] = json.RawMessage(`"` + pluginType + `"`)
	tmp["path"] = json.RawMessage(`"` + path + `"`)
	raw, err = json.Marshal(map[string]any{
		path: tmp,
	})
	if err != nil {
		tb.Fatal(err)
	}
	c, err := data.Factory().Validate(getTestManager(), raw)
	if err != nil {
		tb.Fatal(err)
	}
	cfg := c.(data.Config)
	k, ok := cfg.DataPlugins[path]
	if !ok {
		tb.Fatalf("expected config under %q", path)
	}
	validated, ok := k.Config.(T)
	if !ok {
		tb.Fatalf("expected %T, got %T", exp, validated)
	}
	exp = validated

	return func(tb testing.TB, c any, err error) {
		if err != nil {
			tb.Fatalf("unexpected error: %v", err)
		}
		cfg := c.(data.Config)
		k, ok := cfg.DataPlugins[path]
		if !ok {
			tb.Fatalf("expected config under %q", path)
		}
		act, ok := k.Config.(T)
		if !ok {
			tb.Fatalf("expected %T, got %T", act, k)
		}
		if d := diff(exp, act); d != "" {
			tb.Errorf("Config mismatch (-want +got):\n%s", d)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		note   string
		config string
		checks func(testing.TB, any, error)
	}{
		{
			note: "one kafka",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
`,
			checks: isConfig(t, kafka.Name, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
			}),
		},
		{
			note: "one kafka, tls",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  tls_client_cert: kafka/testdata/tls/client-cert.pem
  tls_client_private_key: kafka/testdata/tls/client-key.pem
  tls_ca_cert: kafka/testdata/tls/ca.pem
`,
			checks: isConfig(t, kafka.Name, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				Cert:              "kafka/testdata/tls/client-cert.pem",
				PrivateKey:        "kafka/testdata/tls/client-key.pem",
				CACert:            "kafka/testdata/tls/ca.pem",
			}),
		},
		{
			note: "one kafka, sasl/plain",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  sasl_mechanism: PLAIN
  sasl_username: alice
  sasl_password: password
`,
			checks: isConfig(t, kafka.Name, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				SASLMechanism:     "PLAIN",
				SASLUsername:      "alice",
				SASLPassword:      "password",
			}),
		},
		{
			note: "one kafka, sasl/scram-sha-512",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  sasl_mechanism: SCRAM-SHA-512
  sasl_username: alice
  sasl_password: password
  sasl_token: true
`,
			checks: isConfig(t, kafka.Name, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				SASLMechanism:     "SCRAM-SHA-512",
				SASLUsername:      "alice",
				SASLPassword:      "password",
				SASLToken:         true,
			}),
		},
		{
			note: "one kafka, sasl/scram-sha-256",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  sasl_mechanism: SCRAM-SHA-256
  sasl_username: alice
  sasl_password: password
  sasl_token: true
`,
			checks: isConfig(t, kafka.Name, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				SASLMechanism:     "SCRAM-SHA-256",
				SASLUsername:      "alice",
				SASLPassword:      "password",
				SASLToken:         true,
			}),
		},
		{
			note: "one kafka, tls+sasl/scram-sha-256, lowercase",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  tls_client_cert: kafka/testdata/tls/client-cert.pem
  tls_client_private_key: kafka/testdata/tls/client-key.pem
  tls_ca_cert: kafka/testdata/tls/ca.pem
  sasl_mechanism: scram-sha-256
  sasl_username: alice
  sasl_password: password
  sasl_token: true
`,
			checks: isConfig(t, kafka.Name, "kafka.updates", kafka.Config{
				Topics:            []string{"updates"},
				URLs:              []string{"127.0.0.1:8083"},
				RegoTransformRule: "data.utils.transform_events",
				Cert:              "kafka/testdata/tls/client-cert.pem",
				PrivateKey:        "kafka/testdata/tls/client-key.pem",
				CACert:            "kafka/testdata/tls/ca.pem",
				SASLMechanism:     "scram-sha-256",
				SASLUsername:      "alice",
				SASLPassword:      "password",
				SASLToken:         true,
			}),
		},
		{
			note: "two kafka",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
kafka.downdates:
  type: kafka
  urls:
  - some.other:8083
  topics:
  - downdates.huh
  rego_transform: data.utils.transform_events
`,
			checks: func(tb testing.TB, c any, err error) {
				isConfig(tb, kafka.Name, "kafka.updates", kafka.Config{
					Topics:            []string{"updates"},
					URLs:              []string{"127.0.0.1:8083"},
					RegoTransformRule: "data.utils.transform_events",
				})(tb, c, err)
				isConfig(tb, kafka.Name, "kafka.downdates", kafka.Config{
					Topics:            []string{"downdates.huh"},
					URLs:              []string{"some.other:8083"},
					RegoTransformRule: "data.utils.transform_events",
				})(tb, c, err)
			},
		},
		{
			note: "bad path",
			config: `
"kafka.updates":
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
"kafka.updates.test":
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
`,
			checks: func(tb testing.TB, c any, err error) {
				var poe *data.PathOverlapError
				if errors.As(err, &poe) {
					return
				}
				if err == nil {
					err = errors.New("nil")
				}

				exp := data.NewPathOverlapError(ast.MustParseRef("kafka.updates"), ast.MustParseRef("kafka.updates.test"))
				tb.Fatalf("expected error %q, got %q", exp.Error(), err.Error())
			},
		},
		{
			note: "kafka, no urls",
			config: `
kafka.updates:
  type: kafka
  topics:
  - updates
`,
			checks: func(tb testing.TB, _ any, err error) {
				if exp, act := "data plugin kafka (kafka.updates): need at least one broker URL", err.Error(); exp != act {
					tb.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "kafka, no topics",
			config: `
kafka.updates:
  type: kafka
  urls: ["127.0.0.1:9092"]
  topics:
`,
			checks: func(tb testing.TB, _ any, err error) {
				if exp, act := "data plugin kafka (kafka.updates): need at least one topic", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "kafka, sasl, unknown mechanism",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
  sasl_mechanism: SHALALALA
  sasl_username: alice
  sasl_password: password
  sasl_token: true
`,
			checks: func(tb testing.TB, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "data plugin kafka (kafka.updates): unknown SASL mechanism", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "http simple",
			config: `
http.placeholder:
  type: http
  url: https://example.com
`,
			checks: isConfig(t, http.Name, "http.placeholder", http.Config{
				URL: "https://example.com",
			}),
		},
		{
			note: "http full",
			config: `
http.placeholder:
  type: http
  url: https://example.com
  method: foo
  body: >
    plain
    body
  file: path/to/foo.txt
  headers:
    Authorization: Bearer Foo
    Custom:
      - Foo
      - Bar
  timeout: 5s
  polling_interval: 1m
  tls_skip_verification: true
  tls_client_cert: http/testdata/client-cert.pem
  tls_client_private_key: http/testdata/client-key.pem
  tls_ca_cert: http/testdata/ca.pem
`,
			checks: isConfig(t, http.Name, "http.placeholder", http.Config{
				URL:    "https://example.com",
				Method: "foo",
				Body:   "plain body\n",
				File:   "path/to/foo.txt",
				Headers: map[string]any{
					"Authorization": "Bearer Foo",
					"Custom":        []any{"Foo", "Bar"},
				},
				Timeout:          "5s",
				Interval:         "1m",
				SkipVerification: true,
				Cert:             "http/testdata/client-cert.pem",
				CACert:           "http/testdata/ca.pem",
				PrivateKey:       "http/testdata/client-key.pem",
			}),
		},
		{
			note: "http follow redirects true",
			config: `
http.placeholder:
  type: http
  url: https://example.com
  follow_redirects: true
`,
			checks: func(tb testing.TB, a any, err error) {
				isConfig(tb, http.Name, "http.placeholder", http.Config{
					URL: "https://example.com",
				})(tb, a, err)
				t := true
				isConfig(tb, http.Name, "http.placeholder", http.Config{
					URL:             "https://example.com",
					FollowRedirects: &t,
				})(tb, a, err)
			},
		},
		{
			note: "http follow redirects empty",
			config: `
http.placeholder:
  type: http
  url: https://example.com
`,
			checks: func(tb testing.TB, a any, err error) {
				isConfig(tb, http.Name, "http.placeholder", http.Config{
					URL: "https://example.com",
				})(tb, a, err)
				t := true
				isConfig(tb, http.Name, "http.placeholder", http.Config{
					URL:             "https://example.com",
					FollowRedirects: &t,
				})(tb, a, err)
			},
		},
		{
			note: "http follow redirects false",
			config: `
http.placeholder:
  type: http
  url: https://example.com
  follow_redirects: false
`,
			checks: func(tb testing.TB, a any, err error) {
				f := false
				isConfig(tb, http.Name, "http.placeholder", http.Config{
					URL:             "https://example.com",
					FollowRedirects: &f,
				})(tb, a, err)
			},
		},
		{
			note: "okta full",
			config: `
okta.placeholder:
  type: okta
  url: https://example.com
  client_id: foo
  client_secret: bar
  token: xyz
  private_key: okta/testdata/private_key_new.txt
  private_key_id: buzz
  users: true
  groups: true
  roles: true
  apps: true
  polling_interval: 1m
`,
			checks: isConfig(t, okta.Name, "okta.placeholder", okta.Config{
				URL:          "https://example.com",
				ClientID:     "foo",
				ClientSecret: "bar",
				Token:        "xyz",
				PrivateKey:   "okta/testdata/private_key_new.txt",
				PrivateKeyID: "buzz",
				Users:        true,
				Groups:       true,
				Roles:        true,
				Apps:         true,
				Interval:     "1m",
			}),
		},
		{
			note: "ldap full",
			config: `
ldap.placeholder:
  type: ldap
  urls:
    - https://example.com
    - https://example2.com
  username: foo
  password: bar
  base_dn: dn=example,dn=com
  filter: "(objectclass=*)"
  scope: whole-subtree
  deref: never
  attributes:
    - foo
    - bar
  polling_interval: 1m
  tls_skip_verification: true
  tls_client_cert: ldap/testdata/client-cert.pem
  tls_client_private_key: ldap/testdata/client-key.pem
  tls_ca_cert: ldap/testdata/ca.pem
`,
			checks: isConfig(t, ldap.Name, "ldap.placeholder", ldap.Config{
				URLs: []string{
					"https://example.com",
					"https://example2.com",
				},
				Username:         "foo",
				Password:         "bar",
				BaseDN:           "dn=example,dn=com",
				Filter:           "(objectclass=*)",
				Scope:            "whole-subtree",
				Deref:            "never",
				Attributes:       []string{"foo", "bar"},
				Interval:         "1m",
				SkipVerification: true,
				Cert:             "ldap/testdata/client-cert.pem",
				CACert:           "ldap/testdata/ca.pem",
				PrivateKey:       "ldap/testdata/client-key.pem",
			}),
		},
		{
			note: "git basic auth",
			config: `
git.placeholder:
  type: git
  url: https://git.example.com
  file_path: data.json
  username: git
  password: secret
  polling_interval: 1m
`,
			checks: isConfig(t, git.Name, "git.placeholder", git.Config{
				URL:      "https://git.example.com",
				FilePath: "data.json",
				Username: "git",
				Password: "secret",
				Interval: "1m",
			}),
		},
		{
			note: "git token auth",
			config: `
git.placeholder:
  type: git
  url: https://git.example.com
  file_path: data.json
  token: secret
  polling_interval: 1m
`,
			checks: isConfig(t, git.Name, "git.placeholder", git.Config{
				URL:      "https://git.example.com",
				FilePath: "data.json",
				Token:    "secret",
				Interval: "1m",
			}),
		},
		{
			note: "git private key as file without passphrase",
			config: `
git.placeholder:
  type: git
  url: https://git.example.com
  file_path: data.json
  private_key: git/testdata/no-passphrase
  polling_interval: 1m
`,
			checks: isConfig(t, git.Name, "git.placeholder", git.Config{
				URL:        "https://git.example.com",
				FilePath:   "data.json",
				PrivateKey: "git/testdata/no-passphrase",
				Interval:   "1m",
			}),
		},
		{
			note: "git private key as file with passphrase",
			config: `
git.placeholder:
  type: git
  url: https://git.example.com
  file_path: data.json
  private_key: git/testdata/foo-passphrase
  passphrase: foo
  polling_interval: 1m
`,
			checks: isConfig(t, git.Name, "git.placeholder", git.Config{
				URL:        "https://git.example.com",
				FilePath:   "data.json",
				PrivateKey: "git/testdata/foo-passphrase",
				Passphrase: "foo",
				Interval:   "1m",
			}),
		},
		{
			note: "s3 scheme",
			config: `
s3.placeholder:
  type: s3
  url: s3://bucket/path
  access_id: foo
  secret: bar
  polling_interval: 1m
`,
			checks: isConfig(t, s3.Name, "s3.placeholder", s3.Config{
				URL:      "s3://bucket/path",
				AccessID: "foo",
				Secret:   "bar",
				Interval: "1m",
				Region:   "us-east-1",
			}),
		},
		{
			note: "gs scheme",
			config: `
s3.placeholder:
  type: s3
  url: gs://bucket/path
  access_id: foo
  secret: bar
  polling_interval: 1m
`,
			checks: isConfig(t, s3.Name, "s3.placeholder", s3.Config{
				URL:      "gs://bucket/path",
				AccessID: "foo",
				Secret:   "bar",
				Interval: "1m",
				Region:   "auto",
			}),
		},
		{
			note: "no scheme equals to s3",
			config: `
s3.placeholder:
  type: s3
  url: bucket/path
  access_id: foo
  secret: bar
  polling_interval: 1m
`,
			checks: isConfig(t, s3.Name, "s3.placeholder", s3.Config{
				URL:      "s3://bucket/path",
				AccessID: "foo",
				Secret:   "bar",
				Interval: "1m",
			}),
		},
		{
			note: "bucket only",
			config: `
s3.placeholder:
  type: s3
  url: bucket
  access_id: foo
  secret: bar
  polling_interval: 1m
`,
			checks: isConfig(t, s3.Name, "s3.placeholder", s3.Config{
				URL:      "s3://bucket",
				AccessID: "foo",
				Secret:   "bar",
				Interval: "1m",
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			mgr := getTestManager()
			config, err := data.Factory().Validate(mgr, []byte(tc.config))
			if tc.checks != nil {
				tc.checks(t, config, err)
			}
		})
	}
}

func TestStop(t *testing.T) {
	// change the default timeout of ldap to speed up the tests, since the real connection is not required
	origDefaultTimeout := goldap.DefaultTimeout
	t.Cleanup(func() {
		goldap.DefaultTimeout = origDefaultTimeout
	})
	goldap.DefaultTimeout = 100 * time.Millisecond

	for _, tt := range []struct {
		name   string
		config string
	}{
		{
			name: "kafka",
			config: `
kafka.updates:
  type: kafka
  urls:
  - 127.0.0.1:8083
  topics:
  - updates
  rego_transform: data.utils.transform_events
`,
		},
		{
			name: "http",
			config: `
http.test:
  type: http
  url: https://www.example.com
`,
		},
		{
			name: "okta",
			config: `
okta.test:
  type: okta
  url: https://example.com
  client_id: test
  client_secret: secret
  users: true
`,
		},
		{
			name: "ldap",
			config: `
ldap.test:
  type: ldap
  urls:
    - ldaps://example.com
  base_dn: "dc=example,dc=com"
  filter: "(objectclass=*)"
`,
		},
		{
			name: "git",
			config: `
git.test:
  type: git
  url: https://git.example.com
`,
		},
		{
			name: "s3",
			config: `
s3.test:
  type: s3
  url: s3://test-bucket/test-file
  access_id: foo
  secret: bar
`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("github.com/patrickmn/go-cache.(*janitor).Run"))

			mgr := getTestManager()
			c, err := data.Factory().Validate(mgr, []byte(tt.config))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			dp := data.Factory().New(mgr, c)
			ctx := context.Background()
			if err := dp.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}

			// NOTE(sr): The more time we give the go routines to actually start,
			// the less flaky this test will be, if there are leaked routines.
			time.Sleep(200 * time.Millisecond)
			dp.Stop(ctx)

			// goleak will assert that no goroutine is still running
		})
	}
}

func getTestManager() *plugins.Manager {
	return getTestManagerWithOpts(nil)
}

func getTestManagerWithOpts(config []byte, stores ...storage.Store) *plugins.Manager {
	store := inmem.New()
	if len(stores) == 1 {
		store = stores[0]
	}

	manager, err := plugins.New(config, "test-instance-id", store)
	if err != nil {
		panic(err)
	}
	return manager
}
