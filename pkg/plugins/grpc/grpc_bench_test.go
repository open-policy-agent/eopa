package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"testing"

	datav1 "github.com/styrainc/load-private/gen/proto/go/apis/data/v1"
	bjson "github.com/styrainc/load-private/pkg/json"
	inmem "github.com/styrainc/load-private/pkg/store"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"
)

func setupBenchmarkTest(b *testing.B, storeInput string) *bufconn.Listener {
	b.Helper()
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
	b.Cleanup(func() {
		s.GracefulStop()
	})
	return lis
}

func BenchmarkGRPCReadSingle(b *testing.B) {
	// gRPC server setup/teardown boilerplate
	listener := setupBenchmarkTest(b, `{"a": 27}`)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewLoadServiceClient(conn)

	sizes := []int{1, 10, 100, 1000, 10000}

	for _, n := range sizes {
		b.ResetTimer()
		b.Run(fmt.Sprintf("1x%d", n), func(b *testing.B) {
			for i := 0; i < n; i++ {
				resp, err := client.ReadData(ctx, &datav1.ReadDataRequest{Path: "/a"})
				if err != nil {
					b.Fatalf("ReadData failed: %v", err)
				}
				data := resp.GetData()
				if data != "27" {
					b.Fatalf("Expected 27, got: %v", data)
				}
			}
		})
	}
}

func BenchmarkGRPCWriteSingle(b *testing.B) {
	// gRPC server setup/teardown boilerplate
	listener := setupBenchmarkTest(b, `{"a": 27}`)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewLoadServiceClient(conn)

	sizes := []int{1, 10, 100, 1000, 10000}

	for _, n := range sizes {
		b.ResetTimer()
		b.Run(fmt.Sprintf("1x%d", n), func(b *testing.B) {
			for i := 0; i < n; i++ {
				resp, err := client.WriteData(ctx, &datav1.WriteDataRequest{Path: "/a", Operation: "add", Data: "4"})
				if err != nil {
					b.Fatalf("WriteData failed: %v", err)
				}
				// log.Printf("Response: %+v", resp)
				// Test for output:
				path := resp.GetPath()
				if path != "/a" {
					b.Fatalf("Expected path '/a', got: %v", path)
				}
			}
		})
	}
}

func BenchmarkRWTransactionStream(b *testing.B) {
	// gRPC server setup/teardown boilerplate
	listener := setupBenchmarkTest(b, `{"a": 27}`)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := datav1.NewLoadServiceClient(conn)

	// Actual test:
	streamClient, err := client.RWDataTransactionStream(ctx)
	if err != nil {
		b.Fatal(err)
	}

	numMessages := []int{1, 10, 100, 1000}
	numRWOps := []int{1, 10, 100, 1000}

	for _, n := range numMessages {
		for _, m := range numRWOps {
			b.ResetTimer()
			b.Run(fmt.Sprintf("%dx%d", n, m), func(b *testing.B) {
				go func() {
					for i := 0; i < n; i++ {
						// Build up read/write list.
						clientWrites := make([]*datav1.WriteDataRequest, 0, m)
						clientReads := make([]*datav1.ReadDataRequest, 0, m)
						for j := 0; j < m; j++ {
							clientWrites = append(clientWrites, &datav1.WriteDataRequest{Path: "/a", Operation: "add", Data: "4"})
							clientReads = append(clientReads, &datav1.ReadDataRequest{Path: "/a"})
						}
						if err := streamClient.Send(&datav1.RWDataTransactionStreamRequest{
							Writes: clientWrites,
							Reads:  clientReads,
						}); err != nil {
							panic(fmt.Errorf("RWDataTransactionStream failed: %v", err))
						}
					}
				}()
				messagesReceived := 0
				for messagesReceived < n {
					in, err := streamClient.Recv()
					if err == io.EOF {
						break // reading done, server hung up.
					}
					messagesReceived++
					if err == nil {
						// Check message contents.
						writes := in.GetWrites()
						reads := in.GetReads()
						if len(writes) != m {
							b.Fatalf("Expected return writes of length 1, got: %v", writes)
						}
						if len(reads) != m {
							b.Fatalf("Expected return reads of length 1, got: %v", reads)
						}
						data := reads[0].GetData()
						if data != "4" {
							b.Fatalf("Expected 4, got: %v", data)
						}
					}
				}
			})
		}
	}
}
