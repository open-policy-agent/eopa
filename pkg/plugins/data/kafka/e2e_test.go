package kafka_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kslog"

	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	_ "github.com/styrainc/enterprise-opa-private/pkg/rego_vm" // important! use VM for rego.Eval below
)

func TestKafkaData(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	topic2 := "btw"
	config := `
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [%[3]s]
      topics: [%[1]s, %[2]s]
      rego_transform: "data.e2e.transform"
`

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

	broker, tx := testKafka(t, ctx)
	t.Cleanup(func() { tx.Terminate(ctx) })

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, fmt.Sprintf(config, topic, topic2, broker))

	cl, err := kafkaClient(broker)
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
	config := `
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [%[2]s]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
`

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
	broker, tx := testKafka(t, ctx)
	t.Cleanup(func() { tx.Terminate(ctx) })

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, fmt.Sprintf(config, topic, broker))
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
	config := `
plugins:
  data:
    kafka.messages:
      type: kafka
      urls: [%[2]s]
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
`

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
	broker, tx := testKafka(t, ctx)
	t.Cleanup(func() { tx.Terminate(ctx) })

	noop := `package nothing`
	store := storeWithPolicy(ctx, t, noop)
	mgr := pluginMgr(ctx, t, store, fmt.Sprintf(config, topic, broker))

	cl, err := kafkaClient(broker)
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

func kafkaClient(broker string, extra ...kgo.Opt) (*kgo.Client, error) {
	logger := kslog.New(slog.Default())
	_ = logger

	opts := []kgo.Opt{
		kgo.SeedBrokers(broker),
		kgo.AllowAutoTopicCreation(),
		// kgo.WithLogger(logger), // uncomment for debugging
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

func testKafka(t *testing.T, ctx context.Context, cs ...testcontainers.ContainerCustomizer) (string, testcontainers.Container) {
	tc, err := kafka.RunContainer(ctx, cs...)
	if err != nil {
		t.Fatalf("could not start kafka: %s", err)
	}
	brokers, err := tc.Brokers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return brokers[0], tc
}

// NOTE(sr): TLS setup is much simpler with RedPanda -- so we're using this to verify
// the plugin's TLS/SASL functionality.
// CAVEAT: RP only support SCRAM-SHA-256, no other variant (SCRAM-SHA-512, PLAIN).
func testRedPanda(t *testing.T, ctx context.Context, cs ...testcontainers.ContainerCustomizer) (string, testcontainers.Container) {
	opts := []testcontainers.ContainerCustomizer{
		redpanda.WithAutoCreateTopics(),
	}
	tc, err := redpanda.RunContainer(ctx, append(opts, cs...)...)
	if err != nil {
		t.Fatalf("could not start kafka: %s", err)
	}
	brokers, err := tc.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return brokers, tc
}
