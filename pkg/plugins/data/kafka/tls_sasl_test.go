package kafka_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/plugins/discovery"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	_ "github.com/styrainc/enterprise-opa-private/pkg/rego_vm" // important! use VM for rego.Eval below
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

const (
	caCertPath     = "testdata/tls/ca.pem"
	clientCertPath = "testdata/tls/client-cert.pem"
	clientKeyPath  = "testdata/tls/client-key.pem"
	serverCertPath = "testdata/tls/server-cert.pem"
	serverKeyPath  = "testdata/tls/server-key.pem"
)

const transform = `package e2e
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
	t.Parallel()

	ctx := context.Background()
	topic := "cipot"
	config := `
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [%[2]s]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
      tls_client_cert: testdata/tls/client-cert.pem
      tls_client_private_key: testdata/tls/client-key.pem
      tls_ca_cert: testdata/tls/ca.pem
`

	serverKeyPEMBlock, err := os.ReadFile(serverKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	serverCertPEMBlock, err := os.ReadFile(serverCertPath)
	if err != nil {
		t.Fatal(err)
	}
	broker, tx := testRedPanda(t, ctx,
		redpanda.WithTLS(serverCertPEMBlock, serverKeyPEMBlock),
	)
	t.Cleanup(func() { tx.Terminate(ctx) })

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

	cl, err := kafkaClient(broker, kgo.DialTLSConfig(&tc))
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, fmt.Sprintf(config, topic, broker))

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
	t.Parallel()

	ctx := context.Background()
	topic := "cipot"
	config := `
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [%[2]s]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
      sasl_mechanism: scram-sha-256
      sasl_username: %[3]s
      sasl_password: %[4]s
`

	broker, tx := testRedPanda(t, ctx,
		redpanda.WithEnableSASL(),
		redpanda.WithNewServiceAccount(user, pass),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithSuperusers(user),
	)
	t.Cleanup(func() { tx.Terminate(ctx) })

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, fmt.Sprintf(config, topic, broker, user, pass))

	sasl := kgo.SASL(scram.Auth{
		User: user,
		Pass: pass,
	}.AsSha256Mechanism())

	cl, err := kafkaClient(broker, sasl)
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
	t.Parallel()

	ctx := context.Background()
	topic := "cipot"
	config := `
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [%[2]s]
      topics: [%[1]s]
      from: 100ms
      rego_transform: "data.e2e.transform"
`

	store := storeWithPolicy(ctx, t, transform)

	broker, tx := testRedPanda(t, ctx)
	t.Cleanup(func() { tx.Terminate(ctx) })

	cl, err := kafkaClient(broker)
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

	mgr := pluginMgr(ctx, t, store, fmt.Sprintf(config, topic, broker))
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
	t.Parallel()

	t.Skip("testcontainer module redpanda can't deal with sasl and tls at the same time")
	ctx := context.Background()
	topic := "cipot"
	config := `
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [%[2]s]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
      tls_client_cert: testdata/tls/client-cert.pem
      tls_client_private_key: testdata/tls/client-key.pem
      tls_ca_cert: testdata/tls/ca.pem
      sasl_mechanism: scram-sha-256
      sasl_username: %[3]s
      sasl_password: %[4]s
`

	serverKeyPEMBlock, err := os.ReadFile(serverKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	serverCertPEMBlock, err := os.ReadFile(serverCertPath)
	if err != nil {
		t.Fatal(err)
	}
	broker, tx := testRedPanda(t, ctx,
		redpanda.WithEnableSASL(),
		redpanda.WithNewServiceAccount(user, pass),
		redpanda.WithTLS(serverCertPEMBlock, serverKeyPEMBlock),
	)
	t.Cleanup(func() { tx.Terminate(ctx) })

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
	cl, err := kafkaClient(broker, kgo.DialTLSConfig(&tc), sasl)
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, fmt.Sprintf(config, topic, broker, user, pass))

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
