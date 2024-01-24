//go:build e2e

// package kafka is for testing Enterprise OPA as container, running as server,
// interacting with kafka-compatible services.
package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
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
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kzerolog"

	"github.com/open-policy-agent/opa/util"

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
		note  string
		kafka func(*testing.T, context.Context, ...testcontainers.ContainerCustomizer) (string, testcontainers.Container)
	}{
		{
			note:  "kafka",
			kafka: testKafka,
		},
		{
			note:  "redpanda",
			kafka: testRedPanda,
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
      topics:
      - toothpaste
      - dinner
      rego_transform: "data.e2e.transform"
`, broker)
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

			if err := util.WaitFunc(func() bool {
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
