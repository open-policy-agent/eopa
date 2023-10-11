package kafka_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	_ "github.com/styrainc/enterprise-opa-private/pkg/rego_vm" // important! use VM for rego.Eval below
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

const caCertPath = "testdata/tls/ca.pem"
const clientCertPath = "testdata/tls/client-cert.pem"
const clientKeyPath = "testdata/tls/client-key.pem"

const transform = `package e2e
import future.keywords
transform[key] := val if {
	some msg in input.incoming
	payload := json.unmarshal(base64.decode(msg.value))
	key := base64.decode(msg.key)
	val := {
		"value": payload,
		"headers": msg.headers,
	}
}
`

func TestTLS(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	config := fmt.Sprintf(`
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [127.0.0.1:19092]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
      tls_client_cert: testdata/tls/client-cert.pem
      tls_client_private_key: testdata/tls/client-key.pem
      tls_ca_cert: testdata/tls/ca.pem
`, topic)

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, config)

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
	tc := tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}

	_ = testRedPanda(t, []string{
		`--set`, `redpanda.kafka_api_tls={'name':'internal','enabled':true,'require_client_auth':true,'cert_file':'/w/tls/server-cert.pem','key_file':'/w/tls/server-key.pem','truststore_file':'/w/tls/ca.pem'}`,
		`--set`, `redpanda.admin_api_tls={'name':'internal','enabled':true,'require_client_auth':true,'cert_file':'/w/tls/server-cert.pem','key_file':'/w/tls/server-key.pem','truststore_file':'/w/tls/ca.pem'}`},
		kgo.DialTLSConfig(&tc))
	cl, err := kafkaClient(kgo.DialTLSConfig(&tc))
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}

	// record written before we're consuming messages
	record := &kgo.Record{Topic: topic, Key: []byte("one"), Value: []byte(`{"foo":"bar"}`)}
	if err := cl.ProduceSync(ctx, record).FirstErr(); err != nil {
		t.Fatalf("produce messages: %v", err)
	}

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitForStorePath(ctx, t, store, "/kafka/messages/one")

	{
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages/one"))
		if err != nil {
			t.Fatalf("read back data: %v", err)
		}
		exp := map[string]any{"headers": []any{}, "value": map[string]any{"foo": "bar"}}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch (-want +got):\n%s", diff)
		}
	}
}

// SASL credentials used with clients and the server
const user, pass = "admin", "wasspord"

func TestSASL(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	config := fmt.Sprintf(`
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [127.0.0.1:19092]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
      sasl_mechanism: scram-sha-256
      sasl_username: %[2]s
      sasl_password: %[3]s
`, topic, user, pass)

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, config)

	sasl := kgo.SASL(scram.Auth{
		User: user,
		Pass: pass,
	}.AsSha256Mechanism())
	_ = testRedPanda(t, []string{
		`--set`, `redpanda.enable_sasl=true`,
		`--set`, fmt.Sprintf(`redpanda.superusers=["%s"]`, user),
	}, sasl)
	cl, err := kafkaClient(sasl)
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}

	// record written before we're consuming messages
	record := &kgo.Record{Topic: topic, Key: []byte("one"), Value: []byte(`{"foo":"bar"}`)}
	if err := cl.ProduceSync(ctx, record).FirstErr(); err != nil {
		t.Fatalf("produce messages: %v", err)
	}

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitForStorePath(ctx, t, store, "/kafka/messages/one")

	{
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages/one"))
		if err != nil {
			t.Fatalf("read back data: %v", err)
		}
		exp := map[string]any{"headers": []any{}, "value": map[string]any{"foo": "bar"}}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestPlainFrom(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	config := fmt.Sprintf(`
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [127.0.0.1:19092]
      topics: [%[1]s]
      from: 100ms
      rego_transform: "data.e2e.transform"
`, topic, "", "")

	store := storeWithPolicy(ctx, t, transform)

	_ = testRedPanda(t, []string{
		`--set`, fmt.Sprintf(`redpanda.superusers=["%s"]`, user),
	})
	cl, err := kafkaClient()
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}

	// record written before we're consuming messages
	record := &kgo.Record{Topic: topic, Key: []byte("one"), Value: []byte(`{"foo":"baz"}`)}
	if err := cl.ProduceSync(ctx, record).FirstErr(); err != nil {
		t.Fatalf("produce messages: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	record2 := &kgo.Record{Topic: topic, Key: []byte("two"), Value: []byte(`{"foo":"bar"}`)}
	if err := cl.ProduceSync(ctx, record2).FirstErr(); err != nil {
		t.Fatalf("produce messages: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	mgr := pluginMgr(ctx, t, store, config)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitForStorePath(ctx, t, store, "/kafka/messages/two")

	{
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages/two"))
		if err != nil {
			t.Fatalf("read back data: %v", err)
		}
		exp := map[string]any{"headers": []any{}, "value": map[string]any{"foo": "bar"}}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch (-want +got):\n%s", diff)
		}
	}

	{
		// kafka/messages/one should not be found (older than 100ms)
		_, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages/one"))
		if err == nil {
			t.Errorf("unexpected data value found: /kafka/messages/one")
		}
		if !storage.IsNotFound(err) {
			t.Errorf("unexpected error for /kafka/messages/one: %v", err)
		}
	}
}

func TestTLSAndSASL(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	config := fmt.Sprintf(`
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [127.0.0.1:19092]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
      tls_client_cert: testdata/tls/client-cert.pem
      tls_client_private_key: testdata/tls/client-key.pem
      tls_ca_cert: testdata/tls/ca.pem
      sasl_mechanism: scram-sha-256
      sasl_username: %[2]s
      sasl_password: %[3]s
`, topic, user, pass)

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, config)

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
	tc := tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}

	sasl := kgo.SASL(scram.Auth{
		User: user,
		Pass: pass,
	}.AsSha256Mechanism())
	_ = testRedPanda(t, []string{
		`--set`, `redpanda.enable_sasl=true`,
		`--set`, fmt.Sprintf(`redpanda.superusers=["%s"]`, user),
		`--set`, `redpanda.kafka_api_tls={'name':'internal','enabled':true,'require_client_auth':true,'cert_file':'/w/tls/server-cert.pem','key_file':'/w/tls/server-key.pem','truststore_file':'/w/tls/ca.pem'}`,
		`--set`, `redpanda.admin_api_tls={'name':'internal','enabled':true,'require_client_auth':true,'cert_file':'/w/tls/server-cert.pem','key_file':'/w/tls/server-key.pem','truststore_file':'/w/tls/ca.pem'}`},
		kgo.DialTLSConfig(&tc), sasl)
	cl, err := kafkaClient(kgo.DialTLSConfig(&tc), sasl)
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}

	// record written before we're consuming messages
	record := &kgo.Record{Topic: topic, Key: []byte("one"), Value: []byte(`{"foo":"bar"}`)}
	if err := cl.ProduceSync(ctx, record).FirstErr(); err != nil {
		t.Fatalf("produce messages: %v", err)
	}

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	waitForStorePath(ctx, t, store, "/kafka/messages/one")

	{
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages/one"))
		if err != nil {
			t.Fatalf("read back data: %v", err)
		}
		exp := map[string]any{"headers": []any{}, "value": map[string]any{"foo": "bar"}}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch (-want +got):\n%s", diff)
		}
	}
}

