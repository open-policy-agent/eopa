// nolint
package grpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/util"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	grpc_plugin "github.com/styrainc/enterprise-opa-private/pkg/plugins/grpc"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
	datav1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/data/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	protocmp "google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	defaultBufSize    = 1024 * 1024
	defaultGRPCConfig = `---
plugins:
  grpc:
    addr: ":9090"
`
)

func setupTest(t *testing.T, config, storeDataInput string, storePolicyInputs map[string]string) *bufconn.Listener {
	t.Helper()
	// Note(philip): This wrapper allows us to instantiate the plugin
	// almost entirely without a plugin manager in the mix, allowing direct
	// control, and some low-level hacks for nicer testing, like shimming
	// in the bufconn listener instead of a normal socket listener.
	type Wrapper struct {
		Plugins struct {
			GRPC grpc_plugin.Config `json:"grpc"`
		} `json:"plugins"`
	}
	wrappedConfig := Wrapper{}
	if err := util.Unmarshal([]byte(config), &wrappedConfig); err != nil {
		t.Fatal(err)
	}
	pluginConfig := wrappedConfig.Plugins.GRPC
	lis := bufconn.Listen(defaultBufSize)
	pluginConfig.SetListener(lis)

	store := inmem.New()
	// Create the new store with the dummy data.
	if storeDataInput != "" {
		// OPA uses Go's standard JSON library but assumes that numbers have been
		// decoded as json.Number instead of float64. You MUST decode with UseNumber
		// enabled.
		decoder := json.NewDecoder(bytes.NewBufferString(storeDataInput))
		decoder.UseNumber()

		var data map[string]interface{}
		if err := decoder.Decode(&data); err != nil {
			t.Fatal(err)
		}
		store = inmem.NewFromObject(bjson.MustNew(data).(bjson.Object))
	}
	// Add policies to the store.
	if len(storePolicyInputs) > 0 {
		ctx := context.Background()
		txn, err := store.NewTransaction(ctx, storage.WriteParams)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range storePolicyInputs {
			err := store.UpsertPolicy(ctx, txn, k, []byte(v))
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Commit(ctx, txn); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	mgr := pluginMgr(ctx, t, store, config)
	// Note(philip): This Init() step is important-- without it, the
	// manager never initializes its compiler instance, causing chaos down
	// the road.
	if err := mgr.Init(context.TODO()); err != nil {
		t.Fatal(err)
	}
	// Note(philip): In the past, we actually created an instance of
	// grpc_plugin.Server, and worked off of that. However, to get proper
	// compiler/store triggers when policies update, we need to work at the
	// plugin level now, because the trigger lifetimes are managed via the
	// plugin's Start/Stop methods.
	p := grpc_plugin.Factory().New(mgr, pluginConfig)

	go func() {
		p.Start(context.TODO())
	}()
	t.Cleanup(func() {
		p.Stop(context.TODO())
	})
	return lis
}

// Wonky function returning a closure for launching the custom dialer.
func GetBufDialer(listener *bufconn.Listener) func(context.Context, string) (net.Conn, error) {
	return func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
}

// Borrowed from the Kafka data source plugin:
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
		discovery.Factories(map[string]plugins.Factory{
			grpc_plugin.PluginName: grpc_plugin.Factory(),
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}

// Note(philip): This test unfortunately also requires wiring in the GetData
// method, so that we can check that the value was stored correctly.
func TestCreateData(t *testing.T) {
	// gRPC server setup/teardown boilerplate
	listener := setupTest(t, defaultGRPCConfig, `{}`, nil)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewDataServiceClient(conn)

	// Create new data store item.
	{
		_, err := client.CreateData(ctx, &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}})
		if err != nil {
			t.Fatalf("CreateData failed: %v", err)
		}
	}
	// Fetch down the new data item.
	{
		resp, err := client.GetData(ctx, &datav1.GetDataRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetData failed: %v", err)
		}
		resultDoc := resp.GetResult()
		path := resultDoc.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		data := resultDoc.GetDocument()
		if data.GetNumberValue() != 27 {
			t.Fatalf("Expected 27, got: %v", data)
		}
	}
}

// We pre-populate the store with a base document (non-Rego rule) value, and query it.
func TestGetDataBaseDocument(t *testing.T) {
	// gRPC server setup/teardown boilerplate
	listener := setupTest(t, defaultGRPCConfig, `{"a": 27}`, nil)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewDataServiceClient(conn)

	// Fetch down the data item.
	{
		resp, err := client.GetData(ctx, &datav1.GetDataRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetData failed: %v", err)
		}
		resultDoc := resp.GetResult()
		path := resultDoc.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		data := resultDoc.GetDocument()
		if data.GetNumberValue() != 27 {
			t.Fatalf("Expected 27, got: %v", data)
		}
	}
}

func TestUpdateData(t *testing.T) {
	// gRPC server setup/teardown boilerplate
	listener := setupTest(t, defaultGRPCConfig, `{"a": 27}`, nil)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewDataServiceClient(conn)

	// Update the data item.
	{
		_, err := client.UpdateData(ctx, &datav1.UpdateDataRequest{Op: datav1.PatchOp_PATCH_OP_REPLACE, Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(4)}})
		if err != nil {
			t.Fatalf("UpdateData failed: %v", err)
		}
	}
	// Fetch down the altered data item.
	{
		resp, err := client.GetData(ctx, &datav1.GetDataRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetData failed: %v", err)
		}
		resultDoc := resp.GetResult()
		path := resultDoc.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		data := resultDoc.GetDocument()
		if data.GetNumberValue() != 4 {
			t.Fatalf("Expected 4, got: %v", data)
		}
	}
}

