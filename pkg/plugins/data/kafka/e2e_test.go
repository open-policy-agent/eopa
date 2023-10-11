package kafka_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kzerolog"

	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	_ "github.com/styrainc/enterprise-opa-private/pkg/rego_vm" // important! use VM for rego.Eval below
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

func TestKafkaData(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	topic2 := "btw"
	config := fmt.Sprintf(`
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [localhost:19092]
      topics: [%[1]s, %[2]s]
      rego_transform: "data.e2e.transform"
`, topic, topic2)

	transform := `package e2e
import future.keywords
transform[key] := val if {
	some msg in input.incoming # incoming is a batch
	print(msg)
	payload := json.unmarshal(base64.decode(msg.value))
	key := base64.decode(msg.key)
	val := {
		"value": payload,
		"headers": msg.headers,
	}
}
`
	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, config)

	_ = testKafka(t)
	cl, err := kafkaClient()
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

	// record written while we're consuming, different topic
	record = &kgo.Record{
		Topic: topic2,
		Key:   []byte("two"),
		Value: []byte(`{"fox":"box"}`),
		Headers: []kgo.RecordHeader{
			{Key: "header", Value: []byte("value")},
		},
	}
	if err := cl.ProduceSync(ctx, record).FirstErr(); err != nil {
		t.Fatalf("produce messages: %v", err)
	}

	waitForStorePath(ctx, t, store, "/kafka/messages/one")
	waitForStorePath(ctx, t, store, "/kafka/messages/two")

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

	{
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages/two"))
		if err != nil {
			t.Fatalf("read back data: %v", err)
		}
		exp := map[string]any{
			"headers": []any{map[string]any{"key": "header", "value": "dmFsdWU="}},
			"value":   map[string]any{"fox": "box"},
		}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestKafkaOwned(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	config := fmt.Sprintf(`
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [localhost:19092]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
`, topic)

	transform := `package e2e
import future.keywords
transform[key] := val if {
	some msg in input.incoming # incoming is a batch
	payload := json.unmarshal(base64.decode(msg.value))
	key := base64.decode(msg.key)
	val := {
		"value": payload,
		"headers": msg.headers,
	}
}
`
	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, config)

	testKafka(t)

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// test owned path
	err := storage.WriteOne(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/kafka/messages"), map[string]any{"foo": "bar"})
	if err == nil || err.Error() != `path "/kafka/messages" is owned by plugin "kafka"` {
		t.Errorf("owned check failed, got %v", err)
	}
}

// TestKafkaPolicyUpdate sets up this sequence of events:
//  0. there is no transform policy present, but it is configured with the kafka data plugin
//  1. data is published on the topic
//  2. the data is NOT pulled, nothing happens
//  3. the transform policy is stored
//  4. on the next iteration, the previously-published record is processed, transformed and stored
//  5. another message is published on the topic
//  6. the message is processed, and the rego_transform takes care of KEEPING the previously stored
//     message -- it takes care of the merging of old and new
//  7. another message is published on the topic, same key as (1.)
//  8. the newest version of the message replaces the previous one
func TestKafkaPolicyUpdate(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	config := fmt.Sprintf(`
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [localhost:19092]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
`, topic)

	transform := `package e2e
import future.keywords
transform[key] := val if {
	some msg in input.incoming
	print("new", msg)
	payload := json.unmarshal(base64.decode(msg.value))
	key := base64.decode(msg.key)
	val := {
		"value": payload,
		"headers": msg.headers,
	}
}

# merge with old
transform[key] := val if {
	some key, val in input.previous
	print("prev", key, val)
	every msg in input.incoming {
		key != base64.decode(msg.key) # incoming batch takes precedence
	}
}
`

	noop := `package nothing`
	store := storeWithPolicy(ctx, t, noop)
	mgr := pluginMgr(ctx, t, store, config)

	_ = testKafka(t)
	cl, err := kafkaClient()
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}

	{
		// record written before we're consuming messages
		// this one is NOT to be ignored: we don't have a data transform yet but we might just
		// be waiting for a bundle to be activated
		record := &kgo.Record{Topic: topic, Key: []byte("one"), Value: []byte(`{"foo":"bar"}`)}
		if err := cl.ProduceSync(ctx, record).FirstErr(); err != nil {
			t.Fatalf("produce messages: %v", err)
		}
	}

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Give the plugin some time to process the first message before we're updating the
	// transform policy.
	time.Sleep(100 * time.Millisecond)

	// update transform
	if err := storage.Txn(ctx, mgr.Store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "e2e.rego", []byte(transform))
	}); err != nil {
		t.Fatalf("store transform policy: %v", err)
	}

	// Storage triggers are all run before the store write returns, so the transform
	// should be in place by now.

	{ // this record should be transformed and stored
		record := &kgo.Record{
			Topic: topic,
			Key:   []byte("two"),
			Value: []byte(`{"fox":"box"}`),
			Headers: []kgo.RecordHeader{
				{Key: "header", Value: []byte("value")},
			},
		}
		if err := cl.ProduceSync(ctx, record).FirstErr(); err != nil {
			t.Fatalf("produce messages: %v", err)
		}
	}

	waitForStorePath(ctx, t, store, "/kafka/messages/one")
	waitForStorePath(ctx, t, store, "/kafka/messages/two")
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
	{
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages/two"))
		if err != nil {
			t.Fatalf("read back data: %v", err)
		}
		exp := map[string]any{
			"headers": []any{map[string]any{"key": "header", "value": "dmFsdWU="}},
			"value":   map[string]any{"fox": "box"},
		}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch (-want +got):\n%s", diff)
		}
	}

	{
		// record overwriting "one" published before
		record := &kgo.Record{Topic: topic, Key: []byte("one"), Value: []byte(`{"foo":"bear"}`)}
		if err := cl.ProduceSync(ctx, record).FirstErr(); err != nil {
			t.Fatalf("produce messages: %v", err)
		}
		// sentinel message, everything in this test is in-order
		if err := cl.ProduceSync(ctx, &kgo.Record{Topic: topic, Key: []byte("three"), Value: []byte(`{}`)}).FirstErr(); err != nil {
			t.Fatalf("produce messages: %v", err)
		}
	}

	waitForStorePath(ctx, t, store, "/kafka/messages/three")
	{
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages/one"))
		if err != nil {
			t.Fatalf("read back data: %v", err)
		}
		exp := map[string]any{"headers": []any{}, "value": map[string]any{"foo": "bear"}}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch (-want +got):\n%s", diff)
		}
	}
}

