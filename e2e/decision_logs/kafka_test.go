//go:build e2e

package decisionlogs

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/plugin/kzerolog"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

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

func TestDecisionLogsKafkaOutput(t *testing.T) {
	const caCertPath = "testdata/tls/ca.pem"
	const clientCertPath = "testdata/tls/client-cert.pem"
	const clientKeyPath = "testdata/tls/client-key.pem"

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
      - localhost:29092
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
	sasl512Config := plaintextConfig + `
      sasl:
        - username: admin512
          password: testPassword
          mechanism: scram-sha-512
`

	keyPEMBlock, err := os.ReadFile(clientKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	certPEMBlock, err := os.ReadFile(clientCertPath)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatal(err)
	}
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
		kafka     func(*testing.T, []string, []kgo.Opt, func(*dockertest.Resource) error) *dockertest.Resource
		kafkaArgs []string
		setup     func(*dockertest.Resource) error
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
			kafkaArgs: []string{
				`--set`, `redpanda.kafka_api_tls={'name':'internal','enabled':true,'require_client_auth':true,'cert_file':'/w/tls/server-cert.pem','key_file':'/w/tls/server-key.pem','truststore_file':'/w/tls/ca.pem'}`,
				`--set`, `redpanda.admin_api_tls={'name':'internal','enabled':true,'require_client_auth':true,'cert_file':'/w/tls/server-cert.pem','key_file':'/w/tls/server-key.pem','truststore_file':'/w/tls/ca.pem'}`,
			},
		},
		{
			note:   "kafka/plaintext/batching",
			config: batchConfig,
			array:  true,
			kafka:  testKafka,
		},
		{
			note:   "kafka/scram-sha-256",
			config: sasl256Config,
			opts: []kgo.Opt{kgo.SASL(scram.Auth{
				User: "admin256",
				Pass: "testPassword",
			}.AsSha256Mechanism())},
			kafka: testRedPanda,
			kafkaArgs: []string{
				"--set", "redpanda.enable_sasl=true",
				"--set", `redpanda.superusers=["admin256"]`,
			},
			setup: redPandaUser("admin256", "testPassword", "scram-sha-256"),
		},
		{
			note:   "kafka/scram-sha-512",
			config: sasl512Config,
			opts: []kgo.Opt{kgo.SASL(scram.Auth{
				User: "admin512",
				Pass: "testPassword",
			}.AsSha512Mechanism())},
			kafka: testRedPanda,
			kafkaArgs: []string{
				"--set", "redpanda.enable_sasl=true",
				"--set", `redpanda.superusers=["admin512"]`,
			},
			setup: redPandaUser("admin512", "testPassword", "scram-sha-512"),
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			cleanupPrevious(t)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			buf := bytes.Buffer{}

			_ = tc.kafka(t, tc.kafkaArgs, tc.opts, tc.setup)
			go func() {
				cl, err := kafkaClient("logs", tc.opts...)
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

			eopa, _, eopaErr := loadEnterpriseOPA(t, tc.config, policy, false)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			for i := 0; i < 2; i++ { // act: send API requests
				req, err := http.NewRequest("POST", "http://localhost:28181/v1/data/test/coin",
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

func testRedPanda(t *testing.T, flags []string, opts []kgo.Opt, setup func(*dockertest.Resource) error) *dockertest.Resource {
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	kafkaResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "kafka-dl-e2e",
		Repository: "docker.redpanda.com/redpandadata/redpanda",
		Tag:        "latest",
		Hostname:   "kafka-dl-e2e",
		Env:        []string{},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"29092/tcp": {{HostIP: "localhost", HostPort: "29092/tcp"}}, // needed to have localhost:29092 work for kafkaClient
		},
		ExposedPorts: []string{"29092/tcp"},
		Mounts: []string{
			filepath.Join(pwd, "testdata/") + ":/w",
		},
		Cmd: append(strings.Split(`redpanda
		start
		--kafka-addr internal://0.0.0.0:29092
		--advertise-kafka-addr internal://127.0.0.1:29092
		--overprovisioned
		--check=false
		--default-log-level=debug
		--set redpanda.auto_create_topics_enabled=true
	`, " \n\t"), flags...),
	})
	if err != nil {
		t.Fatalf("could not start kafka: %s", err)
	}
	if setup != nil {
		if err := dockerPool.Retry(func() error { return setup(kafkaResource) }); err != nil {
			t.Fatalf("could not set up kafka: %s", err)
		}
	}
	if err := dockerPool.Retry(func() error { return pingKafka(opts...) }); err != nil {
		t.Fatalf("could not connect to kafka: %s", err)
	}
	t.Cleanup(func() {
		if err := dockerPool.Purge(kafkaResource); err != nil {
			t.Fatalf("could not purge kafkaResource: %s", err)
		}
	})

	return kafkaResource
}

func redPandaUser(username string, password string, mechanism string) func(*dockertest.Resource) error {
	return func(r *dockertest.Resource) error {
		stderr := new(bytes.Buffer)
		exitCode, err := r.Exec(
			[]string{"rpk", "acl", "user", "create", username, "-p", password, "--api-urls", "localhost:9644", "--mechanism", mechanism},
			dockertest.ExecOptions{StdErr: stderr},
		)
		if err != nil {
			return fmt.Errorf("error running create admin comment: %w", err)
		}
		if exitCode != 0 {
			errorMessage, _ := io.ReadAll(stderr)
			return fmt.Errorf("could not create admin user: %s", string(errorMessage))
		}
		return nil
	}
}

func pingKafka(opts ...kgo.Opt) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	client, err := kafkaClient("ping", opts...)
	if err != nil {
		return err
	}
	if err := client.Ping(ctx); err != nil {
		return err
	}

	record := &kgo.Record{Topic: "ping", Value: []byte(`true`)}
	return client.ProduceSync(ctx, record).FirstErr()
}

