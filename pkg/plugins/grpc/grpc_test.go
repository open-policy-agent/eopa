package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"testing"

	datav1 "github.com/styrainc/load-private/gen/proto/go/apis/data/v1"
	bjson "github.com/styrainc/load-private/pkg/json"
	inmem "github.com/styrainc/load-private/pkg/store"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func setupTest(t *testing.T, storeInput string) *bufconn.Listener {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	store := inmem.New()
	// Create the new store with the dummy data.
	if storeInput != "" {
		// OPA uses Go's standard JSON library but assumes that numbers have been
		// decoded as json.Number instead of float64. You MUST decode with UseNumber
		// enabled.
		decoder := json.NewDecoder(bytes.NewBufferString(storeInput))
		decoder.UseNumber()

		var data map[string]interface{}
		if err := decoder.Decode(&data); err != nil {
			panic(err)
		}
		store = inmem.NewFromObject(bjson.MustNew(data).(bjson.Object))
	}
	datav1.RegisterLoadServiceServer(s, &srv{store: store})
	reflection.Register(s)
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Server exited with error: %v", err)
		}
	}()
	t.Cleanup(func() {
		s.GracefulStop()
	})
	return lis
}

// Wonky function returning a closure for launching the custom dialer.
func GetBufDialer(listener *bufconn.Listener) func(context.Context, string) (net.Conn, error) {
	return func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
}

func TestReadData(t *testing.T) {
	// gRPC server setup/teardown boilerplate
	listener := setupTest(t, `{"a": 27}`)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewLoadServiceClient(conn)

	// Actual test:
	resp, err := client.ReadData(ctx, &datav1.ReadDataRequest{Path: "/a"})
	if err != nil {
		t.Fatalf("ReadData failed: %v", err)
	}
	// log.Printf("Response: %+v", resp)
	// Test for output:
	data := resp.GetData()
	if data != "27" {
		t.Fatalf("Expected 27, got: %v", data)
	}
}

func TestWriteData(t *testing.T) {
	// gRPC server setup/teardown boilerplate
	listener := setupTest(t, "")
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewLoadServiceClient(conn)

	// Actual test:
	resp, err := client.WriteData(ctx, &datav1.WriteDataRequest{Path: "/a", Operation: "add", Data: "4"})
	if err != nil {
		t.Fatalf("WriteData failed: %v", err)
	}
	// log.Printf("Response: %+v", resp)
	// Test for output:
	path := resp.GetPath()
	if path != "/a" {
		t.Fatalf("Expected path '/a', got: %v", path)
	}
}

func TestRWTransactionStream(t *testing.T) {
	// gRPC server setup/teardown boilerplate
	listener := setupTest(t, `{"a": 27}`)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewLoadServiceClient(conn)

	// Actual test:
	streamClient, err := client.RWDataTransactionStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Send 2x messages to the server, so the we can test its Recv loop.
	if err := streamClient.Send(&datav1.RWDataTransactionStreamRequest{
		Writes: []*datav1.WriteDataRequest{{Path: "/a", Operation: "add", Data: "4"}},
		Reads:  []*datav1.ReadDataRequest{{Path: "/a"}},
	}); err != nil {
		t.Fatalf("RWDataTransactionStream failed: %v", err)
	}
	if err := streamClient.Send(&datav1.RWDataTransactionStreamRequest{
		Writes: []*datav1.WriteDataRequest{{Path: "/a", Operation: "add", Data: "4"}},
		Reads:  []*datav1.ReadDataRequest{{Path: "/a"}},
	}); err != nil {
		t.Fatalf("RWDataTransactionStream failed: %v", err)
	}
	// Check to make sure we get 2x messages back.
	messagesReceived := 0
	for messagesReceived < 2 {
		in, err := streamClient.Recv()
		if err == io.EOF {
			break // reading done, server hung up.
		}
		messagesReceived++
		if err == nil {
			// Check message contents.
			writes := in.GetWrites()
			reads := in.GetReads()
			if len(writes) != 1 {
				t.Fatalf("Expected return writes of length 1, got: %v", writes)
			}
			if len(reads) != 1 {
				t.Fatalf("Expected return reads of length 1, got: %v", reads)
			}
			data := reads[0].GetData()
			if data != "4" {
				t.Fatalf("Expected 4, got: %v", data)
			}
		}
	}
	// Close the stream, so the server can gracefully disconnect.
	streamClient.CloseSend()
}
