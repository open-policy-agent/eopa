package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	strictnethttp "github.com/oapi-codegen/runtime/strictmiddleware/nethttp"

	"github.com/styrainc/enterprise-opa-private/pkg/dasapi"
)

var cookieFile = flag.String("cookie-file", "", "file containing the cookie secret")
var tokenFile = flag.String("token-file", "", "file containing the token secret")
var port = flag.Int("port", 0, "port to listen to")

func main() {
	flag.Parse()
	if *port == 0 || (*cookieFile == "" && *tokenFile == "") {
		log.Fatalf("usage: %s --port <port> [--cookie-file <cookie-file>|--token-file <token-file>]", os.Args[0])
	}

	var cookie, token string
	if *cookieFile != "" {
		cookie = strings.TrimSpace(string(must(os.ReadFile(*cookieFile))))
	} else if *tokenFile != "" {
		token = strings.TrimSpace(string(must(os.ReadFile(*tokenFile))))
	}

	s := &srv{}
	mw := []dasapi.StrictMiddlewareFunc{
		//nolint:staticcheck // We need to pass those types
		func(f strictnethttp.StrictHttpHandlerFunc, _ string) strictnethttp.StrictHttpHandlerFunc {
			return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
				// NOTE(sr): it would be more realistic to return some HTTP status code here,
				// with the errors, we just get a 500. But for testing, it's good enough.
				if cookie != "" {
					c, err := r.Cookie("gosessid")
					if err != nil {
						return nil, fmt.Errorf("missing cookie")
					}
					if c.Value != string(cookie) {
						return nil, fmt.Errorf("wrong cookie")
					}
				} else if token != "" {
					if act := r.Header.Get("Authorization"); act[len("bearer "):] != token {
						return nil, fmt.Errorf("wrong token")
					}
				}
				return f(ctx, w, r, request)
			}
		},
	}
	http.Handle("/", dasapi.Handler(dasapi.NewStrictHandler(s, mw)))

	log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *port), nil))
}

type srv struct{}

var errNotImplemented = fmt.Errorf("not implemented")

// List data
// (GET /v1/data)
func (*srv) ListData(context.Context, dasapi.ListDataRequestObject) (dasapi.ListDataResponseObject, error) {
	return nil, errNotImplemented
}

// Check size of data
// (HEAD /v1/data)
func (*srv) HeadListData(context.Context, dasapi.HeadListDataRequestObject) (dasapi.HeadListDataResponseObject, error) {
	return nil, errNotImplemented
}

// Show all data
// (POST /v1/data)
func (*srv) ShowAllData(context.Context, dasapi.ShowAllDataRequestObject) (dasapi.ShowAllDataResponseObject, error) {
	return nil, errNotImplemented
}

// Get data
// (GET /v1/data/{name})
func (*srv) GetData(_ context.Context, request dasapi.GetDataRequestObject) (dasapi.GetDataResponseObject, error) {
	switch request.Name {
	case "libraries/lib02/token":
		return dasapi.GetData200JSONResponse{
			Body: dasapi.DataV1DataResponse{
				Result: dasapi.DataV1DataResponseResult(map[string]any{
					"bearer_token": "testtoken",
				}),
			},
		}, nil
	case "libraries/lib02/thing":
		return dasapi.GetData200JSONResponse{
			Body: dasapi.DataV1DataResponse{
				Result: dasapi.DataV1DataResponseResult(map[string]any{
					"data": map[string]any{
						"one": map[string]any{"foo": 1},
						"two": map[string]any{"foo": 2},
					},
				}),
			},
		}, nil
	}

	return dasapi.GetData404JSONResponse{}, nil
}

// Check the size of the data
// (HEAD /v1/data/{name})
func (*srv) HeadGetData(context.Context, dasapi.HeadGetDataRequestObject) (dasapi.HeadGetDataResponseObject, error) {
	return nil, errNotImplemented
}

// Patch data
// (PATCH /v1/data/{name})
func (*srv) PatchData(context.Context, dasapi.PatchDataRequestObject) (dasapi.PatchDataResponseObject, error) {
	return nil, errNotImplemented
}

// Show data
// (POST /v1/data/{name})
func (*srv) ShowData(context.Context, dasapi.ShowDataRequestObject) (dasapi.ShowDataResponseObject, error) {
	return nil, errNotImplemented
}

// Publish data
// (PUT /v1/data/{name})
func (*srv) PutData(context.Context, dasapi.PutDataRequestObject) (dasapi.PutDataResponseObject, error) {
	return nil, errNotImplemented
}

// List all libraries
// (GET /v1/libraries)
func (*srv) LibrariesList(context.Context, dasapi.LibrariesListRequestObject) (dasapi.LibrariesListResponseObject, error) {
	return dasapi.LibrariesList200JSONResponse{
		// NOTE(sr): we're ignoring fields that the sync code doesn't care for,
		// like metadata, read_only, source_control, ...
		Result: []dasapi.LibrariesV1LibraryEntity{
			{
				Id:          "lib01",
				Description: "A lib without content",
			},
			{
				Id:          "lib02",
				Description: "A lib with policies and data",
			},
		},
	}, nil
}

// Verify git access
// (POST /v1/libraries/source-control/verify-config)
func (*srv) SourceControlVerifyConfigLibrary(context.Context, dasapi.SourceControlVerifyConfigLibraryRequestObject) (dasapi.SourceControlVerifyConfigLibraryResponseObject, error) {
	return nil, errNotImplemented
}

// Delete a library
// (DELETE /v1/libraries/{id})
func (*srv) LibrariesDelete(context.Context, dasapi.LibrariesDeleteRequestObject) (dasapi.LibrariesDeleteResponseObject, error) {
	return nil, errNotImplemented
}

