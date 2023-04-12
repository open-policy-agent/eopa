package decisionlogs

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage/inmem"
)

func isConfig(exp Config) func(testing.TB, any, error) {
	opt := []cmp.Option{cmpopts.IgnoreFields(Config{}, "Buffer", "Output"), cmp.AllowUnexported(Config{})}
	diff := func(x, y any) string {
		return cmp.Diff(x, y, opt...)
	}
	if exp.DropDecision == "" {
		exp.DropDecision = "/system/log/drop"
	}
	if exp.MaskDecision == "" {
		exp.MaskDecision = "/system/log/mask"
	}
	return func(tb testing.TB, c any, err error) {
		tb.Helper()
		if err != nil {
			tb.Fatalf("unexpected error: %v", err)
		}
		act := c.(Config)
		if d := diff(exp, act); d != "" {
			tb.Errorf("Config mismatch (-want +got):\n%s", d)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		note   string
		config string
		mgr    string
		checks func(testing.TB, any, error)
	}{
		{
			note: "default (memory buffer)",
			config: `output:
  type: console`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputConsole: &outputConsoleOpts{},
			}),
		},
		{
			note: "overridden decisions",
			config: `
drop_decision: /foo/bar/drop
mask_decision: /foo/bar/mask
output:
  type: console`,
			checks: isConfig(Config{
				MaskDecision: "/foo/bar/mask",
				DropDecision: "/foo/bar/drop",
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputConsole: &outputConsoleOpts{},
			}),
		},
		{
			note: "memory buffer",
			config: `
buffer:
  type: memory
  max_bytes: 120
  flush_at_count: 100
  flush_at_period: 10s
  flush_at_bytes: 12
output:
  type: console
 `,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					FlushAtCount:  100,
					FlushAtBytes:  12,
					FlushAtPeriod: "10s",
					MaxBytes:      120,
				},
				outputConsole: &outputConsoleOpts{},
			}),
		},
		{
			note: "disk buffer",
			config: `
buffer:
  type: disk
  path: /what/ev/er.sqlite
output:
  type: console
 `,
			checks: isConfig(Config{
				diskBuffer: &diskBufferOpts{
					Path: "/what/ev/er.sqlite",
				},
				outputConsole: &outputConsoleOpts{},
			}),
		},
		{
			note: "unbuffered",
			config: `
buffer:
  type: unbuffered
output:
  type: console
`,
			checks: isConfig(Config{
				outputConsole: &outputConsoleOpts{},
			}),
		},
		{
			note: "invalid buffer",
			config: `
buffer:
  type: flash drive
output:
  type: console
 `,
			checks: func(tb testing.TB, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "unknown buffer type: \"flash drive\"", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "invalid output",
			config: `
output:
  type: flash drive
 `,
			checks: func(tb testing.TB, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "unknown output type: \"flash drive\"", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "console output",
			config: `
output:
  type: console`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputConsole: &outputConsoleOpts{},
			}),
		},
		{
			note: "service output, unknown service",
			config: `
output:
  type: service
  service: someservice`,
			checks: func(tb testing.TB, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "unknown service \"someservice\"", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "service output",
			mgr: `services:
- name: knownservice
  url: "http://knownservice/prefix"
  response_header_timeout_seconds: 12
`,
			config: `
output:
  type: service
  service: knownservice
  resource: decisionlogs`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputHTTP: &outputHTTPOpts{
					URL:      "http://knownservice/prefix/decisionlogs",
					Timeout:  "12s",
					Array:    true,
					Compress: true,
				},
			}),
		},
		{
			note: "service output, default resource+timeout",
			mgr: `services:
- name: knownservice
  url: "http://knownservice/prefix"
`,
			config: `
output:
  type: service
  service: knownservice`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputHTTP: &outputHTTPOpts{
					URL:      "http://knownservice/prefix/logs",
					Timeout:  "10s",
					Array:    true,
					Compress: true,
				},
			}),
		},
		{
			note: "service output with headers",
			mgr: `services:
- name: knownservice
  url: "http://knownservice/prefix"
  headers:
    content-type: "application/foobear"
    x-token: "y"
`,
			config: `
output:
  type: service
  service: knownservice
  resource: decisionlogs`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputHTTP: &outputHTTPOpts{
					URL:     "http://knownservice/prefix/decisionlogs",
					Timeout: "10s",
					Headers: map[string]string{
						"content-type": "application/foobear",
						"x-token":      "y",
					},
					Array:    true,
					Compress: true,
				},
			}),
		},
		{
			note: "service output with oauth2",
			mgr: `services:
- name: knownservice
  url: "http://knownservice/prefix"
  credentials:
    oauth2:
      token_url: "https://otherservice.com"
      client_id: mememe
      client_secret: sesamememe
      scopes:
      - foo
      - bar
`,
			config: `
output:
  type: service
  service: knownservice
  resource: decisionlogs`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputHTTP: &outputHTTPOpts{
					URL:      "http://knownservice/prefix/decisionlogs",
					Timeout:  "10s",
					Array:    true,
					Compress: true,
					OAuth2: &httpAuthOAuth2{
						Enabled:      true,
						TokenURL:     "https://otherservice.com",
						ClientKey:    "mememe",
						ClientSecret: "sesamememe",
						Scopes:       []string{"foo", "bar"},
					},
				},
			}),
		},
		{
			note: "service output with tls",
			mgr: `services:
- name: knownservice
  url: "http://knownservice/prefix"
  tls:
    ca_cert: testdata/ca.pem
  credentials:
    client_tls:
      cert: testdata/cert.pem
      private_key: testdata/key.pem
`,
			config: `
output:
  type: service
  service: knownservice
  resource: decisionlogs`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputHTTP: &outputHTTPOpts{
					URL:      "http://knownservice/prefix/decisionlogs",
					Timeout:  "10s",
					Array:    true,
					Compress: true,
					TLS: &httpAuthTLS{
						Enabled: true,
						Certificates: []certs{
							{Key: "key\n", Cert: "cert\n"},
						},
						RootCAs: "ca\n",
					},
				},
			}),
		},
		{
			note: "http output",
			config: `
output:
  type: http
  url: https://logs.logs.logs:1234/post`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputHTTP: &outputHTTPOpts{URL: "https://logs.logs.logs:1234/post"},
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			mgr := getTestManager(tc.mgr)
			config, err := Factory().Validate(mgr, []byte(tc.config))
			if tc.checks != nil {
				tc.checks(t, config, err)
			}
		})
	}
}

func getTestManager(mgr string) *plugins.Manager {
	store := inmem.New()
	manager, err := plugins.New([]byte(mgr), "test-instance-id", store)
	if err != nil {
		panic(err)
	}
	return manager
}
