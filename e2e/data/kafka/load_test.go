//go:build e2e

package kafka

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/redpanda-data/benthos/v4/public/service"
	_ "github.com/redpanda-data/connect/v4/public/components/kafka"
	_ "github.com/redpanda-data/connect/v4/public/components/pure"          // "generate" input
	_ "github.com/redpanda-data/connect/v4/public/components/pure/extended" // fake() builtin

	"github.com/testcontainers/testcontainers-go"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

//go:embed transform.rego
var policy []byte

//go:embed benthos-kafka.yml
var benthosConfig []byte

func Test100kMessages(t *testing.T) {
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

			config := fmt.Sprintf(`
plugins:
  data:
    messages:
      type: kafka
      urls: [%[1]s]
      topics: [msgs]
      rego_transform: "data.e2e.transform"
`, broker)

			// first, we fill the topic up!
			benthos(t, broker)

			// then, we start EOPA and see if it can catch up
			eopa, eopaErr := eopaRun(t, config, string(policy), eopaHTTPPort)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			if err := wait.Func(func() bool {
				// check store response (TODO: check metrics/status when we have them)
				resp, err := utils.StdlibHTTPClient.Get(fmt.Sprintf("http://localhost:%d/v1/data/messages/count", eopaHTTPPort))
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
				count, ok := act["result"].(float64)
				t.Logf("count: %d", int(count))
				return ok && count == 100_000
			}, 50*time.Millisecond, 15*time.Second); err != nil {
				t.Error(err)
			}

			users := map[string]any{}
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/v1/data/messages/users", eopaHTTPPort))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(body, &users); err != nil {
				t.Fatal(err)
			}
			users = users["result"].(map[string]any)
			if len(users) < 100 || len(users) > 1000 {
				t.Errorf("expected users count between 100 and 1000, got %d results", len(users))
			}
		})
	}
}

func benthos(t *testing.T, broker string) {
	t.Setenv("BENTHOS_BROKER", broker)
	builder := service.NewStreamBuilder()
	builder.SetPrintLogger(&wrap{t})
	if err := builder.SetYAML(string(benthosConfig)); err != nil {
		t.Fatal(err)
	}
	stream, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type wrap struct {
	t *testing.T
}

func (w wrap) Println(v ...any) {
	line := strings.Builder{}
	for i := range v {
		if i != 0 {
			line.WriteString(" ")
		}
		fmt.Fprintf(&line, "%v", v[i])
	}
	w.t.Log(line.String())
}

func (w wrap) Printf(f string, v ...any) {
	w.t.Logf(strings.TrimRight(f, "\n"), v...)
}