// Get a library
// (GET /v1/libraries/{id})
func (*srv) LibrariesGet(_ context.Context, request dasapi.LibrariesGetRequestObject) (dasapi.LibrariesGetResponseObject, error) {
	switch request.Id {
	case "lib01":
		return dasapi.LibrariesGet200JSONResponse{
			Result: dasapi.LibrariesV1LibraryEntityExpanded{
				Id: "lib01",
			},
		}, nil
	case "lib02":
		envoyMods := []dasapi.SystemsV1Module{
			{
				Name: "policy.rego",
			},
		}
		return dasapi.LibrariesGet200JSONResponse{
			Result: dasapi.LibrariesV1LibraryEntityExpanded{
				Id: "lib02",
				Policies: []dasapi.SystemsV1PolicyConfig{
					{
						Id:      "libraries/lib02/envoy",
						Modules: &envoyMods,
					},
				},
				Datasources: []dasapi.SystemsV1DatasourceConfig{
					{
						Id:       "libraries/lib02/token",
						Category: "rest",
					},
					{
						Id:       "libraries/lib02/thing",
						Category: "gcs/content",
					},
				},
			},
		}, nil
	}
	return nil, errNotImplemented // wrong error, is 500, should be 404
}

// Upsert a new library
// (PUT /v1/libraries/{id})
func (*srv) LibrariesUpdate(context.Context, dasapi.LibrariesUpdateRequestObject) (dasapi.LibrariesUpdateResponseObject, error) {
	return nil, errNotImplemented
}

// Delete a user-owned branch
// (DELETE /v1/libraries/{id}/branch)
func (*srv) DeleteUserBranchLibrary(context.Context, dasapi.DeleteUserBranchLibraryRequestObject) (dasapi.DeleteUserBranchLibraryResponseObject, error) {
	return nil, errNotImplemented
}

// List files in Styra DAS-created branch.
// (GET /v1/libraries/{id}/branch)
func (*srv) GetSourceControlFilesBranchLibrary(context.Context, dasapi.GetSourceControlFilesBranchLibraryRequestObject) (dasapi.GetSourceControlFilesBranchLibraryResponseObject, error) {
	return nil, errNotImplemented
}

// Commit files to library source control
// (POST /v1/libraries/{id}/commits)
func (*srv) CommitFilesToSourceControlLibrary(context.Context, dasapi.CommitFilesToSourceControlLibraryRequestObject) (dasapi.CommitFilesToSourceControlLibraryResponseObject, error) {
	return nil, errNotImplemented
}

// List files in current branch.
// (GET /v1/libraries/{id}/master)
func (*srv) GetSourceControlFilesMasterLibrary(context.Context, dasapi.GetSourceControlFilesMasterLibraryRequestObject) (dasapi.GetSourceControlFilesMasterLibraryResponseObject, error) {
	return nil, errNotImplemented
}

// List policies
// (GET /v1/policies)
func (*srv) ListPolicies(context.Context, dasapi.ListPoliciesRequestObject) (dasapi.ListPoliciesResponseObject, error) {
	return nil, errNotImplemented
}

// Bulk upload policies
// (POST /v1/policies)
func (*srv) BulkUploadPolicies(context.Context, dasapi.BulkUploadPoliciesRequestObject) (dasapi.BulkUploadPoliciesResponseObject, error) {
	return nil, errNotImplemented
}

// List playground policies
// (GET /v1/policies/playground)
func (*srv) ListPlaygroundPolicies(context.Context, dasapi.ListPlaygroundPoliciesRequestObject) (dasapi.ListPlaygroundPoliciesResponseObject, error) {
	return nil, errNotImplemented
}

// Bulk upload playground policies
// (POST /v1/policies/playground)
func (*srv) BulkUploadPlaygroundPolicies(context.Context, dasapi.BulkUploadPlaygroundPoliciesRequestObject) (dasapi.BulkUploadPlaygroundPoliciesResponseObject, error) {
	return nil, errNotImplemented
}

// List system policies
// (GET /v1/policies/systems/{system})
func (*srv) ListSystemPolicies(context.Context, dasapi.ListSystemPoliciesRequestObject) (dasapi.ListSystemPoliciesResponseObject, error) {
	return nil, errNotImplemented
}

// Bulk upload system policies
// (POST /v1/policies/systems/{system})
func (*srv) BulkUploadSystemPolicies(context.Context, dasapi.BulkUploadSystemPoliciesRequestObject) (dasapi.BulkUploadSystemPoliciesResponseObject, error) {
	return nil, errNotImplemented
}

// Delete a policy
// (DELETE /v1/policies/{policy})
func (*srv) DeletePolicy(context.Context, dasapi.DeletePolicyRequestObject) (dasapi.DeletePolicyResponseObject, error) {
	return nil, errNotImplemented
}

// Get a policy
// (GET /v1/policies/{policy})
func (*srv) GetPolicy(_ context.Context, request dasapi.GetPolicyRequestObject) (dasapi.GetPolicyResponseObject, error) {
	switch request.Policy {
	case "libraries/lib02/envoy":
		return &dasapi.GetPolicy200JSONResponse{
			Result: dasapi.PoliciesV1PolicyGetResponseResult(map[string]any{
				"language": "rego",
				"modules": map[string]any{
					"policy.rego": `package libraries.lib02.envoy
import future.keywords
allow if true
`,
				},
			}),
		}, nil
	}
	return &dasapi.GetPolicy404JSONResponse{}, nil
}

// Update a policy
// (PUT /v1/policies/{policy})
func (*srv) UpdatePolicy(context.Context, dasapi.UpdatePolicyRequestObject) (dasapi.UpdatePolicyResponseObject, error) {
	return nil, errNotImplemented
}

func must[T any](x T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return x
}
