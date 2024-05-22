package benthos_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/google/go-cmp/cmp"
	"github.com/testcontainers/testcontainers-go"
	tc_pulsar "github.com/testcontainers/testcontainers-go/modules/pulsar"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	_ "github.com/styrainc/enterprise-opa-private/pkg/rego_vm" // important! use VM for rego.Eval below
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

// NOTE(sr): created by running these commands in a "apache/pulsar" docker container:
// $ bin/pulsar tokens create-secret-key -o secret.key
// $ bin/pulsar tokens create -sk file:///pulsar/secret.key -s admin
// eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.DanjiED-2Aw_K96f__VNoHTtr7CW0ENYaX3zT3CHtWc
// $ bin/pulsar tokens validate -sk file:///pulsar/secret.key eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.DanjiED-2Aw_K96f__VNoHTtr7CW0ENYaX3zT3CHtWc
// {sub=admin}
// $ base64 < secret.key
// EpaPAaTyeQBXW3Gvv3fCzR4OW/G7iserFq7U5G3H0rg=
// $ bin/pulsar tokens validate -sk "data:;base64,EpaPAaTyeQBXW3Gvv3fCzR4OW/G7iserFq7U5G3H0rg=" eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.DanjiED-2Aw_K96f__VNoHTtr7CW0ENYaX3zT3CHtWc
// {sub=admin}
const (
	testSecretKey = "data:;base64,EpaPAaTyeQBXW3Gvv3fCzR4OW/G7iserFq7U5G3H0rg="
	testToken     = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.DanjiED-2Aw_K96f__VNoHTtr7CW0ENYaX3zT3CHtWc"
)

