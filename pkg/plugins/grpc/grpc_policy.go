package grpc

import (
	"bytes"
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"

	policyv1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/policy/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	path := policy.GetPath()
	rawPolicy := policy.GetText()

	if err := s.checkPolicyIDScope(ctx, txn, path); err != nil && !storage.IsNotFound(err) {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	// Early-exit if incoming policy matches a pre-existing one.
	if bs, err := s.store.GetPolicy(ctx, txn, path); err != nil {
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
		parsedMod, err = ast.ParseModule(path, string(rawPolicy))
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

	modules[path] = parsedMod

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
	if err := s.store.UpsertPolicy(ctx, txn, path, []byte(rawPolicy)); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &policyv1.CreatePolicyResponse{}, nil
}

// Note(philip): For the Miro PoC, we're simply dropping the alternative
// fields, like ID and AST, since we can add them directly to the protobuf
// definition later when we've decided how to solve the compiler state
// problem for the plugin.
func (s *Server) getPolicyFromRequest(ctx context.Context, txn storage.Transaction, req *policyv1.GetPolicyRequest) (*policyv1.GetPolicyResponse, error) {
	path := req.GetPath()

	policyBytes, err := s.store.GetPolicy(ctx, txn, path)
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
	return &policyv1.GetPolicyResponse{Result: &policyv1.Policy{Path: path, Text: string(policyBytes)}}, nil
}

// preParsedModule is an optional parameter, allowing module parsing to be done elsewhere.
func (s *Server) updatePolicyFromRequest(ctx context.Context, txn storage.Transaction, req *policyv1.UpdatePolicyRequest, preParsedModule *ast.Module) (*policyv1.UpdatePolicyResponse, error) {
	policy := req.GetPolicy()
	path := policy.GetPath()
	rawPolicy := policy.GetText()
	if err := s.checkPolicyIDScope(ctx, txn, path); err != nil && !storage.IsNotFound(err) {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	// Early-exit if incoming policy matches a pre-existing one.
	if bs, err := s.store.GetPolicy(ctx, txn, path); err != nil {
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
		parsedMod, err = ast.ParseModule(path, string(rawPolicy))
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

	modules[path] = parsedMod

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
	if err := s.store.UpsertPolicy(ctx, txn, path, []byte(rawPolicy)); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &policyv1.UpdatePolicyResponse{}, nil
}

func (s *Server) deletePolicyFromRequest(ctx context.Context, txn storage.Transaction, req *policyv1.DeletePolicyRequest) (*policyv1.DeletePolicyResponse, error) {
	path := req.GetPath()
	if err := s.checkPolicyIDScope(ctx, txn, path); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	modules, err := s.loadModules(ctx, txn)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	delete(modules, path)

	c := ast.NewCompiler() //.SetErrorLimit(s.errLimit)

	// m.Timer(metrics.RegoModuleCompile).Start()

	if c.Compile(modules); c.Failed() {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("error(s) occurred while compiling module(s): %s", c.Errors.Error()))
	}

	// m.Timer(metrics.RegoModuleCompile).Stop()

	if err := s.store.DeletePolicy(ctx, txn, path); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &policyv1.DeletePolicyResponse{}, nil
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