func storeWithPolicy(ctx context.Context, t *testing.T, transform string) storage.Store {
	t.Helper()
	store := inmem.New()
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "e2e.rego", []byte(transform))
	}); err != nil {
		t.Fatalf("store transform policy: %v", err)
	}
	return store
}

func pluginMgr(_ context.Context, t *testing.T, store storage.Store, config string) *plugins.Manager {
	t.Helper()
	h := topdown.NewPrintHook(os.Stderr)
	opts := []func(*plugins.Manager){
		plugins.PrintHook(h),
		plugins.EnablePrintStatements(true),
	}
	if !testing.Verbose() {
		opts = append(opts, plugins.Logger(logging.NewNoOpLogger()))
		opts = append(opts, plugins.ConsoleLogger(logging.NewNoOpLogger()))
	}

	mgr, err := plugins.New([]byte(config), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	disco, err := discovery.New(mgr,
		discovery.Factories(map[string]plugins.Factory{data.Name: data.Factory()}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}

// NOTE(sr): TLS setup is much simpler with RedPanda -- so we're using this to verify
// the plugin's TLS/SASL functionality.
// CAVEAT: RP only support SCRAM-SHA-256, no other variant (SCRAM-SHA-512, PLAIN).
func testRedPanda(t *testing.T, flags []string, extra ...kgo.Opt) *dockertest.Resource {
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// HACK(philip): These Kafka tests are expected to be run serially
	// (thus the shared port number). Both CI and local runs can be
	// disrupted by lingering Kafka containers from prior runs that
	// terminated early with errors/timeouts/panics, which prevented the
	// `t.Cleanup()` function from running normally. Therefore, we hackily
	// look up the container and purge it *before* creating a new Kafka
	// instance.
	if res, found := dockerPool.ContainerByName("kafka"); found {
		_ = dockerPool.Purge(res)
	}
	kafkaResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "kafka",
		Repository: "redpandadata/redpanda",
		Tag:        "latest",
		Hostname:   "kafka",
		Env:        []string{},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"19092/tcp": {{HostIP: "localhost", HostPort: "19092/tcp"}}, // needed to have localhost:19092 work for kafkaClient
		},
		ExposedPorts: []string{"19092/tcp"},
		Mounts: []string{
			filepath.Join(pwd, "testdata/") + ":/w",
		},
		Cmd: append(strings.Split(`redpanda
		start
		--kafka-addr internal://0.0.0.0:19092
		--advertise-kafka-addr internal://127.0.0.1:19092
		--overprovisioned
		--check=false
		--set redpanda.auto_create_topics_enabled=true
	`, " \n\t"), flags...),
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true so that stopped container goes away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		t.Fatalf("could not start kafka: %s", err)
	}

	if err := dockerPool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		client, err := kafkaClient(extra...)
		if err != nil {
			t.Logf("kafkaClient: %v", err)
			return err
		}
		buf := bytes.Buffer{}
		out, err := kafkaResource.Exec(append(strings.Split("rpk acl user create admin -p", " "), pass), dockertest.ExecOptions{StdOut: &buf})
		if err != nil {
			t.Logf("docker exec: %v", err)
			return err
		}
		t.Logf("docker exec exited %d: %s", out, buf.String())
		if err := client.Ping(ctx); err != nil {
			t.Logf("Ping: %v", err)
			return err
		}

		record := &kgo.Record{Topic: "ping", Value: []byte(`true`)}
		err = client.ProduceSync(ctx, record).FirstErr()
		t.Logf("ProduceSync: %v", err)
		return err
	}); err != nil {
		t.Fatalf("could not connect to kafka: %s", err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(kafkaResource); err != nil {
			t.Fatalf("could not purge kafkaResource: %s", err)
		}
	})

	return kafkaResource
}