func TestBenthosPulsar(t *testing.T) {
	ctx := context.Background()
	topic, topic2 := "cipot", "btw"
	configPlain := `
plugins:
  data:
    pulsar:
      type: pulsar
      url: %[3]s
      topics: [%[1]s, %[2]s]
      rego_transform: "data.e2e.transform"
      # auth_token: %[4]s
` // avoid "EXTRA" in formatted string
	configToken := configPlain + `
      auth_token: %[4]s
`

	transform := `package e2e
import rego.v1

_payload(msg) := json.unmarshal(base64.decode(msg.value))
batch_ids contains _payload(msg).id if some msg in input.incoming

transform[payload.key] := val if {
	some msg in input.incoming
	payload := _payload(msg)
	val := {
		"payload": payload,
		"topic": msg.topic,
		"producer": msg.producer,
		"key": msg.key,
	}
}

transform[key] := val if {
    some key, val in input.previous
    not key in batch_ids
}
`

	tests := []struct {
		note       string
		configTmpl string
		pconfig    pulsar.ClientOptions
		extra      []testcontainers.ContainerCustomizer
	}{
		{
			note:       "plain",
			configTmpl: configPlain,
		},
		{
			note:       "token",
			configTmpl: configToken,
			pconfig:    pulsar.ClientOptions{Authentication: pulsar.NewAuthenticationToken(testToken)},
			extra: []testcontainers.ContainerCustomizer{
				tc_pulsar.WithPulsarEnv("authenticationEnabled", "true"),
				tc_pulsar.WithPulsarEnv("authorizationEnabled", "true"),
				tc_pulsar.WithPulsarEnv("tokenSecretKey", testSecretKey),
				tc_pulsar.WithPulsarEnv("authenticationProviders", "org.apache.pulsar.broker.authentication.AuthenticationProviderToken"),
				tc_pulsar.WithPulsarEnv("brokerClientAuthenticationPlugin", "org.apache.pulsar.client.impl.auth.AuthenticationToken"),
				tc_pulsar.WithPulsarEnv("brokerClientAuthenticationParameters", fmt.Sprintf(`{"token": "%s"}`, testToken)),
				tc_pulsar.WithPulsarEnv("superUserRoles", "admin"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {

			broker, tx := testPulsar(t, ctx, tc.extra...)
			t.Cleanup(func() { tx.Terminate(ctx) })

			store := storeWithPolicy(ctx, t, transform)
			mgr := pluginMgr(ctx, t, store, fmt.Sprintf(tc.configTmpl, topic, topic2, broker, testToken))

			conf := tc.pconfig
			conf.URL = broker
			cl, err := pulsar.NewClient(conf)
			if err != nil {
				t.Fatalf("pulsar client: %v", err)
			}

			// record written before we're consuming messages
			producer, err := cl.CreateProducer(pulsar.ProducerOptions{
				Name:  "producer-1",
				Topic: topic,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := producer.Send(ctx, &pulsar.ProducerMessage{
				Key:     "routing-0",
				Payload: []byte(`{"key": "one", "val": "foo"}`),
			}); err != nil {
				t.Fatalf("send first msg: %v", err)
			}
			defer producer.Close()

			if err := mgr.Start(ctx); err != nil {
				t.Fatal(err)
			}

			// record written while we're consuming, different topic
			producer2, err := cl.CreateProducer(pulsar.ProducerOptions{
				Name:  "producer-2",
				Topic: topic2,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := producer2.Send(ctx, &pulsar.ProducerMessage{
				Key:     "routing-key",
				Payload: []byte(`{"key": "two", "val": 123}`),
			}); err != nil {
				t.Fatalf("send second msg: %v", err)
			}

			waitForStorePath(ctx, t, store, "/pulsar/one")
			waitForStorePath(ctx, t, store, "/pulsar/two")

			{
				act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/pulsar/one"))
				if err != nil {
					t.Fatalf("read back data: %v", err)
				}
				exp := map[string]any{
					"payload": map[string]any{
						"key": "one",
						"val": "foo",
					},
					"topic":    "persistent://public/default/cipot",
					"producer": "producer-1",
					"key":      "routing-0",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("data value mismatch (-want +got):\n%s", diff)
				}
			}

			{
				act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/pulsar/two"))
				if err != nil {
					t.Fatalf("read back data: %v", err)
				}
				exp := map[string]any{
					"payload": map[string]any{
						"key": "two",
						"val": json.Number("123"),
					},
					"topic":    "persistent://public/default/btw",
					"producer": "producer-2",
					"key":      "routing-key",
				}
				if diff := cmp.Diff(exp, act); diff != "" {
					t.Errorf("data value mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestBenthosPulsarOwned(t *testing.T) {
	ctx := context.Background()
	topic := "cipot"
	config := `
plugins:
  data:
    pulsar.messages:
      type: pulsar
      url: %[2]s
      topics: [%[1]s]
      rego_transform: "data.e2e.transform"
`

	transform := `package e2e
import rego.v1
transform[msg.key] := msg if some msg in input.incoming
`
	broker, tx := testPulsar(t, ctx)
	t.Cleanup(func() { tx.Terminate(ctx) })

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(ctx, t, store, fmt.Sprintf(config, topic, broker))
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// test owned path
	err := storage.WriteOne(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/pulsar/messages"), map[string]any{"foo": "bar"})
	if err == nil || err.Error() != `path "/pulsar/messages" is owned by plugin "pulsar"` {
		t.Errorf("owned check failed, got %v", err)
	}
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

func testPulsar(t *testing.T, ctx context.Context, cs ...testcontainers.ContainerCustomizer) (string, *tc_pulsar.Container) {
	tc, err := tc_pulsar.RunContainer(ctx,
		append(cs,
			testcontainers.WithImage("docker.io/apachepulsar/pulsar:3.2.2"),
			testcontainers.WithWaitStrategy(
				wait.ForAll(
					wait.ForHTTP("/admin/v2/clusters").
						WithHeaders(map[string]string{
							"Authorization": "Bearer " + testToken,
						}).
						WithPort("8080/tcp").
						WithStatusCodeMatcher(func(status int) bool { return status == 200 }).
						WithResponseMatcher(func(r io.Reader) bool {
							respBytes, _ := io.ReadAll(r)
							resp := string(respBytes)
							return resp == `["standalone"]`
						}),
					wait.ForLog("Created namespace public/default"),
				),
			),
		)...)
	if err != nil {
		t.Fatalf("could not start pulsar: %s", err)
	}
	broker, err := tc.BrokerURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return broker, tc
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
	if testing.Verbose() {
		logger := logging.New()
		logger.SetLevel(logging.Debug)
		opts = append(opts, plugins.Logger(logger))
		opts = append(opts, plugins.ConsoleLogger(logger))
	} else {
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
