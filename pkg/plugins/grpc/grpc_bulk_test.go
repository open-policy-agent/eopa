package grpc_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	loadv1 "github.com/styrainc/load-private/proto/gen/go/load/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	cmp "github.com/google/go-cmp/cmp"
	protocmp "google.golang.org/protobuf/testing/protocmp"
)

// Because the Transaction service is essentially the product of the data /
// policy endpoints, the testing surface is quite large. We have a few
// basic "smoke test" testcases, and then focus on testing the new
// functionality, namely:
//   - sequential bulk writes
//   - bulk read/query operations
func TestBulkRW(t *testing.T) {
	tests := []struct {
		note        string
		storeData   string
		storePolicy map[string]string
		request     *loadv1.BulkRWRequest
		expResponse *loadv1.BulkRWResponse
		expErr      error
	}{
		// Data writes (single req/resp).
		{
			note: "single data create",
			request: &loadv1.BulkRWRequest{
				WritesData: []*loadv1.BulkRWRequest_WriteDataRequest{
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Create{Create: &loadv1.CreateDataRequest{Path: "/a", Data: []byte("27")}}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				WritesData: []*loadv1.BulkRWResponse_WriteDataResponse{
					{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Create{Create: &loadv1.CreateDataResponse{}}},
				},
			},
		},
		{
			note:      "single data update",
			storeData: `{"a": 27}`,
			request: &loadv1.BulkRWRequest{
				WritesData: []*loadv1.BulkRWRequest_WriteDataRequest{
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Update{Update: &loadv1.UpdateDataRequest{Path: "/a", Data: []byte("27")}}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				WritesData: []*loadv1.BulkRWResponse_WriteDataResponse{
					{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Update{Update: &loadv1.UpdateDataResponse{}}},
				},
			},
		},
		{
			note:      "single data delete",
			storeData: `{"a": 27}`,
			request: &loadv1.BulkRWRequest{
				WritesData: []*loadv1.BulkRWRequest_WriteDataRequest{
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Delete{Delete: &loadv1.DeleteDataRequest{Path: "/a"}}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				WritesData: []*loadv1.BulkRWResponse_WriteDataResponse{
					{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Delete{Delete: &loadv1.DeleteDataResponse{}}},
				},
			},
		},
		// Policy writes (single req/resp).
		{
			note: "single policy create",
			request: &loadv1.BulkRWRequest{
				WritesPolicy: []*loadv1.BulkRWRequest_WritePolicyRequest{
					{Req: &loadv1.BulkRWRequest_WritePolicyRequest_Create{Create: &loadv1.CreatePolicyRequest{Path: "/a", Policy: []byte("package a\n\nx { true }\ny { false }\n")}}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				WritesPolicy: []*loadv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &loadv1.BulkRWResponse_WritePolicyResponse_Create{Create: &loadv1.CreatePolicyResponse{}}},
				},
			},
		},
		{
			note: "single policy update",
			storePolicy: map[string]string{
				"/a": "package a\n\nx { false }\ny { false }\n",
			},
			request: &loadv1.BulkRWRequest{
				WritesPolicy: []*loadv1.BulkRWRequest_WritePolicyRequest{
					{Req: &loadv1.BulkRWRequest_WritePolicyRequest_Create{Create: &loadv1.CreatePolicyRequest{Path: "/a", Policy: []byte("package a\n\nx { true }\ny { false }\n")}}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				WritesPolicy: []*loadv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &loadv1.BulkRWResponse_WritePolicyResponse_Create{Create: &loadv1.CreatePolicyResponse{}}},
				},
			},
		},
		{
			note: "single policy delete",
			storePolicy: map[string]string{
				"/a": "package a\n\nx { true }\ny { false }\n",
			},
			request: &loadv1.BulkRWRequest{
				WritesPolicy: []*loadv1.BulkRWRequest_WritePolicyRequest{
					{Req: &loadv1.BulkRWRequest_WritePolicyRequest_Create{Create: &loadv1.CreatePolicyRequest{Path: "/a", Policy: []byte("package a\n\nx { true }\ny { false }\n")}}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				WritesPolicy: []*loadv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &loadv1.BulkRWResponse_WritePolicyResponse_Create{Create: &loadv1.CreatePolicyResponse{}}},
				},
			},
		},
		// Data reads (single req/resp).
		{
			note:      "single data read",
			storeData: `{"a": 27}`,
			request: &loadv1.BulkRWRequest{
				ReadsData: []*loadv1.BulkRWRequest_ReadDataRequest{
					{Req: &loadv1.GetDataRequest{Path: "/a"}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				ReadsData: []*loadv1.BulkRWResponse_ReadDataResponse{
					{Resp: &loadv1.GetDataResponse{Path: "/a", Result: []byte("27")}},
				},
			},
		},
		// Policy reads (single req/resp).
		{
			note: "single policy read",
			storePolicy: map[string]string{
				"/a": "package a\n\nx { true }\ny { false }\n",
			},
			request: &loadv1.BulkRWRequest{
				ReadsPolicy: []*loadv1.BulkRWRequest_ReadPolicyRequest{
					{Req: &loadv1.GetPolicyRequest{Path: "/a"}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				ReadsPolicy: []*loadv1.BulkRWResponse_ReadPolicyResponse{
					{Resp: &loadv1.GetPolicyResponse{Path: "/a", Result: []byte("{\"ast\":null,\"id\":\"/a\",\"raw\":\"package a\\n\\nx { true }\\ny { false }\\n\"}")}},
				},
			},
		},
		// Bulk, sequential writes + reads to check for ordering.
		{
			note: "gradual object construction + policy + reads from base/virtual documents",
			request: &loadv1.BulkRWRequest{
				WritesData: []*loadv1.BulkRWRequest_WriteDataRequest{
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Create{Create: &loadv1.CreateDataRequest{Path: "/a", Data: []byte("27")}}},
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Create{Create: &loadv1.CreateDataRequest{Path: "/b", Data: []byte(`{"c": 1, "d": 2, "e": 3}`)}}},
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Update{Update: &loadv1.UpdateDataRequest{Path: "/b/d", Data: []byte("10")}}},
				},
				WritesPolicy: []*loadv1.BulkRWRequest_WritePolicyRequest{
					{Req: &loadv1.BulkRWRequest_WritePolicyRequest_Create{Create: &loadv1.CreatePolicyRequest{Path: "/test", Policy: []byte("package test\n\nx { true }\ny = false\nz = data.a + data.b.c + data.b.d\n")}}},
				},
				ReadsData: []*loadv1.BulkRWRequest_ReadDataRequest{
					{Req: &loadv1.GetDataRequest{Path: "/test/x"}},
					{Req: &loadv1.GetDataRequest{Path: "/test/y"}},
					{Req: &loadv1.GetDataRequest{Path: "/test/z"}},
				},
				ReadsPolicy: []*loadv1.BulkRWRequest_ReadPolicyRequest{
					{Req: &loadv1.GetPolicyRequest{Path: "/test"}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				WritesData: []*loadv1.BulkRWResponse_WriteDataResponse{
					{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Create{Create: &loadv1.CreateDataResponse{}}},
					{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Create{Create: &loadv1.CreateDataResponse{}}},
					{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Update{Update: &loadv1.UpdateDataResponse{}}},
				},
				WritesPolicy: []*loadv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &loadv1.BulkRWResponse_WritePolicyResponse_Create{Create: &loadv1.CreatePolicyResponse{}}},
				},
				ReadsData: []*loadv1.BulkRWResponse_ReadDataResponse{
					{Resp: &loadv1.GetDataResponse{Path: "/test/x", Result: []byte("true")}},
					{Resp: &loadv1.GetDataResponse{Path: "/test/y", Result: []byte("false")}},
					{Resp: &loadv1.GetDataResponse{Path: "/test/z", Result: []byte("38")}},
				},
				ReadsPolicy: []*loadv1.BulkRWResponse_ReadPolicyResponse{
					{Resp: &loadv1.GetPolicyResponse{Path: "/test", Result: []byte("{\"ast\":null,\"id\":\"/test\",\"raw\":\"package test\\n\\nx { true }\\ny = false\\nz = data.a + data.b.c + data.b.d\\n\"}")}},
				},
			},
		},
		// -------------------------------------------------------------------
		// Error cases
		// Note(philip): These are not exhaustive, but try to hit the new
		// functionality this endpoint introduces:
		// - Any write failure aborts the whole transaction.
		// - Read failures are reported inline, and do not fail the transaction.
		{
			note: "gradual object construction + policy + reads from base/virtual documents",
			request: &loadv1.BulkRWRequest{
				WritesData: []*loadv1.BulkRWRequest_WriteDataRequest{
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Create{Create: &loadv1.CreateDataRequest{Path: "/a", Data: []byte("27")}}},
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Delete{Delete: &loadv1.DeleteDataRequest{Path: "/b"}}}, // will fail because of non-existent path.
				},
				ReadsData: []*loadv1.BulkRWRequest_ReadDataRequest{
					{Req: &loadv1.GetDataRequest{Path: "/a"}},
				},
			},
			expErr: fmt.Errorf("rpc error: code = NotFound desc = storage_not_found_error: /b: document does not exist"),
		},
		{
			note: "reading non-existent value does not break entire request",
			request: &loadv1.BulkRWRequest{
				WritesData: []*loadv1.BulkRWRequest_WriteDataRequest{
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Create{Create: &loadv1.CreateDataRequest{Path: "/a", Data: []byte("27")}}},
					{Req: &loadv1.BulkRWRequest_WriteDataRequest_Create{Create: &loadv1.CreateDataRequest{Path: "/c", Data: []byte("4")}}},
				},
				ReadsData: []*loadv1.BulkRWRequest_ReadDataRequest{
					{Req: &loadv1.GetDataRequest{Path: "/a"}},
					{Req: &loadv1.GetDataRequest{Path: "/b"}},
					{Req: &loadv1.GetDataRequest{Path: "/c"}},
				},
			},
			expResponse: &loadv1.BulkRWResponse{
				WritesData: []*loadv1.BulkRWResponse_WriteDataResponse{
					{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Create{Create: &loadv1.CreateDataResponse{}}},
					{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Create{Create: &loadv1.CreateDataResponse{}}},
				},
				ReadsData: []*loadv1.BulkRWResponse_ReadDataResponse{
					{Resp: &loadv1.GetDataResponse{Path: "/a", Result: []byte("27")}},
					{Resp: &loadv1.GetDataResponse{Path: "/b"}},
					{Resp: &loadv1.GetDataResponse{Path: "/c", Result: []byte("4")}},
				},
			},
		},
	}

	for _, tc := range tests {
		// We do the full setup/teardown for every test, or else we'd get
		// collisions between testcases due to statefulness.
		{
			storeData := "{}"
			if tc.storeData != "" {
				storeData = tc.storeData
			}
			var storePolicyMap map[string]string
			if tc.storePolicy != nil {
				storePolicyMap = tc.storePolicy
			}
			listener := setupTest(t, storeData, storePolicyMap)
			ctx := context.Background()
			conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("Failed to dial bufnet: %v", err)
			}
			defer conn.Close()
			client := loadv1.NewBulkServiceClient(conn)
			resp, err := client.BulkRW(ctx, tc.request)
			if err != nil {
				// No error expected? Fail test.
				if tc.expErr == nil {
					t.Fatalf("[%s] Unexpected error: %v", tc.note, err)
				}
				// Error expected? Was it the right one?
				if !strings.Contains(err.Error(), tc.expErr.Error()) {
					t.Fatalf("[%s] Expected error: %v, got: %v", tc.note, tc.expErr, err)
				}
			}
			// Check value equality of expected vs actual response.
			if !cmp.Equal(tc.expResponse, resp, protocmp.Transform()) {
				fmt.Println("Diff:\n", cmp.Diff(tc.expResponse, resp, protocmp.Transform()))
				t.Fatalf("[%s] Expected:\n%v\n\nGot:\n%v", tc.note, tc.expResponse, resp)
			}
		}
	}
}
