// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package decisionlogs

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/plugin/kslog"

	"github.com/open-policy-agent/eopa/e2e/wait"
)

func TestDecisionLogsKafkaOutput(t *testing.T) {
	const caCertPath = "testdata/tls/ca.pem"
	const clientCertPath = "testdata/tls/client-cert.pem"
	const serverCertPath = "testdata/tls/server-cert.pem"
	const clientKeyPath = "testdata/tls/client-key.pem"
	const serverKeyPath = "testdata/tls/server-key.pem"

	policy := `
package test
import future.keywords

coin if rand.intn("coin", 2)
`

	plaintextConfig := `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    buffer:
      type: memory
    output:
      type: kafka
      urls:
      - %[1]s
      topic: logs
`

	batchConfig := plaintextConfig + `
      batching:
        at_count: 2
        format: array
        compress: false
`

	tlsConfig := plaintextConfig + `
      tls:
        cert: testdata/tls/client-cert.pem
        private_key: testdata/tls/client-key.pem
        ca_cert: testdata/tls/ca.pem
`

	sasl256Config := plaintextConfig + `
      sasl:
        - username: admin256
          password: testPassword
          mechanism: SCRAM-SHA-256
`

	mustBS := must(t, []byte(nil))
	keyPEMBlock := mustBS(os.ReadFile(clientKeyPath))
	certPEMBlock := mustBS(os.ReadFile(clientCertPath))
	serverCertPEMBlock := mustBS(os.ReadFile(serverCertPath))
	serverKeyPEMBlock := mustBS(os.ReadFile(serverKeyPath))
	cert := must(t, tls.Certificate{})(tls.X509KeyPair(certPEMBlock, keyPEMBlock))
	caCert := mustBS(os.ReadFile(caCertPath))
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	tcfg := tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}

	for _, tc := range []struct {
		note      string
		config    string
		opts      []kgo.Opt
		kafka     func(*testing.T, context.Context, ...testcontainers.ContainerCustomizer) (string, testcontainers.Container)
		kafkaArgs []testcontainers.ContainerCustomizer
		array     bool
	}{
		{
			note:   "kafka/plaintext",
			config: plaintextConfig,
			kafka:  testKafka,
		},
		{
			note:   "redpanda/plaintext",
			config: plaintextConfig,
			kafka:  testRedPanda,
		},
		{
			note:   "redpanda/tls",
			config: tlsConfig,
			opts:   []kgo.Opt{kgo.DialTLSConfig(&tcfg)},
			kafka:  testRedPanda,
			kafkaArgs: []testcontainers.ContainerCustomizer{
				redpanda.WithTLS(serverCertPEMBlock, serverKeyPEMBlock),
			},
		},
		{
			note:   "kafka/plaintext/batching",
			config: batchConfig,
			array:  true,
			kafka:  testKafka,
		},
		{
			note:   "redpanda/scram-sha-256", // NOTE(sr): testcontainers-go/modules/redpanda only supports SCRAM-SHA-256
			config: sasl256Config,
			opts: []kgo.Opt{kgo.SASL(scram.Auth{
				User: "admin256",
				Pass: "testPassword",
			}.AsSha256Mechanism())},
			kafka: testRedPanda,
			kafkaArgs: []testcontainers.ContainerCustomizer{
				redpanda.WithEnableSASL(),
				redpanda.WithNewServiceAccount("admin256", "testPassword"),
			},
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			ctx := context.Background()
			buf := bytes.Buffer{}

			broker, tx := tc.kafka(t, ctx, tc.kafkaArgs...)
			t.Cleanup(func() { tx.Terminate(ctx) })
			go func() {
				cl, err := kafkaClient(broker, "logs", tc.opts...)
				if err != nil {
					panic(err)
				}
				for {
					rs := cl.PollFetches(ctx)
					if len(rs.Errors()) > 0 {
						err := rs.Errors()[0].Err
						if errors.Is(err, context.Canceled) {
							return
						}
						panic(err)
					}
					iter := rs.RecordIter()
					for !iter.Done() {
						val := iter.Next().Value
						t.Logf("value: %s", string(val))
						if _, err := buf.Write(val); err != nil {
							panic(err)
						}
					}
				}
			}()

			eopa, _, eopaErr := loadEOPA(t, fmt.Sprintf(tc.config, broker), policy, eopaHTTPPort, false)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			for i := 0; i < 2; i++ { // act: send API requests
				req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort),
					strings.NewReader(fmt.Sprintf(`{"input":%d}`, i)))
				if err != nil {
					t.Fatalf("http request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if exp, act := 200, resp.StatusCode; exp != act {
					t.Fatalf("expected status %d, got %d", exp, act)
				}
			}

			logs := collectDL(t, &buf, tc.array, 2)
			// logs might have come out of the kafka topic out of order: sort by ID
			// before asserting things
			sort.Slice(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })

			{ // request 1
				dl := payload{
					Result: true,
					ID:     1,
					Input:  float64(0),
					Labels: standardLabels,
				}
				if diff := cmp.Diff(dl, logs[0], stdIgnores); diff != "" {
					t.Errorf("diff: (-want +got):\n%s", diff)
				}
			}
			{ // request 2
				dl := payload{
					Result: true,
					ID:     2,
					Input:  float64(1),
					Labels: standardLabels,
				}
				if diff := cmp.Diff(dl, logs[1], stdIgnores); diff != "" {
					t.Errorf("diff: (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func testRedPanda(t *testing.T, ctx context.Context, cs ...testcontainers.ContainerCustomizer) (string, testcontainers.Container) {
	tc, err := redpanda.Run(ctx, "redpandadata/redpanda:v24.2.12", append(cs, redpanda.WithAutoCreateTopics())...)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := tc.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return broker, tc
}

func testKafka(t *testing.T, ctx context.Context, cs ...testcontainers.ContainerCustomizer) (string, testcontainers.Container) {
	tc, err := kafka.Run(ctx, "confluentinc/confluent-local:7.7.2", cs...)
	if err != nil {
		t.Fatalf("could not start kafka: %s", err)
	}
	brokers, err := tc.Brokers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return brokers[0], tc
}

func kafkaClient(broker, topic string, o ...kgo.Opt) (*kgo.Client, error) {
	logger := kslog.New(slog.Default())
	_ = logger

	opts := []kgo.Opt{
		kgo.SeedBrokers(broker),
		kgo.AllowAutoTopicCreation(),
		kgo.ConsumeTopics(topic),
		// kgo.WithLogger(logger), // uncomment for debugging
	}
	return kgo.NewClient(append(opts, o...)...)
}

func must[T any](t *testing.T, _ T) func(T, error) T {
	return func(xs T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return xs
	}
}
