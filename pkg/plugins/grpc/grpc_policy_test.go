// nolint
package grpc_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	datav1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/data/v1"
	policyv1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/policy/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	protocmp "google.golang.org/protobuf/testing/protocmp"
)

func TestListPolicies(t *testing.T) {
	// gRPC server setup/teardown boilerplate.
	policies := map[string]string{
		"/a": `package a

x { true }
y { false }
`,
		"/b": `package b
z := 2
`,
		"/c": `package c
d := 27
`,
	}
	listener := setupTest(t, defaultGRPCConfig, `{}`, policies)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := policyv1.NewPolicyServiceClient(conn)

	// Fetch the policy.
	resp, err := client.ListPolicies(ctx, &policyv1.ListPoliciesRequest{})
	if err != nil {
		t.Fatalf("ListPolicies failed: %v", err)
	}
	// NOTE(philip): When the compiler state issue is resolved in grpc_policy, we can add back the AST.
	expectedPolicies := map[string]string{
		"/a": "package a\n\nx { true }\ny { false }\n",
		"/b": "package b\nz := 2\n",
		"/c": "package c\nd := 27\n",
	}
	results := resp.GetResults()
	// Check list contents by ensuring correct length, then checking each item.
	// Note: This will miss the case where an item is repeated every time.
	if len(results) != 3 {
		t.Fatalf("Expected list of length 3, actual length: %d, list contents: %v", len(results), results)
	}
	for _, result := range results {
		path := result.GetPath()
		result := result.GetText()
		if expectedResult, ok := expectedPolicies[path]; ok {
			if result != expectedResult {
				t.Fatalf("Expected %v\n\ngot:\n%v", expectedResult, result)
			}
		} else {
			t.Fatalf("Path '%v' not found in expectedPolicies", path)
		}
	}
}

