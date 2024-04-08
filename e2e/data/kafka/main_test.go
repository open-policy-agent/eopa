//go:build e2e

// package kafka is for testing Enterprise OPA running as server,
// interacting with kafka-compatible services.
package kafka

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kzerolog"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

// number of messages to produce
const messageCount = 1_000

var eopaHTTPPort int

func TestMain(m *testing.M) {
	r := rand.New(rand.NewSource(2909))
	for {
		port := r.Intn(38181) + 1
		if utils.IsTCPPortBindable(port) {
			eopaHTTPPort = port
			break
		}
	}

	os.Exit(m.Run())
}

func TestSimple(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		note          string
		kafka         func(*testing.T, context.Context, ...testcontainers.ContainerCustomizer) (string, testcontainers.Container)
		consumerGroup bool
	}{
		{
			note:  "kafka",
			kafka: testKafka,
		},
		{
			note:          "kafka/consumer-group",
			kafka:         testKafka,
			consumerGroup: true,
		},
		{
			note:  "redpanda",
			kafka: testRedPanda,
		},
		{
			note:          "redpanda/consumer-group",
			kafka:         testRedPanda,
			consumerGroup: true,
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			broker, tx := tc.kafka(t, ctx)
			t.Cleanup(func() { tx.Terminate(ctx) })

			cl, err := kafkaClient(broker)
			if err != nil {
				t.Fatalf("client: %v", err)
			}

			config := fmt.Sprintf(`
plugins:
  data:
    messages:
      type: kafka
      urls: [%[1]s]
      consumer_group: %[2]v
      topics:
      - toothpaste
      - dinner
      rego_transform: "data.e2e.transform"
`, broker, tc.consumerGroup)
			policy := `package e2e
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
# merge with old
transform[key] := val if {
	some key, val in input.previous
	every msg in input.incoming {
		key != base64.decode(msg.key) # incoming batch takes precedence
	}
}
`
			eopa, eopaErr := eopaRun(t, config, policy, eopaHTTPPort)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			// produce a bunch of messages, fire and forget asynchronously
			for i := 0; i < messageCount; i++ {
				rec := &kgo.Record{
					Topic: "toothpaste",
					Key:   []byte(fmt.Sprint(i)),
					Value: []byte(fmt.Sprintf(`{"number": %d}`, i)),
				}
				cl.Produce(ctx, rec, nil)
			}

			exp := make(map[string]any, messageCount)
			for i := 0; i < messageCount; i++ {
				exp[strconv.Itoa(i)] = map[string]any{
					"headers": []any{},
					"value": map[string]any{
						"number": float64(i),
					},
				}
			}

			if err := wait.Func(func() bool {
				// check store response (TODO: check metrics/status when we have them)
				resp, err := utils.StdlibHTTPClient.Get(fmt.Sprintf("http://localhost:%d/v1/data/messages", eopaHTTPPort))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				act := map[string]any{}
				if err := json.Unmarshal(body, &act); err != nil {
					t.Fatal(err)
				}
				return cmp.Diff(exp, act["result"]) == ""
			}, 50*time.Millisecond, 15*time.Second); err != nil {
				t.Error(err)
			}

			// if we reach this, the diff was "" => our expectation was met

			{ // check prometheus metrics
				resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", eopaHTTPPort))
				if err != nil {
					t.Fatal(err)
				}
				expS := []string{
					"kafka_messages_buffered_fetch_records_total",
					"kafka_messages_buffered_produce_records_total",
					`kafka_messages_connects_total{node_id="[0-9]"}`,
					`kafka_messages_connects_total{node_id="seed_0"}`,
					`kafka_messages_fetch_bytes_total{node_id="[0-9]",topic="toothpaste"}`,
					`kafka_messages_read_bytes_total{node_id="[0-9]"}`,
					`kafka_messages_read_bytes_total{node_id="seed_0"}`,
					`kafka_messages_write_bytes_total{node_id="[0-9]"}`,
					`kafka_messages_write_bytes_total{node_id="seed_0"}`,
				}
				exp := make([]func(string) bool, len(expS))
				for i := range expS {
					exp[i] = regexp.MustCompile(expS[i]).MatchString
				}
				act := []string{}
				scanner := bufio.NewScanner(resp.Body)
				for scanner.Scan() {
					line := scanner.Text()
					if !strings.HasPrefix(line, "kafka_") {
						continue
					}
					sp := strings.Split(line, " ")
					act = append(act, sp[0])
				}
				if err := scanner.Err(); err != nil {
					t.Fatal(err)
				}
				match := 0
			ACT:
				for _, a := range act {
					for _, e := range exp {
						if e(a) {
							match++
							continue ACT
						}
					}
				}
				if match != len(expS) {
					t.Errorf("contains unexpected metrics: %v", act)
				}
			}

			// finally, check consumer group registration
			admCl := kadm.NewClient(cl)
			t.Cleanup(admCl.Close)

			grps, err := admCl.ListGroups(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.consumerGroup {
				if exp, act := 0, len(grps); exp != act {
					t.Errorf("expected %d consumer group, got %d", exp, act)
				}
				return
			}
			if exp, act := 1, len(grps); exp != act {
				t.Errorf("expected %d consumer group, got %d", exp, act)
			}
			var first string
			for k := range grps {
				first = k
			}
			re := regexp.MustCompile(`^eopa_[-a-f0-9]+_messages$`)
			if !re.MatchString(first) {
				t.Errorf("unexpected group name: %s", first)
			}
		})
	}
}

func kafkaClient(broker string) (*kgo.Client, error) {
	// logger := zerolog.New(os.Stderr) // for debugging
	logger := zerolog.New(io.Discard)

	opts := []kgo.Opt{
		kgo.SeedBrokers(broker),
		kgo.WithLogger(kzerolog.New(&logger)),
		kgo.AllowAutoTopicCreation(),
	}
	return kgo.NewClient(opts...)
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

func matches(re string) func(string) bool {
	return regexp.MustCompile(re).MatchString
}
