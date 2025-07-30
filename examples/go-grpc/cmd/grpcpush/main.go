package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"time"

	datav1 "github.com/open-policy-agent/eopa/proto/gen/go/eopa/data/v1"
	policyv1 "github.com/open-policy-agent/eopa/proto/gen/go/eopa/policy/v1"
	"github.com/shirou/gopsutil/v3/mem"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

// Utility function for pushing over data from psutil.
func pushMemStats(ctx context.Context, client datav1.DataServiceClient, mem *mem.VirtualMemoryStat) error {
	// psutil provides JSON serialization using the .String() method, so we
	// take advantage of that for quickly converting into a protobuf Struct
	// type with the correct k/v pairs.
	var temp map[string]interface{}
	if err := json.Unmarshal([]byte(mem.String()), &temp); err != nil {
		return fmt.Errorf("could not unmarshal mem.VirtualMemoryStat struct")
	}
	pbStruct, err := structpb.NewStruct(temp)
	if err != nil {
		return fmt.Errorf("could not convert map[string]interface{} to protobuf struct format")
	}
	v := structpb.NewStructValue(pbStruct)
	// Create new Rego Object at path `data.psutil.memory` (URL: `/psutil/memory`).
	// The protobuf Struct's k/v pairs will map directly to the created
	// Rego Object's k/v pairs.
	if _, err := client.CreateData(ctx, &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/psutil/memory", Document: v}}); err != nil {
		return fmt.Errorf("CreateData failed: %v", err)
	}
	return nil
}

func main() {
	ctx := context.Background()
	addr := os.Getenv("ADDR")
	if addr == "" {
		fmt.Println("No ADDR environment variable found, defaulting to 'localhost:9090'")
		addr = "localhost:9090"
	}

	// Connect to the EOPA instance.
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to dial the EOPA server: %v", err)
	}
	defer conn.Close()
	clientData := datav1.NewDataServiceClient(conn)
	clientPolicy := policyv1.NewPolicyServiceClient(conn)

	// Create a new policy by reading the policy file in, and then pushing the policy up to the EOPA instance via gRPC.
	policy, err := os.ReadFile("policy.rego")
	if err != nil {
		log.Fatal(err)
	}
	_, err = clientPolicy.CreatePolicy(ctx, &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/example", Text: string(policy)}})
	if err != nil {
		log.Fatalf("CreatePolicy failed: %v", err)
	}

	// Query the service until the user hits Ctrl-C.
	for {
		// Query virtual memory stats with psutil, then push those stats up to the EOPA instance via gRPC.
		v, _ := mem.VirtualMemory()
		if err := pushMemStats(ctx, clientData, v); err != nil {
			log.Println(err)
		}

		// Get the re-formatted results by launching a query over gRPC.
		resp, err := clientData.GetData(ctx, &datav1.GetDataRequest{Path: "/example/vmem_summary"})
		if err != nil {
			log.Fatalf("GetData failed: %v", err)
		}
		resultDoc := resp.GetResult()
		path := resultDoc.GetPath()
		data := resultDoc.GetDocument()

		// Display the results, varying the logic by type of the returned values.
		fmt.Println(path)
		if data.GetStructValue() != nil {
			fields := data.GetStructValue().Fields
			// Sort the keys to keep a stable display order.
			keys := make([]string, 0, len(fields))
			for k := range fields {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			// Display each k/v pair.
			for _, k := range keys {
				v := fields[k]
				num := v.GetNumberValue()
				str := v.GetStringValue()
				if str != "" {
					fmt.Printf("k: %15s, \tv: %s\n", k, str)
				} else {
					fmt.Printf("k: %15s, \tv: %f.3\n", k, num)
				}
			}
		} else {
			// Error handling case.
			log.Fatal(data)
		}

		// Wait 1 second before querying again.
		time.Sleep(1 * time.Second)
	}
}