func TestDeleteData(t *testing.T) {
	// gRPC server setup/teardown boilerplate
	listener := setupTest(t, defaultGRPCConfig, `{"a": 27}`, nil)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewDataServiceClient(conn)

	// Delete the data item.
	{
		_, err := client.DeleteData(ctx, &datav1.DeleteDataRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("DeleteData failed: %v", err)
		}
	}
	// Try fetching the deleted data item.
	{
		resp, err := client.GetData(ctx, &datav1.GetDataRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetData failed: %v", err)
		}
		resultDoc := resp.GetResult()
		path := resultDoc.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		data := resultDoc.GetDocument()
		if data.GetStringValue() != "" {
			t.Fatalf("Expected \"\", got: %v", data)
		}
	}
}

// >4 MB messages are often a problem for gRPC servers.
// This test requires that both client and server be able to receive larger
// messages than normal (in this case, an 8 MB max size).
func TestCreateDataSendLargerThan4MB(t *testing.T) {
	megabyteString := strings.Repeat("A", 1024*1024)
	alternateGRPCConfig := `---
plugins:
  grpc:
    max_recv_message_size: 8589934592
    addr: ":9090"
`
	// gRPC server setup/teardown boilerplate
	listener := setupTest(t, alternateGRPCConfig, `{}`, nil)
	ctx := context.Background()
	// We up our receive size here so that we can check that the large
	// message was stored correctly.
	conn, err := grpc.DialContext(ctx,
		"bufnet",
		grpc.WithContextDialer(GetBufDialer(listener)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(8589934592)))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewDataServiceClient(conn)

	// Build an 8+ MB protobuf struct:
	bigStruct, err := structpb.NewStruct(map[string]interface{}{
		"0": megabyteString,
		"1": megabyteString,
		"2": megabyteString,
		"3": megabyteString,
		"4": megabyteString,
		"5": megabyteString,
	})
	if err != nil {
		t.Fatalf("struct creation failed: %v", err)
	}

	// Create new data store large data item.
	{
		_, err := client.CreateData(ctx, &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewStructValue(bigStruct)}})
		if err != nil {
			t.Fatalf("CreateData failed: %v", err)
		}
	}
	// Fetch down the new large data item.
	{
		resp, err := client.GetData(ctx, &datav1.GetDataRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetData failed: %v", err)
		}
		resultDoc := resp.GetResult()
		path := resultDoc.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		data := resultDoc.GetDocument()
		s := data.GetStructValue()
		if s.Fields["0"].GetStringValue() != megabyteString {
			t.Fatalf("Expected %s, got: %v", megabyteString, s.Fields["0"])
		}
	}
}

