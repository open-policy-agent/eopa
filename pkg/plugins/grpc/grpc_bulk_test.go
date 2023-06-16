package grpc_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	bulkv1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/bulk/v1"
	datav1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/data/v1"
	policyv1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/policy/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	cmp "github.com/google/go-cmp/cmp"
	protocmp "google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
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
		request     *bulkv1.BulkRWRequest
		expResponse *bulkv1.BulkRWResponse
		expErr      error
	}{
		// Data writes (single req/resp).
		{
			note: "single data create",
			request: &bulkv1.BulkRWRequest{
				WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
				},
			},
		},
		{
			note:      "single data update",
			storeData: `{"a": 27}`,
			request: &bulkv1.BulkRWRequest{
				WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Update{Update: &datav1.UpdateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Update{Update: &datav1.UpdateDataResponse{}}},
				},
			},
		},
		{
			note:      "single data delete",
			storeData: `{"a": 27}`,
			request: &bulkv1.BulkRWRequest{
				WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Delete{Delete: &datav1.DeleteDataRequest{Path: "/a"}}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Delete{Delete: &datav1.DeleteDataResponse{}}},
				},
			},
		},
		// Policy writes (single req/resp).
		{
			note: "single policy create",
			request: &bulkv1.BulkRWRequest{
				WritesPolicy: []*bulkv1.BulkRWRequest_WritePolicyRequest{
					{Req: &bulkv1.BulkRWRequest_WritePolicyRequest_Create{Create: &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/a", Text: "package a\n\nx { true }\ny { false }\n"}}}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesPolicy: []*bulkv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &bulkv1.BulkRWResponse_WritePolicyResponse_Create{Create: &policyv1.CreatePolicyResponse{}}},
				},
			},
		},
		{
			note: "single policy update",
			storePolicy: map[string]string{
				"/a": "package a\n\nx { false }\ny { false }\n",
			},
			request: &bulkv1.BulkRWRequest{
				WritesPolicy: []*bulkv1.BulkRWRequest_WritePolicyRequest{
					{Req: &bulkv1.BulkRWRequest_WritePolicyRequest_Create{Create: &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/a", Text: "package a\n\nx { true }\ny { false }\n"}}}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesPolicy: []*bulkv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &bulkv1.BulkRWResponse_WritePolicyResponse_Create{Create: &policyv1.CreatePolicyResponse{}}},
				},
			},
		},
		{
			note: "single policy delete",
			storePolicy: map[string]string{
				"/a": "package a\n\nx { true }\ny { false }\n",
			},
			request: &bulkv1.BulkRWRequest{
				WritesPolicy: []*bulkv1.BulkRWRequest_WritePolicyRequest{
					{Req: &bulkv1.BulkRWRequest_WritePolicyRequest_Create{Create: &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/a", Text: "package a\n\nx { true }\ny { false }\n"}}}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesPolicy: []*bulkv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &bulkv1.BulkRWResponse_WritePolicyResponse_Create{Create: &policyv1.CreatePolicyResponse{}}},
				},
			},
		},
		// Data reads (single req/resp).
		{
			note:      "single data read",
			storeData: `{"a": 27}`,
			request: &bulkv1.BulkRWRequest{
				ReadsData: []*bulkv1.BulkRWRequest_ReadDataRequest{
					{Req: &datav1.GetDataRequest{Path: "/a"}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				ReadsData: []*bulkv1.BulkRWResponse_ReadDataResponse{
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}},
				},
			},
		},
		// Policy reads (single req/resp).
		{
			note: "single policy read",
			storePolicy: map[string]string{
				"/a": "package a\n\nx { true }\ny { false }\n",
			},
			request: &bulkv1.BulkRWRequest{
				ReadsPolicy: []*bulkv1.BulkRWRequest_ReadPolicyRequest{
					{Req: &policyv1.GetPolicyRequest{Path: "/a"}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				ReadsPolicy: []*bulkv1.BulkRWResponse_ReadPolicyResponse{
					{Resp: &policyv1.GetPolicyResponse{Result: &policyv1.Policy{Path: "/a", Text: "package a\n\nx { true }\ny { false }\n"}}},
				},
			},
		},
		// Bulk, sequential writes + reads to check for ordering.
		{
			note: "gradual object construction + policy + reads from base/virtual documents",
			request: &bulkv1.BulkRWRequest{
				WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}}},
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/b", Document: structpb.NewStructValue(&structpb.Struct{Fields: map[string]*structpb.Value{"c": structpb.NewNumberValue(1), "d": structpb.NewNumberValue(2), "e": structpb.NewNumberValue(3)}})}}}},
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Update{Update: &datav1.UpdateDataRequest{Data: &datav1.DataDocument{Path: "/b/d", Document: structpb.NewNumberValue(10)}}}},
				},
				WritesPolicy: []*bulkv1.BulkRWRequest_WritePolicyRequest{
					{Req: &bulkv1.BulkRWRequest_WritePolicyRequest_Create{Create: &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/test", Text: "package test\n\nx { true }\ny = false\nz = data.a + data.b.c + data.b.d\n"}}}},
				},
				ReadsData: []*bulkv1.BulkRWRequest_ReadDataRequest{
					{Req: &datav1.GetDataRequest{Path: "/test/x"}},
					{Req: &datav1.GetDataRequest{Path: "/test/y"}},
					{Req: &datav1.GetDataRequest{Path: "/test/z"}},
				},
				ReadsPolicy: []*bulkv1.BulkRWRequest_ReadPolicyRequest{
					{Req: &policyv1.GetPolicyRequest{Path: "/test"}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Update{Update: &datav1.UpdateDataResponse{}}},
				},
				WritesPolicy: []*bulkv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &bulkv1.BulkRWResponse_WritePolicyResponse_Create{Create: &policyv1.CreatePolicyResponse{}}},
				},
				ReadsData: []*bulkv1.BulkRWResponse_ReadDataResponse{
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/test/x", Document: structpb.NewBoolValue(true)}}},
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/test/y", Document: structpb.NewBoolValue(false)}}},
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/test/z", Document: structpb.NewNumberValue(38)}}},
				},
				ReadsPolicy: []*bulkv1.BulkRWResponse_ReadPolicyResponse{
					{Resp: &policyv1.GetPolicyResponse{Result: &policyv1.Policy{Path: "/test", Text: "package test\n\nx { true }\ny = false\nz = data.a + data.b.c + data.b.d\n"}}},
				},
			},
		},
		// Policy + empty input.
		{
			note: "Empty input for policy",
			request: &bulkv1.BulkRWRequest{
				WritesPolicy: []*bulkv1.BulkRWRequest_WritePolicyRequest{
					{Req: &bulkv1.BulkRWRequest_WritePolicyRequest_Create{Create: &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/test", Text: "package test\nz := data.x * data.y\n"}}}},
				},
				WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/x", Document: structpb.NewNumberValue(2)}}}},
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/y", Document: structpb.NewNumberValue(3)}}}},
				},
				ReadsData: []*bulkv1.BulkRWRequest_ReadDataRequest{
					{Req: &datav1.GetDataRequest{Path: "/test/z", Input: &datav1.InputDocument{Document: structpb.NewNullValue().GetStructValue()}}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesPolicy: []*bulkv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &bulkv1.BulkRWResponse_WritePolicyResponse_Create{Create: &policyv1.CreatePolicyResponse{}}},
				},
				WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
				},
				ReadsData: []*bulkv1.BulkRWResponse_ReadDataResponse{
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/test/z", Document: structpb.NewNumberValue(6)}}},
				},
			},
		},
		// Policy + input.
		{
			note: "Miro example - march example",
			request: &bulkv1.BulkRWRequest{
				WritesPolicy: []*bulkv1.BulkRWRequest_WritePolicyRequest{
					{Req: &bulkv1.BulkRWRequest_WritePolicyRequest_Create{Create: &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/march1", Text: "package march1\ny := data.k * input.x + data.b\n"}}}},
				},
				WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/k", Document: structpb.NewNumberValue(2)}}}},
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/b", Document: structpb.NewNumberValue(3)}}}},
				},
				ReadsData: []*bulkv1.BulkRWRequest_ReadDataRequest{
					{Req: &datav1.GetDataRequest{Path: "/march1/y", Input: &datav1.InputDocument{Document: &structpb.Struct{Fields: map[string]*structpb.Value{"x": structpb.NewNumberValue(45)}}}}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesPolicy: []*bulkv1.BulkRWResponse_WritePolicyResponse{
					{Resp: &bulkv1.BulkRWResponse_WritePolicyResponse_Create{Create: &policyv1.CreatePolicyResponse{}}},
				},
				WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
				},
				ReadsData: []*bulkv1.BulkRWResponse_ReadDataResponse{
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/march1/y", Document: structpb.NewNumberValue(93)}}},
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
			request: &bulkv1.BulkRWRequest{
				WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}}},
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Delete{Delete: &datav1.DeleteDataRequest{Path: "/b"}}}, // will fail because of non-existent path.
				},
				ReadsData: []*bulkv1.BulkRWRequest_ReadDataRequest{
					{Req: &datav1.GetDataRequest{Path: "/a"}},
				},
			},
			expErr: fmt.Errorf("rpc error: code = NotFound desc = storage_not_found_error: /b: document does not exist"),
		},
		{
			note: "reading non-existent value does not break entire request",
			request: &bulkv1.BulkRWRequest{
				WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}}},
					{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/c", Document: structpb.NewNumberValue(4)}}}},
				},
				ReadsData: []*bulkv1.BulkRWRequest_ReadDataRequest{
					{Req: &datav1.GetDataRequest{Path: "/a"}},
					{Req: &datav1.GetDataRequest{Path: "/b"}},
					{Req: &datav1.GetDataRequest{Path: "/c"}},
				},
			},
			expResponse: &bulkv1.BulkRWResponse{
				WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
					{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
				},
				ReadsData: []*bulkv1.BulkRWResponse_ReadDataResponse{
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}},
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/b"}}},
					{Resp: &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: "/c", Document: structpb.NewNumberValue(4)}}},
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
			listener := setupTest(t, defaultGRPCConfig, storeData, storePolicyMap)
			ctx := context.Background()
			conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("Failed to dial bufnet: %v", err)
			}
			defer conn.Close()
			client := bulkv1.NewBulkServiceClient(conn)
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

