package grpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"

	policyv1 "github.com/open-policy-agent/eopa/proto/gen/go/eopa/policy/v1"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage"
)

// To support the Bulk service, we have to do a bit of creative
// refactoring here: all of the interesting low-level work for each
// operation is done in a dedicated helper function, allowing transaction
// management to be deferred to the gRPC handler functions. This means
// that we can use these helper functions in the Bulk service too.

// --------------------------------------------------------
// Low-level request handlers
// These handlers take care of the grunt work around validating the request
// parameters, and querying the store. They defer transaction creation /
// destruction to the caller.

func (s *Server) listPoliciesFromRequest(ctx context.Context, txn storage.Transaction, _ *policyv1.ListPoliciesRequest) (*policyv1.ListPoliciesResponse, error) {
	// Note(philip): We take a similar approach to the OPA REST API's
	// handler, but we only return the raw policy text at this time, not
	// the ASTs.
	ids, err := s.store.ListPolicies(ctx, txn)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	policies := make([]*policyv1.Policy, 0, len(ids))
	for _, id := range ids {
		bs, err := s.store.GetPolicy(ctx, txn, id)
		if err != nil {
			// No need to check for IsNotFound errors; these policies should always exist.
			return nil, status.Error(codes.Internal, err.Error())
		}
		policies = append(policies, &policyv1.Policy{Path: id, Text: string(bs)})
	}

	return &policyv1.ListPoliciesResponse{Results: policies}, nil
}

