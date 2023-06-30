package decisionlogs

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage/inmem"
)

func isConfig(exp Config) func(testing.TB, any, error) {
	opt := []cmp.Option{
		cmpopts.IgnoreFields(Config{}, "Buffer", "Output"),
		cmp.AllowUnexported(Config{}, outputKafkaOpts{}, outputSplunkOpts{}, outputS3Opts{}, batchOpts{}),
	}
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

func isBenthos(exp map[string]any) func(testing.TB, any, error) {
	return func(tb testing.TB, c any, err error) {
		tb.Helper()
		if err != nil {
			tb.Fatalf("unexpected error: %v", err)
		}
		act := c.(Config).outputs[0].Benthos()
		if d := cmp.Diff(exp, act); d != "" {
			tb.Errorf("Benthos mismatch (-want +got):\n%s", d)
		}
	}
}

func withResources(exp map[string]any) func(testing.TB, any, error) {
	return func(tb testing.TB, c any, err error) {
		tb.Helper()
		if err != nil {
			tb.Fatalf("unexpected error: %v", err)
		}
		act := c.(Config).outputs[0].(additionalResources).Resources()
		if d := cmp.Diff(exp, act); d != "" {
			tb.Errorf("Resources mismatch (-want +got):\n%s", d)
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
				outputs: []output{&outputConsoleOpts{}},
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
				outputs: []output{&outputConsoleOpts{}},
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
				outputs: []output{&outputConsoleOpts{}},
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
				outputs: []output{&outputConsoleOpts{}},
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
				outputs: []output{&outputConsoleOpts{}},
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
			checks: func(t testing.TB, _ any, err error) {
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
			checks: func(t testing.TB, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "unknown output type: \"flash drive\"", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
		{
			note: "no output",
			config: `
output: []
 `,
			checks: func(t testing.TB, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "at least one output required", err.Error(); exp != act {
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
				outputs: []output{&outputConsoleOpts{}},
			}),
		},
		{
			note: "service output, unknown service",
			config: `
output:
  type: service
  service: someservice`,
			checks: func(t testing.TB, _ any, err error) {
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
				outputs: []output{&outputHTTPOpts{
					URL:     "http://knownservice/prefix/decisionlogs",
					Timeout: "12s",
					Batching: &batchOpts{
						Period:   "10ms",
						Array:    true,
						Compress: true,
					},
				}},
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
				outputs: []output{&outputHTTPOpts{
					URL:     "http://knownservice/prefix/logs",
					Timeout: "10s",
					Batching: &batchOpts{
						Period:   "10ms",
						Array:    true,
						Compress: true,
					},
				}},
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
				outputs: []output{&outputHTTPOpts{
					URL:     "http://knownservice/prefix/decisionlogs",
					Timeout: "10s",
					Headers: map[string]string{
						"content-type": "application/foobear",
						"x-token":      "y",
					},
					Batching: &batchOpts{
						Period:   "10ms",
						Array:    true,
						Compress: true,
					},
				}},
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
				outputs: []output{&outputHTTPOpts{
					URL:     "http://knownservice/prefix/decisionlogs",
					Timeout: "10s",
					Batching: &batchOpts{
						Period:   "10ms",
						Array:    true,
						Compress: true,
					},
					OAuth2: &httpAuthOAuth2{
						Enabled:      true,
						TokenURL:     "https://otherservice.com",
						ClientKey:    "mememe",
						ClientSecret: "sesamememe",
						Scopes:       []string{"foo", "bar"},
					},
				}},
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
				outputs: []output{&outputHTTPOpts{
					URL:     "http://knownservice/prefix/decisionlogs",
					Timeout: "10s",
					Batching: &batchOpts{
						Period:   "10ms",
						Array:    true,
						Compress: true,
					},
					TLS: &sinkAuthTLS{
						Enabled: true,
						Certificates: []certs{
							{Key: "key\n", Cert: "cert\n"},
						},
						RootCAs: "ca\n",
					},
				}},
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
				outputs: []output{&outputHTTPOpts{URL: "https://logs.logs.logs:1234/post"}},
			}),
		},
		{
			note: "http retries",
			config: `
output:
  type: http
  url: https://logs.logs.logs:1234/post
  retry:
    period: 5s
    max_attempts: 10
    max_backoff: 600s
    backoff_on:
      - 400
      - 429
    drop_on:
      - 300
    successful_on:
      - 202`,
			checks: func(t testing.TB, a any, err error) {
				isConfig(Config{
					memoryBuffer: &memBufferOpts{
						MaxBytes: defaultMemoryMaxBytes,
					},
					outputs: []output{&outputHTTPOpts{
						URL: "https://logs.logs.logs:1234/post",
						Retry: &retryOpts{
							Period:       "5s",
							MaxAttempts:  10,
							MaxBackoff:   "600s",
							BackoffOn:    []int{400, 429},
							DropOn:       []int{300},
							SuccessfulOn: []int{202},
						},
					}},
				})(t, a, err)

				isBenthos(map[string]any{
					"drop_on": map[string]any{
						"error": true,
						"output": map[string]any{
							"http_client": map[string]any{
								"url":               "https://logs.logs.logs:1234/post",
								"retries":           10,
								"retry_period":      "5s",
								"max_retry_backoff": "600s",
								"backoff_on":        []int{400, 429},
								"drop_on":           []int{300},
								"successful_on":     []int{202},
							},
						},
					},
				})(t, a, err)
			},
		},
		{
			note: "http rate limit",
			config: `
output:
  type: http
  url: https://logs.logs.logs:1234/post
  rate_limit:
    count: 200
    interval: 1s`,
			checks: func(t testing.TB, a any, err error) {
				isConfig(Config{
					memoryBuffer: &memBufferOpts{
						MaxBytes: defaultMemoryMaxBytes,
					},
					outputs: []output{
						&outputHTTPOpts{
							URL: "https://logs.logs.logs:1234/post",
							RateLimit: &rateLimitOpts{
								Interval: "1s",
								Count:    200,
							},
						},
					},
				})(t, a, err)

				isBenthos(map[string]any{
					"drop_on": map[string]any{
						"error": true,
						"output": map[string]any{
							"http_client": map[string]any{
								"url":        "https://logs.logs.logs:1234/post",
								"rate_limit": ResourceKey([]byte("https://logs.logs.logs:1234/post")),
							},
						},
					},
				})(t, a, err)

				withResources(map[string]any{
					"rate_limit_resources": []map[string]any{
						{
							"label": ResourceKey([]byte("https://logs.logs.logs:1234/post")),
							"local": map[string]any{
								"count":    200,
								"interval": "1s",
							},
						},
					},
				})

			},
		},
		{
			note: "kafka output",
			config: `
output:
  type: kafka
  topic: logs
  timeout: 30s
  urls:
  - 127.0.0.1:9091`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputs: []output{&outputKafkaOpts{
					URLs:    []string{"127.0.0.1:9091"},
					Topic:   "logs",
					Timeout: "30s",
				}},
			}),
		},
		{
			note: "http+kafka+console output",
			config: `
output:
- type: http
  url: https://logs.logs.logs:1234/post
- type: kafka
  topic: logs
  timeout: 30s
  urls:
  - 127.0.0.1:9091
- type: console
`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputs: []output{
					&outputHTTPOpts{URL: "https://logs.logs.logs:1234/post"},
					&outputKafkaOpts{
						URLs:    []string{"127.0.0.1:9091"},
						Topic:   "logs",
						Timeout: "30s",
					},
					&outputConsoleOpts{},
				},
			}),
		},
		{
			note: "kafka output with TLS",
			config: `
output:
  type: kafka
  topic: logs
  urls:
  - 127.0.0.1:9091
  tls:
    cert: testdata/cert.pem
    private_key: testdata/key.pem
    ca_cert: testdata/ca.pem`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputs: []output{&outputKafkaOpts{
					URLs:  []string{"127.0.0.1:9091"},
					Topic: "logs",
					tls: &sinkAuthTLS{
						Enabled: true,
						Certificates: []certs{
							{Key: "key\n", Cert: "cert\n"},
						},
						RootCAs: "ca\n",
					},
				}},
			}),
		},
		{
			note: "kafka output with SASL",
			config: `
output:
  type: kafka
  topic: logs
  urls:
  - 127.0.0.1:9091
  sasl:
    - mechanism: scram-sha-512
      username: open
      password: sesame
    - mechanism: SCRAM-SHA-256
      username: open
      password: sesame
    - mechanism: PLAIN
      username: open
      password: sesame`,
			checks: func(t testing.TB, a any, err error) {
				isConfig(Config{
					memoryBuffer: &memBufferOpts{
						MaxBytes: defaultMemoryMaxBytes,
					},
					outputs: []output{&outputKafkaOpts{
						URLs:  []string{"127.0.0.1:9091"},
						Topic: "logs",
						SASL: []saslOpts{
							{
								Mechanism: "scram-sha-512",
								Username:  "open",
								Password:  "sesame",
							},
							{
								Mechanism: "SCRAM-SHA-256",
								Username:  "open",
								Password:  "sesame",
							},
							{
								Mechanism: "PLAIN",
								Username:  "open",
								Password:  "sesame",
							},
						},
					}},
				})(t, a, err)

				isBenthos(map[string]any{
					"kafka_franz": map[string]any{
						"seed_brokers": []string{"127.0.0.1:9091"},
						"topic":        "logs",
						"sasl": []map[string]any{
							{
								"mechanism": "SCRAM-SHA-512",
								"username":  "open",
								"password":  "sesame",
							},
							{
								"mechanism": "SCRAM-SHA-256",
								"username":  "open",
								"password":  "sesame",
							},
							{
								"mechanism": "PLAIN",
								"username":  "open",
								"password":  "sesame",
							},
						},
					},
				})(t, a, err)
			},
		},
		{
			note: "kafka output with batching",
			config: `
output:
  type: kafka
  topic: logs
  urls:
  - 127.0.0.1:9091
  batching:
    at_count: 42
    at_bytes: 43
    at_period: "300ms"
    array: true
    compress: true
`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputs: []output{&outputKafkaOpts{
					URLs:  []string{"127.0.0.1:9091"},
					Topic: "logs",
					Batching: &batchOpts{
						Period:   "300ms",
						Count:    42,
						Bytes:    43,
						Array:    true,
						Compress: true,
					},
				}},
			}),
		},
		{
			note: "kafka output with TLS skipped",
			config: `
output:
  type: kafka
  topic: logs
  urls:
  - 127.0.0.1:9091
  tls:
    skip_cert_verify: true
`,
			checks: isConfig(Config{
				memoryBuffer: &memBufferOpts{
					MaxBytes: defaultMemoryMaxBytes,
				},
				outputs: []output{&outputKafkaOpts{
					URLs:  []string{"127.0.0.1:9091"},
					Topic: "logs",
					tls: &sinkAuthTLS{
						Enabled:    true,
						SkipVerify: true,
					},
				}},
			}),
		},
		{
			note: "splunk output with tls+batching+gzip",
			config: `
output:
  type: splunk
  url: "https://http-input.foobar.hec.splunk.com/services/collector/event"
  token: opensplunkame
  batching:
    at_count: 42
    at_bytes: 43
    at_period: "300ms"
    array: true # overridden for splunk
    compress: true
  tls:
      cert: testdata/cert.pem
      private_key: testdata/key.pem
      ca_cert: testdata/ca.pem
      skip_cert_verify: true
`,
			checks: func(t testing.TB, a any, err error) {
				isConfig(Config{
					memoryBuffer: &memBufferOpts{
						MaxBytes: defaultMemoryMaxBytes,
					},
					outputs: []output{&outputSplunkOpts{
						URL:   "https://http-input.foobar.hec.splunk.com/services/collector/event",
						Token: "opensplunkame",
						Batching: &batchOpts{
							Period:   "300ms",
							Count:    42,
							Bytes:    43,
							Array:    false, // NB: overridden
							Compress: true,
						},
						tls: &sinkAuthTLS{
							Enabled:    true,
							SkipVerify: true,
							Certificates: []certs{
								{Key: "key\n", Cert: "cert\n"},
							},
							RootCAs: "ca\n",
						},
					}},
				})(t, a, err)

				isBenthos(map[string]any{
					"http_client": map[string]any{
						"batching": map[string]any{
							"byte_size": 43,
							"count":     42,
							"period":    "300ms",
							"processors": []map[string]any{
								{"archive": map[string]any{"format": "lines"}},
								{"compress": map[string]any{"algorithm": "gzip"}},
							},
						},
						"headers": map[string]any{
							"Authorization":    "Splunk opensplunkame",
							"Content-Encoding": "gzip",
							"Content-Type":     "application/json",
						},
						"tls": map[string]any{
							"client_certs":     []map[string]any{{"cert": "cert\n", "key": "key\n"}},
							"enabled":          true,
							"skip_cert_verify": true,
							"root_cas":         "ca\n",
						},
						"url":  "https://http-input.foobar.hec.splunk.com/services/collector/event",
						"verb": "POST",
					},
				})(t, a, err)
			},
		},
		{
			note: "splunk output with batching",
			config: `
output:
  type: splunk
  url: "https://http-input.foobar.hec.splunk.com/services/collector/event"
  token: opensplunkame
  batching:
    at_count: 42
    at_bytes: 43
    at_period: "300ms"
    array: true # overridden for splunk
    compress: false
`,
			checks: func(t testing.TB, a any, err error) {
				isConfig(Config{
					memoryBuffer: &memBufferOpts{
						MaxBytes: defaultMemoryMaxBytes,
					},
					outputs: []output{&outputSplunkOpts{
						URL:   "https://http-input.foobar.hec.splunk.com/services/collector/event",
						Token: "opensplunkame",
						Batching: &batchOpts{
							Period:   "300ms",
							Count:    42,
							Bytes:    43,
							Array:    false, // NB: overridden
							Compress: false,
						},
					}},
				})(t, a, err)

				isBenthos(map[string]any{
					"http_client": map[string]any{
						"batching": map[string]any{
							"byte_size": 43,
							"count":     42,
							"period":    "300ms",
							"processors": []map[string]any{
								{"archive": map[string]any{"format": "lines"}},
							},
						},
						"headers": map[string]any{
							"Authorization": "Splunk opensplunkame",
							"Content-Type":  "application/json",
						},
						"url":  "https://http-input.foobar.hec.splunk.com/services/collector/event",
						"verb": "POST",
					},
				})(t, a, err)
			},
		},
		{
			note: "s3 output with batching",
			config: `
output:
  type: s3
  endpoint: "https://local.minio:9000"
  region: us-underwater-1
  bucket: dl
  access_key_id: abc123
  access_secret: opens3same
  batching:
    at_count: 42
    at_bytes: 43
    at_period: "300ms"
    array: true # overridden for s3
    compress: true # overridden for s3
`,
			checks: func(t testing.TB, a any, err error) {
				isConfig(Config{
					memoryBuffer: &memBufferOpts{
						MaxBytes: defaultMemoryMaxBytes,
					},
					outputs: []output{&outputS3Opts{
						Endpoint:     "https://local.minio:9000",
						Region:       "us-underwater-1",
						Bucket:       "dl",
						AccessKeyID:  "abc123",
						AccessSecret: "opens3same",
						Batching: &batchOpts{
							Period:      "300ms",
							Count:       42,
							Bytes:       43,
							Array:       true,
							Compress:    true,
							unprocessed: true,
						},
					}},
				})(t, a, err)

				isBenthos(map[string]any{
					"aws_s3": map[string]any{
						"batching": map[string]any{
							"byte_size": 43,
							"count":     42,
							"period":    "300ms",
						},
						"bucket":       "dl",
						"path":         `${!json("timestamp").ts_strftime("%Y/%m/%d/%H")}/${!json("decision_id")}.json`,
						"endpoint":     "https://local.minio:9000",
						"content_type": "application/json",
						"region":       "us-underwater-1",
						"credentials":  map[string]any{"id": "abc123", "secret": "opens3same"},
					},
				})(t, a, err)
			},
		},
		{
			note: "s3 output missing bucket",
			config: `
output:
  type: s3
  endpoint: "https://local.minio:9000"
  access_key_id: abc123
  access_secret: opens3same
`,
			checks: func(t testing.TB, _ any, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if exp, act := "output S3 missing required configs: bucket, region", err.Error(); exp != act {
					t.Errorf("expected error %q, got %q", exp, act)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// defer goleak.VerifyNone(t) // TODO(sr): enable, fix failures
			mgr := getTestManager(tc.mgr)
			config, err := Factory().Validate(mgr, []byte(tc.config))
			if tc.checks != nil {
				tc.checks(t, config, err)
			}
			if err != nil {
				return
			}
			// always start to validate
			p := Factory().New(mgr, config)
			if err := p.Start(ctx); err != nil {
				t.Fatal(err)
			}
			p.Stop(ctx)
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