// Sequential request / response tests.
// Ported over from `TestBulkRWSeq“ in `grpc_bulk_test.go` .
func TestStreamingDataRWSeq(t *testing.T) {
	type StreamingDataRWSeqStep struct {
		request     *datav1.StreamingDataRWRequest
		expResponse *datav1.StreamingDataRWResponse
		expErr      error
	}

	tests := []struct {
		note      string
		storeData string
		steps     []StreamingDataRWSeqStep
	}{
		{
			// Inspired by a bug Miro found in the v0.100.8 release.
			note: "Multiple empty requests",
			steps: []StreamingDataRWSeqStep{
				{
					request:     &datav1.StreamingDataRWRequest{},
					expResponse: &datav1.StreamingDataRWResponse{},
				},
				{
					request:     &datav1.StreamingDataRWRequest{},
					expResponse: &datav1.StreamingDataRWResponse{},
				},
				{
					request:     &datav1.StreamingDataRWRequest{},
					expResponse: &datav1.StreamingDataRWResponse{},
				},
			},
		},
		{
			note: "Multiple data writes",
			steps: []StreamingDataRWSeqStep{
				{
					request: &datav1.StreamingDataRWRequest{
						Writes: []*datav1.StreamingDataRWRequest_WriteRequest{
							{Req: &datav1.StreamingDataRWRequest_WriteRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}}},
						},
					},
					expResponse: &datav1.StreamingDataRWResponse{
						Writes: []*datav1.StreamingDataRWResponse_WriteResponse{
							{Resp: &datav1.StreamingDataRWResponse_WriteResponse_Create{Create: &datav1.CreateDataResponse{}}},
						},
					},
				},
				{
					request: &datav1.StreamingDataRWRequest{
						Writes: []*datav1.StreamingDataRWRequest_WriteRequest{
							{Req: &datav1.StreamingDataRWRequest_WriteRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(28)}}}},
						},
					},
					expResponse: &datav1.StreamingDataRWResponse{
						Writes: []*datav1.StreamingDataRWResponse_WriteResponse{
							{Resp: &datav1.StreamingDataRWResponse_WriteResponse_Create{Create: &datav1.CreateDataResponse{}}},
						},
					},
				},
				{
					request: &datav1.StreamingDataRWRequest{
						Writes: []*datav1.StreamingDataRWRequest_WriteRequest{
							{Req: &datav1.StreamingDataRWRequest_WriteRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(29)}}}},
						},
					},
					expResponse: &datav1.StreamingDataRWResponse{
						Writes: []*datav1.StreamingDataRWResponse_WriteResponse{
							{Resp: &datav1.StreamingDataRWResponse_WriteResponse_Create{Create: &datav1.CreateDataResponse{}}},
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		// We do the full setup/teardown for every test, or else we'd get
		// collisions between testcases due to statefulness.
		t.Run(tc.note, func(t *testing.T) {
			storeData := "{}"
			if tc.storeData != "" {
				storeData = tc.storeData
			}
			listener := setupTest(t, defaultGRPCConfig, storeData, nil)
			ctx := context.Background()
			conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("Failed to dial bufnet: %v", err)
			}
			defer conn.Close()
			client := datav1.NewDataServiceClient(conn)
			sclient, err := client.StreamingDataRW(ctx)
			if err != nil {
				t.Fatal(err)
			}

			// Run each test's steps in sequence on the live service instance.
			for _, step := range tc.steps {
				// Send message...
				if err := sclient.Send(step.request); err != nil {
					// No error expected? Fail test.
					if step.expErr == nil {
						t.Fatalf("[%s] Unexpected error: %v", tc.note, err)
					}
					// Error expected? Was it the right one?
					if !strings.Contains(err.Error(), step.expErr.Error()) {
						t.Fatalf("[%s] Expected error: %v, got: %v", tc.note, step.expErr, err)
					}
				}
				// ...See what we got in response.
				resp, err := sclient.Recv()
				if err != nil {
					t.Fatal(err)
				}
				// Check value equality of expected vs actual response.
				if !cmp.Equal(step.expResponse, resp, protocmp.Transform()) {
					fmt.Println("Diff:\n", cmp.Diff(step.expResponse, resp, protocmp.Transform()))
					t.Fatalf("[%s] Expected:\n%v\n\nGot:\n%v", tc.note, step.expResponse, resp)
				}
			}

			// Send the close message, and make sure there were no errors.
			if err := sclient.CloseSend(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
