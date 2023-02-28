package grpc_test

import (
	"context"
	"testing"

	loadv1 "github.com/styrainc/load-private/proto/gen/go/load/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Note(philip): This test unfortunately also requires wiring in the GetPolicy
// method, so that we can check that the value was stored correctly.
func TestCreatePolicy(t *testing.T) {
	// gRPC server setup/teardown boilerplate.
	listener := setupTest(t, `{}`, nil)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := loadv1.NewPolicyServiceClient(conn)

	// Create new policy in the store.
	{
		_, err := client.CreatePolicy(ctx, &loadv1.CreatePolicyRequest{
			Path: "/a", Policy: `package a

x { true }
y { false }
`,
		})
		if err != nil {
			t.Fatalf("CreatePolicy failed: %v", err)
		}
	}
	// Fetch the new policy.
	{
		resp, err := client.GetPolicy(ctx, &loadv1.GetPolicyRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetPolicy failed: %v", err)
		}
		path := resp.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		// NOTE(philip): When the compiler state issue is resolved in grpc_policy, we can add back the AST.
		const expectedPolicy = "package a\n\nx { true }\ny { false }\n"
		result := resp.GetResult()
		if expectedPolicy != string(result) {
			t.Fatalf("Expected %v\n\ngot:\n%v", expectedPolicy, string(result))
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
	listener := setupTest(t, `{}`, policies)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := loadv1.NewPolicyServiceClient(conn)

	// Fetch the policy.
	resp, err := client.GetPolicy(ctx, &loadv1.GetPolicyRequest{Path: "/a"})
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}
	// NOTE(philip): When the compiler state issue is resolved in grpc_policy, we can add back the AST.
	const expectedPolicy = "package a\n\nx { true }\ny { false }\n"
	result := resp.GetResult()
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
	listener := setupTest(t, `{}`, policies)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := loadv1.NewPolicyServiceClient(conn)

	// Update the policy in the store.
	{
		_, err := client.UpdatePolicy(ctx, &loadv1.UpdatePolicyRequest{
			Path: "/a", Policy: `package a

r { true }
s { false }
`,
		})
		if err != nil {
			t.Fatalf("UpdatePolicy failed: %v", err)
		}
	}
	// Fetch the updated policy.
	{
		resp, err := client.GetPolicy(ctx, &loadv1.GetPolicyRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("GetPolicy failed: %v", err)
		}
		path := resp.GetPath()
		if path != "/a" {
			t.Fatalf("Expected /a, got: %v", path)
		}
		// NOTE(philip): When the compiler state issue is resolved in grpc_policy, we can add back the AST.
		const expectedPolicy = "package a\n\nr { true }\ns { false }\n"
		result := resp.GetResult()
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
	listener := setupTest(t, `{}`, policies)
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(GetBufDialer(listener)), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	client := loadv1.NewPolicyServiceClient(conn)

	// Delete the policy from the store.
	{
		_, err := client.DeletePolicy(ctx, &loadv1.DeletePolicyRequest{Path: "/a"})
		if err != nil {
			t.Fatalf("DeletePolicy failed: %v", err)
		}
	}
	// Try fetching the deleted policy.
	{
		resp, err := client.GetPolicy(ctx, &loadv1.GetPolicyRequest{Path: "/a"})
		if err == nil {
			t.Fatalf("GetPolicy was expected to error, got response: %v", resp)
		}
		if status.Code(err) != codes.NotFound {
			t.Fatalf("Expected NotFound error, got: %v", err)
		}
	}
}