// preParsedModule is an optional parameter, allowing module parsing to be done elsewhere.
func (s *Server) createPolicyFromRequest(ctx context.Context, txn storage.Transaction, req *policyv1.CreatePolicyRequest, preParsedModule *ast.Module) (*policyv1.CreatePolicyResponse, error) {
	policy := req.GetPolicy()
	rawPolicy := policy.GetText()
	id := policy.GetPath()

	if err := s.checkPolicyIDScope(ctx, txn, id); err != nil && !storage.IsNotFound(err) {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	// Early-exit if incoming policy matches a pre-existing one.
	if bs, err := s.store.GetPolicy(ctx, txn, id); err != nil {
		if !storage.IsNotFound(err) {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else if bytes.Equal([]byte(rawPolicy), bs) {
		return &policyv1.CreatePolicyResponse{}, nil
	}

	// Parse the incoming Rego module.
	// m.Timer(metrics.RegoModuleParse).Start()
	parsedMod := preParsedModule
	if preParsedModule == nil {
		var err error
		parsedMod, err = ast.ParseModule(id, rawPolicy)
		// m.Timer(metrics.RegoModuleParse).Stop()
		if err != nil {
			switch err := err.(type) {
			case ast.Errors:
				return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("error(s) occurred while compiling module(s): %s", err.Error()))
			default:
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
	}

	// Empty module check.
	if parsedMod == nil {
		return nil, status.Error(codes.InvalidArgument, "empty module")
	}

	if err := s.checkPolicyPackageScope(ctx, txn, parsedMod.Package); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	modules, err := s.loadModules(ctx, txn)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	modules[id] = parsedMod

	// Make new compiler
	c := ast.NewCompiler().
		// SetErrorLimit(s.errLimit).
		WithPathConflictsCheck(storage.NonEmpty(ctx, s.store, txn)).
		WithEnablePrintStatements(s.manager.EnablePrintStatements())

	// m.Timer(metrics.RegoModuleCompile).Start()

	if c.Compile(modules); c.Failed() {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("error(s) occurred while compiling module(s): %s", c.Errors.Error()))
	}

	// m.Timer(metrics.RegoModuleCompile).Stop()

	// Upsert policy into the store.
	if err := s.store.UpsertPolicy(ctx, txn, id, []byte(rawPolicy)); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &policyv1.CreatePolicyResponse{}, nil
}

// Note(philip): For the Miro PoC, we're simply dropping the alternative
// fields, like ID and AST, since we can add them directly to the protobuf
// definition later when we've decided how to solve the compiler state
// problem for the plugin.
func (s *Server) getPolicyFromRequest(ctx context.Context, txn storage.Transaction, req *policyv1.GetPolicyRequest) (*policyv1.GetPolicyResponse, error) {
	id := req.GetPath()
	policyBytes, err := s.store.GetPolicy(ctx, txn, id)
	if err != nil {
		// If it's a NotFound error, provide a more helpful error code.
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	// TODO(philip): We have a state problem here: normally, the bundle
	// plugin sets the compiler on the context being used by the *entire*
	// server. However, we can't guarantee that it's loaded yet, and so
	// this puts us in a bind. Do we manually reload *all* the modules and
	// compile them, or do we rely on a stateful compiler stored somewhere?
	// c := s.getCompiler()

	// result := types.PolicyV1{
	// 	ID:  path,
	// 	Raw: string(policyBytes),
	// 	// AST: c.Modules[path], // TODO(philip): We intentionally leave out the AST here for complexity reasons.
	// }
	// bjsonItem, err := bjson.New(result)
	// if err != nil {
	// 	return nil, status.Error(codes.Internal, err.Error())
	// }
	// bs := bjsonItem.String()
	return &policyv1.GetPolicyResponse{Result: &policyv1.Policy{Path: id, Text: string(policyBytes)}}, nil
}

// preParsedModule is an optional parameter, allowing module parsing to be done elsewhere.
func (s *Server) updatePolicyFromRequest(ctx context.Context, txn storage.Transaction, req *policyv1.UpdatePolicyRequest, preParsedModule *ast.Module) (*policyv1.UpdatePolicyResponse, error) {
	policy := req.GetPolicy()
	id := policy.GetPath()
	rawPolicy := policy.GetText()
	if err := s.checkPolicyIDScope(ctx, txn, id); err != nil && !storage.IsNotFound(err) {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	// Early-exit if incoming policy matches a pre-existing one.
	if bs, err := s.store.GetPolicy(ctx, txn, id); err != nil {
		if !storage.IsNotFound(err) {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else if bytes.Equal([]byte(rawPolicy), bs) {
		return &policyv1.UpdatePolicyResponse{}, nil
	}

	// Parse the incoming Rego module.
	// m.Timer(metrics.RegoModuleParse).Start()
	parsedMod := preParsedModule
	if preParsedModule == nil {
		var err error
		parsedMod, err = ast.ParseModule(id, string(rawPolicy))
		// m.Timer(metrics.RegoModuleParse).Stop()
		if err != nil {
			switch err := err.(type) {
			case ast.Errors:
				return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("error(s) occurred while compiling module(s): %s", err.Error()))
			default:
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
	}

	// Empty module check.
	if parsedMod == nil {
		return nil, status.Error(codes.InvalidArgument, "empty module")
	}

	if err := s.checkPolicyPackageScope(ctx, txn, parsedMod.Package); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	modules, err := s.loadModules(ctx, txn)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	modules[id] = parsedMod

	// Make new compiler.
	c := ast.NewCompiler().
		// SetErrorLimit(s.errLimit).
		WithPathConflictsCheck(storage.NonEmpty(ctx, s.store, txn)).
		WithEnablePrintStatements(s.manager.EnablePrintStatements())

	// m.Timer(metrics.RegoModuleCompile).Start()

	if c.Compile(modules); c.Failed() {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("error(s) occurred while compiling module(s): %s", c.Errors.Error()))
	}

	// m.Timer(metrics.RegoModuleCompile).Stop()

	// Upsert policy into the store.
	if err := s.store.UpsertPolicy(ctx, txn, id, []byte(rawPolicy)); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &policyv1.UpdatePolicyResponse{}, nil
}

func (s *Server) deletePolicyFromRequest(ctx context.Context, txn storage.Transaction, req *policyv1.DeletePolicyRequest) (*policyv1.DeletePolicyResponse, error) {
	id := req.GetPath()
	if err := s.checkPolicyIDScope(ctx, txn, id); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	modules, err := s.loadModules(ctx, txn)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	delete(modules, id)

	c := ast.NewCompiler() //.SetErrorLimit(s.errLimit)

	// m.Timer(metrics.RegoModuleCompile).Start()

	if c.Compile(modules); c.Failed() {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("error(s) occurred while compiling module(s): %s", c.Errors.Error()))
	}

	// m.Timer(metrics.RegoModuleCompile).Stop()

	if err := s.store.DeletePolicy(ctx, txn, id); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &policyv1.DeletePolicyResponse{}, nil
}

// Parsing function for individual Policy write payloads.
func StreamingPolicyRWParsePolicyFromRequest(req *policyv1.StreamingPolicyRWRequest_WriteRequest) (*ast.Module, error) {
	var path string
	var rawPolicy string

	switch x := req.GetReq().(type) {
	case *policyv1.StreamingPolicyRWRequest_WriteRequest_Create:
		wr := x.Create
		policy := wr.GetPolicy()
		path = policy.GetPath()
		rawPolicy = policy.GetText()
	case *policyv1.StreamingPolicyRWRequest_WriteRequest_Update:
		wr := x.Update
		policy := wr.GetPolicy()
		path = policy.GetPath()
		rawPolicy = policy.GetText()
	default:
		// All other types.
		return nil, nil
	}

	parsedMod, err := ast.ParseModule(path, string(rawPolicy))
	if err != nil {
		switch err := err.(type) {
		case ast.Errors:
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("error(s) occurred while compiling module(s): %s", err.Error()))
		default:
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}

	return parsedMod, nil
}

func (s *Server) streamingPolicyRWHandleWritesParallel(ctx context.Context, txn storage.Transaction, writes []*policyv1.StreamingPolicyRWRequest_WriteRequest) ([]*policyv1.StreamingPolicyRWResponse_WriteResponse, error) {
	// Process data writes sequentially.
	// Errors coming from the xFromRequest functions should all be
	// pre-wrapped with status.Error, so we can simply forward them
	// up the call chain.
	out := make([]*policyv1.StreamingPolicyRWResponse_WriteResponse, len(writes))

	// Unpack writes in parallel.
	parsedPolicy := make([]*ast.Module, len(writes))
	// Cap the worker pool to GOMAXPROCS, since additional concurrency won't help.
	wg, errCtx := errgroup.WithContext(ctx)
	wg.SetLimit(runtime.GOMAXPROCS(0))
	for i := range writes {
		wg.Go(func() error {
			select {
			case <-errCtx.Done():
				return errCtx.Err()
			default:
				parsedPolicyItem, err := StreamingPolicyRWParsePolicyFromRequest(writes[i])
				if err != nil {
					return err
				}
				parsedPolicy[i] = parsedPolicyItem
				//<-ctx.Done()
				return nil
			}
		})
	}
	if err := wg.Wait(); err != nil {
		return nil, err // txn will be aborted further up the call chain.
	}

	// Execute writes sequentially, aborting on error.
	for i := range writes {
		switch x := writes[i].GetReq().(type) {
		case *policyv1.StreamingPolicyRWRequest_WriteRequest_Create:
			wr := x.Create
			resp, err := s.createPolicyFromRequest(ctx, txn, wr, parsedPolicy[i])
			if err != nil {
				return nil, err // txn will be aborted further up the call chain.
			}
			out[i] = &policyv1.StreamingPolicyRWResponse_WriteResponse{Resp: &policyv1.StreamingPolicyRWResponse_WriteResponse_Create{Create: resp}}
		case *policyv1.StreamingPolicyRWRequest_WriteRequest_Update:
			wr := x.Update
			resp, err := s.updatePolicyFromRequest(ctx, txn, wr, parsedPolicy[i])
			if err != nil {
				return nil, err // txn will be aborted further up the call chain.
			}
			out[i] = &policyv1.StreamingPolicyRWResponse_WriteResponse{Resp: &policyv1.StreamingPolicyRWResponse_WriteResponse_Update{Update: resp}}
		case *policyv1.StreamingPolicyRWRequest_WriteRequest_Delete:
			wr := x.Delete
			resp, err := s.deletePolicyFromRequest(ctx, txn, wr)
			if err != nil {
				return nil, err // txn will be aborted further up the call chain.
			}
			out[i] = &policyv1.StreamingPolicyRWResponse_WriteResponse{Resp: &policyv1.StreamingPolicyRWResponse_WriteResponse_Delete{Delete: resp}}
		case nil:
			// Field was not set.
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("empty Policy write request at index: %d", i)) // txn will be aborted further up the call chain.
		default:
			// Unknown type?
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("unknown type for Policy write request at index: %d", i)) // txn will be aborted further up the call chain.
		}
	}

	// All writes successful? Return results.
	return out, nil
}

// This function handles transaction lifetimes internally. It creates an individual read transaction for each read request in the list.
func (s *Server) streamingPolicyRWHandleReadsParallel(ctx context.Context, reads []*policyv1.StreamingPolicyRWRequest_ReadRequest) ([]*policyv1.StreamingPolicyRWResponse_ReadResponse, error) {
	out := make([]*policyv1.StreamingPolicyRWResponse_ReadResponse, len(reads))
	// Cap the worker pool to GOMAXPROCS, since additional concurrency won't help.
	wg, errCtx := errgroup.WithContext(ctx)
	wg.SetLimit(runtime.GOMAXPROCS(0))
	for i := range reads {
		wg.Go(func() error {
			select {
			case <-errCtx.Done():
				return errCtx.Err()
			default:
				txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
				if err != nil {
					return status.Error(codes.Internal, "read transaction failed")
				}
				policyReadReq := reads[i].GetGet() // TODO: Nil-checks here?
				resp, err := s.getPolicyFromRequest(ctx, txn, policyReadReq)
				if err != nil {
					s.store.Abort(ctx, txn)
					listErr, err := structpb.NewList([]any{structpb.NewStringValue(err.Error())})
					if err != nil {
						return status.Error(codes.Internal, "error serialization failed")
					}
					serializedErr, err := anypb.New(listErr)
					if err != nil {
						return status.Error(codes.Internal, "error serialization failed")
					}
					out[i] = &policyv1.StreamingPolicyRWResponse_ReadResponse{Errors: &policyv1.ErrorList{Errors: []*anypb.Any{serializedErr}}}
					return nil
				}
				out[i] = &policyv1.StreamingPolicyRWResponse_ReadResponse{Get: resp}
				s.store.Abort(ctx, txn)
				return nil
			}
		})
	}
	if err := wg.Wait(); err != nil {
		return nil, err
	}

	// No fatal errors from the reads? Return results.
	return out, nil
}

// --------------------------------------------------------
// Top-level gRPC API request handlers

// Lists all stored policy modules. Equivalent to the Policy REST API's List method.
func (s *Server) ListPolicies(ctx context.Context, req *policyv1.ListPoliciesRequest) (*policyv1.ListPoliciesResponse, error) {
	txn, err := s.store.NewTransaction(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}
	defer s.store.Abort(ctx, txn)

	return s.listPoliciesFromRequest(ctx, txn, req)
}

// Parses, compiles, and installs a policy. Equivalent to the Policy REST API's PUT method.
func (s *Server) CreatePolicy(ctx context.Context, req *policyv1.CreatePolicyRequest) (*policyv1.CreatePolicyResponse, error) {
	// Open a write transaction.
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}

	resp, err := s.createPolicyFromRequest(ctx, txn, req, nil)
	if err != nil {
		s.store.Abort(ctx, txn)
		return nil, err
	}

	if err := s.store.Commit(ctx, txn); err != nil {
		s.store.Abort(ctx, txn)
		return nil, err
	}

	return resp, nil
}

// Retrieves a policy module. Equivalent to the Policy REST API's GET method.
func (s *Server) GetPolicy(ctx context.Context, req *policyv1.GetPolicyRequest) (*policyv1.GetPolicyResponse, error) {
	txn, err := s.store.NewTransaction(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}
	defer s.store.Abort(ctx, txn)

	return s.getPolicyFromRequest(ctx, txn, req)
}

// Parses, compiles, and installs a policy. Equivalent to the Policy REST API's PUT method.
func (s *Server) UpdatePolicy(ctx context.Context, req *policyv1.UpdatePolicyRequest) (*policyv1.UpdatePolicyResponse, error) {
	// Open a write transaction.
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}

	resp, err := s.updatePolicyFromRequest(ctx, txn, req, nil)
	if err != nil {
		s.store.Abort(ctx, txn)
		return nil, err
	}

	if err := s.store.Commit(ctx, txn); err != nil {
		s.store.Abort(ctx, txn)
		return nil, err
	}
	return resp, nil
}

// Deletes a policy module. If other policy modules in the same package
// depend on rules in the policy module to be deleted, the server will
// return an error. Equivalent to the Policy REST API's DELETE method.
func (s *Server) DeletePolicy(ctx context.Context, req *policyv1.DeletePolicyRequest) (*policyv1.DeletePolicyResponse, error) {
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}

	resp, err := s.deletePolicyFromRequest(ctx, txn, req)
	if err != nil {
		s.store.Abort(ctx, txn)
		return nil, err
	}

	if err := s.store.Commit(ctx, txn); err != nil {
		s.store.Abort(ctx, txn)
		return nil, err
	}
	return resp, nil
}

// Only truly fatal errors should cause it to return a non-nil error to the gRPC client.
func (s *Server) StreamingPolicyRW(stream policyv1.PolicyService_StreamingPolicyRWServer) error {
	ctx := stream.Context()
	for {
		// Check context to allow cancellation.
		if err := ctx.Err(); err != nil {
			return err
		}

		resp := policyv1.StreamingPolicyRWResponse{}
		switch req, err := stream.Recv(); err {
		case nil:
			// Process writes first, if present.
			writes := req.GetWrites()
			if len(writes) > 0 {
				params := storage.TransactionParams{Write: len(writes) > 0}

				txn, err := s.store.NewTransaction(ctx, params)
				if err != nil {
					return err
				}
				resp.Writes, err = s.streamingPolicyRWHandleWritesParallel(ctx, txn, writes)
				if err != nil {
					s.store.Abort(ctx, txn)
					return err
				}

				s.store.Commit(ctx, txn)
			}
			// Process reads in parallel, if present.
			reads := req.GetReads()
			if len(reads) > 0 {
				resp.Reads, err = s.streamingPolicyRWHandleReadsParallel(ctx, reads)
				if err != nil {
					return err
				}
			}
			stream.Send(&resp)
		case io.EOF:
			return nil
		default:
			return err
		}
	}
}
