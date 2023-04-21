package kafka_test

import (
	"context"
	"encoding/json"
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

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/load-private/pkg/plugins/data"
	"github.com/styrainc/load-private/pkg/plugins/discovery"
	_ "github.com/styrainc/load-private/pkg/rego_vm" // important! use VM for rego.Eval below
	inmem "github.com/styrainc/load-private/pkg/store"
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
transform contains {"op": "add", "path": key, "value": val} if {
	print(input)
	payload := json.unmarshal(base64.decode(input.value))
	key := base64.decode(input.key)
	val := {
		"value": payload,
		"headers": input.headers,
	}
}
`
	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, config)

	kafka := testKafka(t)
	cl, err := kafkaClient(kafka)
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
transform contains {"op": "add", "path": key, "value": val} if {
	print(input)
	payload := json.unmarshal(base64.decode(input.value))
	key := base64.decode(input.key)
	val := {
		"value": payload,
		"headers": input.headers,
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

func TestKafkaTransforms(t *testing.T) {
	ctx := context.Background()
	kafka := testKafka(t)
	topic := "cipot"
	store := inmem.New()
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
transform contains json.unmarshal(base64.decode(input.value)) if print(input)
`
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "e2e.rego", []byte(transform))
	}); err != nil {
		t.Fatalf("store transform policy: %v", err)
	}
	l := logging.New()
	l.SetLevel(logging.Debug)
	mgr, err := plugins.New([]byte(config), "test-instance-id", store,
		plugins.Logger(l),
		plugins.EnablePrintStatements(true),
	)
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

	cl, err := kafkaClient(kafka)
	if err != nil {
		t.Fatalf("kafka client: %v", err)
	}

	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, op := range []struct {
		path string
		op   string
		val  any
	}{
		{
			path: "a",
			op:   "add",
			val:  map[string]any{"b": false, "c": 21},
		},
		{
			path: "a/c",
			op:   "replace",
			val:  float64(22),
		},
		{
			path: "a/b",
			op:   "remove",
		},
		{
			path: "arr",
			op:   "add",
			val:  []string{"foo"},
		},
		{
			path: "arr/-",
			op:   "add",
			val:  "bar",
		},
		{
			path: "arr/0",
			op:   "replace",
			val:  "fox",
		},
		{
			path: "done",
			op:   "add",
			val:  true,
		},
	} {
		payload, err := json.Marshal(map[string]any{
			"value": op.val,
			"op":    op.op,
			"path":  op.path,
		})
		if err != nil {
			t.Fatal(err)
		}
		record := &kgo.Record{
			Topic: topic,
			Value: payload,
		}
		cl.Produce(ctx, record, nil)
	}

	waitForStorePath(ctx, t, store, "/kafka/messages/done")

	act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/kafka/messages"))
	if err != nil {
		t.Fatalf("read back data: %v", err)
	}
	exp := map[string]any{
		"a":    map[string]any{"c": json.Number("22")},
		"arr":  []any{"fox", "bar"},
		"done": true,
	}
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("data value mismatch (-want +got):\n%s", diff)
	}
}

func testKafka(t *testing.T) *dockertest.Resource {
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
	})
	if err != nil {
		t.Fatalf("could not start kafka: %s", err)
	}

	if err := dockerPool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		client, err := kafkaClient(kafkaResource)
		if err != nil {
			return err
		}
		if err := client.Ping(ctx); err != nil {
			return err
		}

		record := &kgo.Record{Topic: "ping", Value: []byte(`true`)}
		return client.ProduceSync(ctx, record).FirstErr()

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

func kafkaClient(_ *dockertest.Resource, extra ...kgo.Opt) (*kgo.Client, error) {
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
