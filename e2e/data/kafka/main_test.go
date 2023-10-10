//go:build e2e

// package kafka is for testing Enterprise OPA as container, running as server,
// interacting with kafka-compatible services.
package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kzerolog"

	"github.com/open-policy-agent/opa/util"
)

const defaultImage = "ko.local/enterprise-opa-private:edge" // built via `make build-local`

// number of messages to produce
const messageCount = 1_000

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

func TestSimple(t *testing.T) {
	for _, tc := range []struct {
		note  string
		kafka func(*testing.T, *docker.Network) *dockertest.Resource
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
			cleanupPrevious(t)
			ctx := context.Background()
			image := os.Getenv("IMAGE")
			if image == "" {
				image = defaultImage
			}

			network, err := dockerPool.Client.CreateNetwork(docker.CreateNetworkOptions{Name: "eopa_kafka_e2e"})
			if err != nil {
				t.Fatalf("network: %v", err)
			}
			t.Cleanup(func() {
				if err := dockerPool.Client.RemoveNetwork(network.ID); err != nil {
					t.Fatal(err)
				}
			})

			_ = tc.kafka(t, network)

			cl, err := kafkaClient()
			if err != nil {
				t.Fatalf("client: %v", err)
			}

			config := `
plugins:
  data:
    messages:
      type: kafka
      urls: ["kafka-e2e:9091"]
      topics:
      - toothpaste
      - dinner
      rego_transform: "data.e2e.transform"
`
			policy := `package e2e
import future.keywords
transform[key] := val if {
	some msg in input
	payload := json.unmarshal(base64.decode(msg.value))
	key := base64.decode(msg.key)
	val := {
		"value": payload,
		"headers": msg.headers,
	}
}
# merge with old
transform[key] := val if {
	some key, val in data.messages
	every msg in input {
		key != base64.decode(msg.key) # incoming batch takes precedence
	}
}
`
			eopa := loadEnterpriseOPA(t, config, policy, image, network)

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
				resp, err := http.Get("http://" + eopa.GetHostPort("8181/tcp") + "/v1/data/messages")
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

func loadEnterpriseOPA(t *testing.T, config, policy, image string, network *docker.Network) *dockertest.Resource {
	img := strings.Split(image, ":")

	dir := t.TempDir()
	confPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(confPath, []byte(config), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}
	policyPath := filepath.Join(dir, "eval.rego")
	if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}

	eopa, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "eopa-e2e",
		Repository: img[0],
		Tag:        img[1],
		Hostname:   "eopa-e2e",
		NetworkID:  network.ID,
		Env: []string{
			"EOPA_LICENSE_TOKEN=" + os.Getenv("EOPA_LICENSE_TOKEN"),
			"EOPA_LICENSE_KEY=" + os.Getenv("EOPA_LICENSE_KEY"),
		},
		Mounts: []string{
			confPath + ":/config.yml",
			policyPath + ":/eval.rego",
		},
		ExposedPorts: []string{"8181/tcp"},
		Cmd:          strings.Split("run --server --addr :8181 --config-file /config.yml --log-level debug --disable-telemetry /eval.rego", " "),
	})
	if err != nil {
		t.Fatalf("could not start %s: %s", image, err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(eopa); err != nil {
			t.Fatalf("could not purge eopa: %s", err)
		}
	})

	if err := dockerPool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		req, err := http.NewRequest("GET", "http://localhost:"+eopa.GetPort("8181/tcp")+"/v1/data/system", nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return nil
	}); err != nil {
		t.Fatalf("could not connect to enterprise OPA: %s", err)
	}

	return eopa
}

func testKafka(t *testing.T, network *docker.Network) *dockertest.Resource {
	if res, found := dockerPool.ContainerByName("kafka-e2e"); found {
		_ = dockerPool.Purge(res)
	}
	kafkaResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "kafka-e2e",
		Repository: "bitnami/kafka",
		Tag:        "latest",
		NetworkID:  network.ID,
		Hostname:   "kafka-e2e",
		Env: []string{
			"BITNAMI_DEBUG=yes", // show an error if this config is wrong
			"KAFKA_BROKER_ID=1",
			"KAFKA_CFG_NODE_ID=1",
			"KAFKA_ENABLE_KRAFT=yes",
			"KAFKA_CFG_PROCESS_ROLES=broker,controller",
			"KAFKA_CFG_CONTROLLER_LISTENER_NAMES=CONTROLLER",
			"KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE=true",
			"KAFKA_CFG_LISTENERS=INTERNAL://kafka-e2e:9091,EXTERNAL://:9092,CONTROLLER://:9093", // INTERNAL is between docker containers; EXTERNAL is the exposed port
			"KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,EXTERNAL:PLAINTEXT,INTERNAL:PLAINTEXT",
			"KAFKA_CFG_ADVERTISED_LISTENERS=EXTERNAL://127.0.0.1:9092,INTERNAL://kafka-e2e:9091",
			"KAFKA_CFG_INTER_BROKER_LISTENER_NAME=INTERNAL",
			"KAFKA_CFG_CONTROLLER_QUORUM_VOTERS=1@127.0.0.1:9093",
			"ALLOW_PLAINTEXT_LISTENER=yes",
		},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"9092/tcp": {{HostIP: "localhost", HostPort: "9092/tcp"}}, // needed to have localhost:9092 work for kafkaClient
		},
		ExposedPorts: []string{"9092/tcp"},
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

func testRedPanda(t *testing.T, network *docker.Network) *dockertest.Resource {
	if res, found := dockerPool.ContainerByName("kafka-e2e"); found {
		_ = dockerPool.Purge(res)
	}
	kafkaResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "kafka-e2e",
		Repository: "redpandadata/redpanda",
		Tag:        "latest",
		NetworkID:  network.ID,
		Hostname:   "kafka-e2e",
		Env:        []string{},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"9092/tcp": {{HostIP: "localhost", HostPort: "9092/tcp"}}, // needed to have localhost:9092 work for kafkaClient
		},
		ExposedPorts: []string{"9092/tcp"},
		Cmd: strings.Split(`redpanda
			start
			--kafka-addr internal://0.0.0.0:9091,external://0.0.0.0:9092
			--advertise-kafka-addr internal://kafka-e2e:9091,external://localhost:9092
			--overprovisioned
			--seeds "kafka-e2e:33145"
			--set redpanda.empty_seed_starts_cluster=false
			--smp 1
			--memory 1G
			--reserve-memory 0M
			--check=false
			--advertise-rpc-addr kafka-e2e:33145
			--default-log-level=debug`, " \n\t"),
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

func kafkaClient() (*kgo.Client, error) {
	// logger := zerolog.New(os.Stderr) // for debugging
	logger := zerolog.New(io.Discard)

	opts := []kgo.Opt{
		kgo.SeedBrokers("localhost:9092"),
		kgo.WithLogger(kzerolog.New(&logger)),
		kgo.AllowAutoTopicCreation(),
	}
	return kgo.NewClient(opts...)
}

func cleanupPrevious(t *testing.T) {
	t.Helper()
	for _, n := range []string{"eopa-e2e", "kafka-e2e"} {
		if err := dockerPool.RemoveContainerByName(n); err != nil {
			t.Fatalf("remove %s: %v", n, err)
		}
	}
}

func matches(re string) func(string) bool {
	return regexp.MustCompile(re).MatchString
}