// Note(philip): This test unfortunately also requires wiring in the GetPolicy
// method, so that we can check that the value was stored correctly.
func TestCreatePolicy(t *testing.T) {
	// gRPC server setup/teardown boilerplate.
	listener := setupTest(t, defaultGRPCConfig, `{}`, nil)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := policyv1.NewPolicyServiceClient(conn)

	// Create new policy in the store.
	{
		_, err := client.CreatePolicy(ctx, &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/a", Text: `package a

x { true }
y { false }
`}})
		if err != nil {
			t.Fatalf("CreatePolicy failed: %v", err)
		}
	}
	// Fetch the new policy.
	{
		resp, err := client.GetPolicy(ctx, &policyv1.GetPolicyRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetPolicy failed: %v", err)
		}
		policy := resp.GetResult()
		path := policy.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		// NOTE(philip): When the compiler state issue is resolved in grpc_policy, we can add back the AST.
		const expectedPolicy = "package a\n\nx { true }\ny { false }\n"
		result := policy.GetText()
		if expectedPolicy != string(result) {
			t.Fatalf("Expected %v\n\ngot:\n%v", expectedPolicy, string(result))
		}
	}
}

// Note(philip): This serves as a regression test against the bug in Github issue #552.
// Reference: https://github.com/StyraInc/load-private/issues/552
func TestCreateAndOverwritePolicy(t *testing.T) {
	// gRPC server setup/teardown boilerplate.
	listener := setupTest(t, defaultGRPCConfig, `{}`, nil)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	clientPolicy := policyv1.NewPolicyServiceClient(conn)
	clientData := datav1.NewDataServiceClient(conn)

	// Create new policy in the store.
	{
		_, err := clientPolicy.CreatePolicy(ctx, &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/aaa", Text: `package a

x { true }
y { false }
`}})
		if err != nil {
			t.Fatalf("CreatePolicy failed: %v", err)
		}
	}
	{
		resp, err := clientData.GetData(ctx, &datav1.GetDataRequest{Path: "/a/x"})
		if err != nil {
			t.Fatalf("GetData failed: %v", err)
		}
		resultDoc := resp.GetResult()
		path := resultDoc.GetPath()
		if path != "/a/x" {
			t.Fatalf("Expected /a/x, got: %v", path)
		}
		data := resultDoc.GetDocument()
		if data.GetBoolValue() != true {
			t.Fatalf("Expected true, got: %v", data)
		}
	}
	// Update the policy in the store.
	{
		_, err := clientPolicy.CreatePolicy(ctx, &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/aaa", Text: `package a

x := 2
y := 3
`}})
		if err != nil {
			t.Fatalf("CreatePolicy failed: %v", err)
		}
	}
	{
		resp, err := clientData.GetData(ctx, &datav1.GetDataRequest{Path: "/a/x"})
		if err != nil {
			t.Fatalf("GetData failed: %v", err)
		}
		resultDoc := resp.GetResult()
		// path := resultDoc.GetPath()
		// if path != "/a/x" {
		// 	t.Fatalf("Expected /a/x, got: %v", path)
		// }
		data := resultDoc.GetDocument()
		if data.GetNumberValue() != 2 {
			t.Fatalf("Expected 2, got: %v", data)
		}
	}
}

// We pre-populate the store with the same policy used in the CreatePolicy example.
func TestGetPolicy(t *testing.T) {
	// gRPC server setup/teardown boilerplate.
	policies := map[string]string{
		"/a": `package a

x { true }
y { false }
`,
	}
	listener := setupTest(t, defaultGRPCConfig, `{}`, policies)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := policyv1.NewPolicyServiceClient(conn)

	// Fetch the policy.
	resp, err := client.GetPolicy(ctx, &policyv1.GetPolicyRequest{Path: "/a"})
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}
	// NOTE(philip): When the compiler state issue is resolved in grpc_policy, we can add back the AST.
	const expectedPolicy = "package a\n\nx { true }\ny { false }\n"
	policy := resp.GetResult()
	result := policy.GetText()
	if expectedPolicy != string(result) {
		t.Fatalf("Expected %v\n\ngot:\n%v", expectedPolicy, string(result))
	}
}

// We start with the same base policy as the GetPolicy example, and then update the rule head names.
func TestUpdatePolicy(t *testing.T) {
	// gRPC server setup/teardown boilerplate.
	policies := map[string]string{
		"/a": `package a

x { true }
y { false }
`,
	}
	listener := setupTest(t, defaultGRPCConfig, `{}`, policies)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := policyv1.NewPolicyServiceClient(conn)

	// Update the policy in the store.
	{
		_, err := client.UpdatePolicy(ctx, &policyv1.UpdatePolicyRequest{
			Policy: &policyv1.Policy{
				Path: "/a", Text: `package a

r { true }
s { false }
`,
			},
		})
		if err != nil {
			t.Fatalf("UpdatePolicy failed: %v", err)
		}
	}
	// Fetch the updated policy.
	{
		resp, err := client.GetPolicy(ctx, &policyv1.GetPolicyRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetPolicy failed: %v", err)
		}
		policy := resp.GetResult()
		path := policy.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		// NOTE(philip): When the compiler state issue is resolved in grpc_policy, we can add back the AST.
		const expectedPolicy = "package a\n\nr { true }\ns { false }\n"
		result := policy.GetText()
		if expectedPolicy != string(result) {
			t.Fatalf("Expected %v\n\ngot:\n%v", expectedPolicy, string(result))
		}
	}
}

func TestDeletePolicy(t *testing.T) {
	// gRPC server setup/teardown boilerplate.
	policies := map[string]string{
		"/a": `package a

x { true }
y { false }
`,
	}
	listener := setupTest(t, defaultGRPCConfig, `{}`, policies)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := policyv1.NewPolicyServiceClient(conn)

	// Delete the policy from the store.
	{
		_, err := client.DeletePolicy(ctx, &policyv1.DeletePolicyRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("DeletePolicy failed: %v", err)
		}
	}
	// Try fetching the deleted policy.
	{
		resp, err := client.GetPolicy(ctx, &policyv1.GetPolicyRequest{Path: "/a"})
		if err == nil {
			t.Fatalf("GetPolicy was expected to error, got response: %v", resp)
		}
		if status.Code(err) != codes.NotFound {
			t.Fatalf("Expected NotFound error, got: %v", err)
		}
	}
}

// Sequential request / response tests.
// Ported over from `TestBulkRWSeq“ in `grpc_bulk_test.go` .
func TestStreamingPolicyRWSeq(t *testing.T) {
	type StreamingPolicyRWSeqStep struct {
		request     *policyv1.StreamingPolicyRWRequest
		expResponse *policyv1.StreamingPolicyRWResponse
		expErr      error
	}

	tests := []struct {
		note        string
		storeData   string
		storePolicy map[string]string
		steps       []StreamingPolicyRWSeqStep
	}{
		{
			// Inspired by a bug Miro found in the v0.100.8 release.
			note: "Multiple empty requests",
			steps: []StreamingPolicyRWSeqStep{
				{
					request:     &policyv1.StreamingPolicyRWRequest{},
					expResponse: &policyv1.StreamingPolicyRWResponse{},
				},
				{
					request:     &policyv1.StreamingPolicyRWRequest{},
					expResponse: &policyv1.StreamingPolicyRWResponse{},
				},
				{
					request:     &policyv1.StreamingPolicyRWRequest{},
					expResponse: &policyv1.StreamingPolicyRWResponse{},
				},
			},
		},
		{
			note: "Multiple Policy writes",
			steps: []StreamingPolicyRWSeqStep{
				{
					request: &policyv1.StreamingPolicyRWRequest{
						Writes: []*policyv1.StreamingPolicyRWRequest_WriteRequest{
							{Req: &policyv1.StreamingPolicyRWRequest_WriteRequest_Create{Create: &policyv1.CreatePolicyRequest{Policy: &policyv1.Policy{Path: "/march1", Text: "package march1\ny := data.k * input.x + data.b\n"}}}},
						},
					},
					expResponse: &policyv1.StreamingPolicyRWResponse{
						Writes: []*policyv1.StreamingPolicyRWResponse_WriteResponse{
							{Resp: &policyv1.StreamingPolicyRWResponse_WriteResponse_Create{Create: &policyv1.CreatePolicyResponse{}}},
						},
					},
				},
				{
					request: &policyv1.StreamingPolicyRWRequest{
						Writes: []*policyv1.StreamingPolicyRWRequest_WriteRequest{
							{Req: &policyv1.StreamingPolicyRWRequest_WriteRequest_Update{Update: &policyv1.UpdatePolicyRequest{Policy: &policyv1.Policy{Path: "/march1", Text: "package march1\ny := 4 * input.x + 4\n"}}}},
						},
					},
					expResponse: &policyv1.StreamingPolicyRWResponse{
						Writes: []*policyv1.StreamingPolicyRWResponse_WriteResponse{
							{Resp: &policyv1.StreamingPolicyRWResponse_WriteResponse_Update{Update: &policyv1.UpdatePolicyResponse{}}},
						},
					},
				},
				{
					request: &policyv1.StreamingPolicyRWRequest{
						Writes: []*policyv1.StreamingPolicyRWRequest_WriteRequest{
							{Req: &policyv1.StreamingPolicyRWRequest_WriteRequest_Delete{Delete: &policyv1.DeletePolicyRequest{Path: "/march1"}}},
						},
					},
					expResponse: &policyv1.StreamingPolicyRWResponse{
						Writes: []*policyv1.StreamingPolicyRWResponse_WriteResponse{
							{Resp: &policyv1.StreamingPolicyRWResponse_WriteResponse_Delete{Delete: &policyv1.DeletePolicyResponse{}}},
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
			client := policyv1.NewPolicyServiceClient(conn)
			sclient, err := client.StreamingPolicyRW(ctx)
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