func testKafka(t *testing.T, extraEnv []string, opts []kgo.Opt, setup func(*dockertest.Resource) error) *dockertest.Resource {
	kafkaResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "kafka-dl-e2e",
		Repository: "bitnami/kafka",
		Tag:        "latest",
		Hostname:   "kafka-dl-e2e",
		Env: append([]string{
			"BITNAMI_DEBUG=yes", // show an error if this config is wrong
			"KAFKA_BROKER_ID=1",
			"KAFKA_CFG_NODE_ID=1",
			"KAFKA_ENABLE_KRAFT=yes",
			"KAFKA_CFG_PROCESS_ROLES=broker,controller",
			"KAFKA_CFG_CONTROLLER_LISTENER_NAMES=CONTROLLER",
			"KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE=true",
			"KAFKA_CFG_LISTENERS=INTERNAL://kafka-dl-e2e:9091,EXTERNAL://:29092,CONTROLLER://:9093", // INTERNAL is between docker containers; EXTERNAL is the exposed port
			"KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,EXTERNAL:PLAINTEXT,INTERNAL:PLAINTEXT",
			"KAFKA_CFG_ADVERTISED_LISTENERS=EXTERNAL://127.0.0.1:29092,INTERNAL://kafka-dl-e2e:9091",
			"KAFKA_CFG_INTER_BROKER_LISTENER_NAME=INTERNAL",
			"KAFKA_CFG_CONTROLLER_QUORUM_VOTERS=1@127.0.0.1:9093",
			"ALLOW_PLAINTEXT_LISTENER=yes",
		}, extraEnv...),
		PortBindings: map[docker.Port][]docker.PortBinding{
			"29092/tcp": {{HostIP: "localhost", HostPort: "29092/tcp"}}, // needed to have localhost:29092 work for kafkaClient
		},
		ExposedPorts: []string{"29092/tcp"},
	})
	if err != nil {
		t.Fatalf("could not start kafka: %s", err)
	}
	if setup != nil {
		if err := dockerPool.Retry(func() error { return setup(kafkaResource) }); err != nil {
			t.Fatalf("could not set up kafka: %s", err)
		}
	}
	if err := dockerPool.Retry(func() error { return pingKafka(opts...) }); err != nil {
		t.Fatalf("could not connect to kafka: %s", err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(kafkaResource); err != nil {
			t.Fatalf("could not purge kafkaResource: %s", err)
		}
	})
	return kafkaResource
}

func kafkaClient(topic string, o ...kgo.Opt) (*kgo.Client, error) {
	// logger := zerolog.New(os.Stderr) // for debugging
	logger := zerolog.New(io.Discard)

	opts := []kgo.Opt{
		kgo.SeedBrokers("localhost:29092"),
		kgo.WithLogger(kzerolog.New(&logger)),
		kgo.AllowAutoTopicCreation(),
		kgo.ConsumeTopics(topic),
	}
	return kgo.NewClient(append(opts, o...)...)
}

func cleanupPrevious(t *testing.T) {
	t.Helper()
	for _, n := range []string{"kafka-dl-e2e"} {
		if err := dockerPool.RemoveContainerByName(n); err != nil {
			t.Fatalf("remove %s: %v", n, err)
		}
	}
}
