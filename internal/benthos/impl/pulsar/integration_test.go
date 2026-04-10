package pulsar

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/ory/dockertest/v4"
	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/benthos/v4/public/service/integration"
)

func TestIntegrationPulsar(t *testing.T) {
	integration.CheckSkip(t)
	t.Parallel()

	pool := dockertest.NewPoolT(t, "")
	dockertest.WithMaxWait(time.Minute * 2)

	resource := pool.RunT(t, "apachepulsar/pulsar-standalone", dockertest.WithTag("2.8.3"), nil)

	require.NoError(t, pool.Retry(context.TODO(), 900*time.Second, func() error {
		client, err := pulsar.NewClient(pulsar.ClientOptions{
			URL:    fmt.Sprintf("pulsar://localhost:%v/", resource.GetPort("6650/tcp")),
			Logger: NoopLogger(),
		})
		if err != nil {
			return err
		}
		prod, err := client.CreateProducer(pulsar.ProducerOptions{
			Topic: "benthos-connection-test",
		})
		if err == nil {
			prod.Close()
		}
		client.Close()
		return err
	}))

	template := `
output:
  pulsar:
    url: pulsar://localhost:$PORT/
    topic: "topic-$ID"
    max_in_flight: $MAX_IN_FLIGHT

input:
  pulsar:
    url: pulsar://localhost:$PORT/
    topics: [ "topic-$ID" ]
    subscription_name: "sub-$ID"
`

	patternTemplate := `
output:
  pulsar:
    url: pulsar://localhost:$PORT/
    topic: "topic-$ID"
    max_in_flight: $MAX_IN_FLIGHT

input:
  pulsar:
    url: pulsar://localhost:$PORT/
    topics_pattern: "t.*c-$ID"
    subscription_name: "sub-$ID"
`
	suite := integration.StreamTests(
		integration.StreamTestOpenClose(),
		integration.StreamTestSendBatch(10),
		integration.StreamTestStreamSequential(1000),
		integration.StreamTestStreamParallel(1000),
		integration.StreamTestStreamParallelLossy(1000),
		integration.StreamTestStreamParallelLossyThroughReconnect(1000),
		integration.StreamTestAtLeastOnceDelivery(),
	)

	suite.Run(
		t, template,
		integration.StreamTestOptSleepAfterInput(500*time.Millisecond),
		integration.StreamTestOptSleepAfterOutput(500*time.Millisecond),
		integration.StreamTestOptPort(resource.GetPort("6650/tcp")),
	)

	t.Run("with topics pattern", func(t *testing.T) {
		suite.Run(
			t, patternTemplate,
			integration.StreamTestOptSleepAfterInput(500*time.Millisecond),
			integration.StreamTestOptSleepAfterOutput(500*time.Millisecond),
			integration.StreamTestOptPort(resource.GetPort("6650/tcp")),
		)
	})

	t.Run("with max in flight", func(t *testing.T) {
		t.Parallel()
		suite.Run(
			t, template,
			integration.StreamTestOptSleepAfterInput(500*time.Millisecond),
			integration.StreamTestOptSleepAfterOutput(500*time.Millisecond),
			integration.StreamTestOptPort(resource.GetPort("6650/tcp")),
			integration.StreamTestOptMaxInFlight(10),
		)
	})
}
