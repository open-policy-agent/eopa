package grpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"testing"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/discovery"
	grpc_plugin "github.com/styrainc/enterprise-opa-private/pkg/plugins/grpc"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
	datav1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/data/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

const bufSize = 1024 * 1024

func setupTest(t *testing.T, storeDataInput string, storePolicyInputs map[string]string) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(bufSize)
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
	mgr := pluginMgr(ctx, t, store, storeDataInput)
	configInterface, err := grpc_plugin.Factory().Validate(mgr, []byte("addr: :9090")) // Note(philip): This is a no-op right now.
	if err != nil {
		log.Fatal(err)
	}
	config := configInterface.(grpc_plugin.Config)
	config.SetListener(lis)
	// Note(philip): In the past, we actually created an instance of
	// grpc_plugin.Server, and worked off of that. However, to get proper
	// compiler/store triggers when policies update, we need to work at the
	// plugin level now, because the trigger lifetimes are managed via the
	// plugin's Start/Stop methods.
	p := grpc_plugin.Factory().New(mgr, config)

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
			data.Name:              data.Factory(),
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
	listener := setupTest(t, `{}`, nil)
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
	listener := setupTest(t, `{"a": 27}`, nil)
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
	listener := setupTest(t, `{"a": 27}`, nil)
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
	listener := setupTest(t, `{"a": 27}`, nil)
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