func testKafka(t *testing.T) *dockertest.Resource {
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
		Repository: "bitnami/kafka",
		Tag:        "latest",
		Hostname:   "kafka",
		Env: []string{
			"KAFKA_BROKER_ID=1",
			"KAFKA_CFG_NODE_ID=1",
			"KAFKA_ENABLE_KRAFT=yes",
			"KAFKA_CFG_PROCESS_ROLES=broker,controller",
			"KAFKA_CFG_CONTROLLER_LISTENER_NAMES=CONTROLLER",
			"KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE=true",
			"KAFKA_CFG_LISTENERS=PLAINTEXT://:19092,CONTROLLER://:9093",
			"KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
			"KAFKA_CFG_ADVERTISED_LISTENERS=PLAINTEXT://127.0.0.1:19092",
			"KAFKA_CFG_CONTROLLER_QUORUM_VOTERS=1@127.0.0.1:9093",
			"ALLOW_PLAINTEXT_LISTENER=yes",
		},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"19092/tcp": {{HostIP: "localhost", HostPort: "19092/tcp"}},
		},
		ExposedPorts: []string{"19092/tcp"},
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
	if err := dockerPool.Retry(kafkaPing); err != nil {
		t.Fatalf("could not connect to kafka: %s", err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(kafkaResource); err != nil {
			t.Fatalf("could not purge kafkaResource: %s", err)
		}
	})

	return kafkaResource
}

func kafkaPing() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	client, err := kafkaClient()
	if err != nil {
		return err
	}
	if err := client.Ping(ctx); err != nil {
		return err
	}

	record := &kgo.Record{Topic: "ping", Value: []byte(`true`)}
	return client.ProduceSync(ctx, record).FirstErr()
}

func kafkaClient(extra ...kgo.Opt) (*kgo.Client, error) {
	var logger zerolog.Logger
	if testing.Verbose() {
		logger = zerolog.New(os.Stderr)
	} else {
		logger = zerolog.New(io.Discard)
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers("127.0.0.1:19092"),
		kgo.WithLogger(kzerolog.New(&logger)),
		kgo.AllowAutoTopicCreation(),
	}
	return kgo.NewClient(append(opts, extra...)...)
}

func waitForStorePath(ctx context.Context, t *testing.T, store storage.Store, path string) {
	t.Helper()
	if err := util.WaitFunc(func() bool {
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath(path))
		if err != nil {
			if storage.IsNotFound(err) {
				return false
			}
			t.Fatalf("read back data: %v", err)
		}
		if cmp.Diff(map[string]any{}, act) == "" { // empty obj
			return false
		}
		return true
	}, 200*time.Millisecond, 10*time.Second); err != nil {
		t.Fatalf("wait for store path %v: %v", path, err)
	}
}