// Sequential request / response tests.
// Note(philip): These tests have been introduced because of a bug found in
// the v0.100.8 release by Miro, where a write transaction was opened, but
// potentially never closed, causing Enterprise OPA to hang indefinitely.
func TestBulkRWSeq(t *testing.T) {
	type BulkRWSeqStep struct {
		request     *bulkv1.BulkRWRequest
		expResponse *bulkv1.BulkRWResponse
		expErr      error
	}
	tests := []struct {
		note        string
		storeData   string
		storePolicy map[string]string
		steps       []BulkRWSeqStep
	}{
		{
			// Inspired by a bug Miro found in the v0.100.8 release.
			note: "Multiple empty requests",
			steps: []BulkRWSeqStep{
				{
					request:     &bulkv1.BulkRWRequest{},
					expResponse: &bulkv1.BulkRWResponse{},
				},
				{
					request:     &bulkv1.BulkRWRequest{},
					expResponse: &bulkv1.BulkRWResponse{},
				},
				{
					request:     &bulkv1.BulkRWRequest{},
					expResponse: &bulkv1.BulkRWResponse{},
				},
			},
		},
		{
			note: "Multiple data writes",
			steps: []BulkRWSeqStep{
				{
					request: &bulkv1.BulkRWRequest{
						WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
							{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(27)}}}},
						},
					},
					expResponse: &bulkv1.BulkRWResponse{
						WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
							{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
						},
					},
				},
				{
					request: &bulkv1.BulkRWRequest{
						WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
							{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(28)}}}},
						},
					},
					expResponse: &bulkv1.BulkRWResponse{
						WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
							{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
						},
					},
				},
				{
					request: &bulkv1.BulkRWRequest{
						WritesData: []*bulkv1.BulkRWRequest_WriteDataRequest{
							{Req: &bulkv1.BulkRWRequest_WriteDataRequest_Create{Create: &datav1.CreateDataRequest{Data: &datav1.DataDocument{Path: "/a", Document: structpb.NewNumberValue(29)}}}},
						},
					},
					expResponse: &bulkv1.BulkRWResponse{
						WritesData: []*bulkv1.BulkRWResponse_WriteDataResponse{
							{Resp: &bulkv1.BulkRWResponse_WriteDataResponse_Create{Create: &datav1.CreateDataResponse{}}},
						},
					},
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
			listener := setupTest(t, defaultGRPCConfig, storeData, storePolicyMap)
			ctx := context.Background()
			conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("Failed to dial bufnet: %v", err)
			}
			defer conn.Close()
			client := bulkv1.NewBulkServiceClient(conn)

			// Run each test's steps in sequence on the live service instance.
			for _, step := range tc.steps {
				resp, err := client.BulkRW(ctx, step.request)
				if err != nil {
					// No error expected? Fail test.
					if step.expErr == nil {
						t.Fatalf("[%s] Unexpected error: %v", tc.note, err)
					}
					// Error expected? Was it the right one?
					if !strings.Contains(err.Error(), step.expErr.Error()) {
						t.Fatalf("[%s] Expected error: %v, got: %v", tc.note, step.expErr, err)
					}
				}
				// Check value equality of expected vs actual response.
				if !cmp.Equal(step.expResponse, resp, protocmp.Transform()) {
					fmt.Println("Diff:\n", cmp.Diff(step.expResponse, resp, protocmp.Transform()))
					t.Fatalf("[%s] Expected:\n%v\n\nGot:\n%v", tc.note, step.expResponse, resp)
				}
			}
		}
	}
}
