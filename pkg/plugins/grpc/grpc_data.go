package grpc

import (
	"bytes"
	"context"
	"fmt"
	"io"

	bjson "github.com/styrainc/load-private/pkg/json"
	loadv1 "github.com/styrainc/load-private/proto/gen/go/load/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/server/types"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/util"
)

// To support the Transaction service, we have to do a bit of creative
// refactoring here: all of the interesting low-level work for each
// operation is done in a dedicated helper function, allowing transaction
// management to be deferred to the gRPC handler functions. This means
// that we can use these helper functions in the transaction service too.

// This is only the JSON-parsing path from OPA's server.readInputPostV1() function.
func readInputJSON(jstr []byte) (ast.Value, error) {
	var request types.DataRequestV1
	dec := util.NewJSONDecoder(bytes.NewReader(jstr))
	if err := dec.Decode(&request); err != nil && err != io.EOF {
		return nil, fmt.Errorf("body contains malformed input document: %w", err)
	}

	if request.Input == nil {
		return nil, nil
	}

	return ast.InterfaceToValue(*request.Input)
}

// --------------------------------------------------------
// Low-level request handlers
// These handlers take care of the grunt work around validating the request
// parameters, and querying the store. They defer transaction creation /
// destruction to the caller.

// Handles all validation and store reads/writes. Transaction commit/abort is handled by the caller.
func (s *Server) createDataFromRequest(ctx context.Context, txn storage.Transaction, req *loadv1.CreateDataRequest) (*loadv1.CreateDataResponse, error) {
	path := req.GetPath()
	p, ok := storage.ParsePath(path)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid path")
	}
	val, err := bjson.NewDecoder(bytes.NewReader(req.GetData())).Decode()
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid data: %v", err)
	}
	if err := s.checkPathScope(ctx, txn, p); err != nil {
		return nil, err
	}
	// Create parent paths as needed in the store.
	if _, err = s.store.Read(ctx, txn, p); err != nil {
		// Ignore IsNotFound errors. That just means the key doesn't exist yet.
		if !storage.IsNotFound(err) {
			return nil, status.Error(codes.Internal, err.Error())
		}
		if len(path) > 0 {
			if err := storage.MakeDir(ctx, s.store, txn, p[:len(p)-1]); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}
	// Write a single value to the store.
	if err := s.store.Write(ctx, txn, storage.AddOp, p, val); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &loadv1.CreateDataResponse{}, nil
}

func (s *Server) getDataFromRequest(ctx context.Context, txn storage.Transaction, req *loadv1.GetDataRequest) (*loadv1.GetDataResponse, error) {
	path := req.GetPath()

	rawInput := req.GetInput()
	input, err := readInputJSON(rawInput)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid input")
	}

	var ndbCache builtins.NDBCache
	if s.ndbCacheEnabled {
		ndbCache = builtins.NDBCache{}
	}

	var buf *topdown.BufferTracer
	explainMode := types.ExplainOffV1 // TODO: Plumb in bool option later.
	if explainMode != types.ExplainOffV1 {
		buf = topdown.NewBufferTracer()
	}

	includeInstrumentation := false // TODO: Plumb in bool option later.
	// pretty := false                 // TODO: Plumb in bool option later.

	// Build a prepared query, caching it so that similar queries in the
	// near future won't have the same parsing/setup overheads.
	pqID := "v1GetData::"
	strictBuiltinErrors := false
	if strictBuiltinErrors {
		pqID += "strict-builtin-errors::"
	}
	pqID += path
	preparedQuery, ok := s.getCachedPreparedEvalQuery(pqID)
	if !ok {
		opts := []func(*rego.Rego){
			rego.Compiler(s.getCompiler()),
			rego.Store(s.store),
		}

		// Set resolvers on the base Rego object to avoid having them get
		// re-initialized, and to propagate them to the prepared query.
		for _, r := range s.manager.GetWasmResolvers() {
			for _, entrypoint := range r.Entrypoints() {
				opts = append(opts, rego.Resolver(entrypoint, r))
			}
		}

		rego, err := s.makeRego(ctx, strictBuiltinErrors, txn, input, path, includeInstrumentation, buf, opts)
		if err != nil {
			//_ = logger.Log(ctx, txn, urlPath, "", goInput, input, nil, ndbCache, err)
			return nil, status.Errorf(codes.Internal, "failed to create Rego evaluator: %v", err)
		}

		pq, err := rego.PrepareForEval(ctx)
		if err != nil {
			//_ = logger.Log(ctx, txn, urlPath, "", goInput, input, nil, ndbCache, err)
			return nil, status.Errorf(codes.Internal, "failed to parse Rego query: %v", err)
		}
		preparedQuery = &pq
		s.preparedEvalQueries.Insert(pqID, preparedQuery)
	}

	evalOpts := []rego.EvalOption{
		rego.EvalTransaction(txn),
		rego.EvalParsedInput(input),
		// rego.EvalMetrics(m),
		// rego.EvalQueryTracer(buf),
		rego.EvalInterQueryBuiltinCache(s.interQueryBuiltinCache),
		// rego.EvalInstrument(includeInstrumentation),
		rego.EvalNDBuiltinCache(ndbCache),
	}

	rs, err := preparedQuery.Eval(
		ctx,
		evalOpts...,
	)
	// m.Timer(metrics.ServerHandler).Stop()
	// Handle results.
	if err != nil {
		//_ = logger.Log(ctx, txn, urlPath, "", goInput, input, nil, ndbCache, err, m)
		return nil, status.Errorf(codes.Internal, "evaluation failed: %v", err)
	}

	// result := types.DataResponseV1{
	// 	DecisionID: decisionID,
	// }

	// TODO: Skip metrics and provenance for now...
	if len(rs) == 0 {
		// err = logger.Log(ctx, txn, urlPath, "", goInput, input, nil, ndbCache, nil, m)
		// if err != nil {
		// 	return nil, status.Errorf(codes.Internal, "evaluation failed: %v", err)
		// }
		return &loadv1.GetDataResponse{Path: path}, nil
	}

	resultValue := &rs[0].Expressions[0].Value

	bjsonItem, err := bjson.New(resultValue)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	bs := bjsonItem.String()
	return &loadv1.GetDataResponse{Path: path, Result: []byte(bs)}, nil
}

func (s *Server) updateDataFromRequest(ctx context.Context, txn storage.Transaction, req *loadv1.UpdateDataRequest) (*loadv1.UpdateDataResponse, error) {
	path := req.GetPath()
	p, ok := storage.ParsePath(path)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid path")
	}
	val, err := bjson.NewDecoder(bytes.NewReader(req.GetData())).Decode()
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid data: %v", err)
	}
	// Write single value to the store:
	var patchOp storage.PatchOp
	op := req.GetOp()
	switch op {
	case loadv1.PatchOp_PATCH_OP_UNSPECIFIED:
		patchOp = storage.ReplaceOp // Default to replace.
	case loadv1.PatchOp_PATCH_OP_ADD:
		patchOp = storage.AddOp
	case loadv1.PatchOp_PATCH_OP_REMOVE:
		patchOp = storage.RemoveOp
	case loadv1.PatchOp_PATCH_OP_REPLACE:
		patchOp = storage.ReplaceOp
	default:
		return nil, status.Errorf(codes.InvalidArgument, "invalid op: %v", op)
	}
	if err := s.checkPathScope(ctx, txn, p); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := s.store.Write(ctx, txn, patchOp, p, val); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &loadv1.UpdateDataResponse{}, nil
}

func (s *Server) deleteDataFromRequest(ctx context.Context, txn storage.Transaction, req *loadv1.DeleteDataRequest) (*loadv1.DeleteDataResponse, error) {
	path := req.GetPath()
	p, ok := storage.ParsePath(path)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid path")
	}

	if err := s.checkPathScope(ctx, txn, p); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := s.store.Write(ctx, txn, storage.RemoveOp, p, nil); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &loadv1.DeleteDataResponse{}, nil
}

// --------------------------------------------------------
// Top-level gRPC API request handlers

func (s *Server) CreateData(ctx context.Context, req *loadv1.CreateDataRequest) (*loadv1.CreateDataResponse, error) {
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}

	resp, err := s.createDataFromRequest(ctx, txn, req)
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

// Note(philip): This one is tricky, because the caller could ask for a virtual document, implying an eval.
func (s *Server) GetData(ctx context.Context, req *loadv1.GetDataRequest) (*loadv1.GetDataResponse, error) {
	// decisionID := s.generateDecisionID()
	// ctx := logging.WithDecisionID(r.Context(), decisionID)
	// annotateSpan(ctx, decisionID)

	// Start a transaction (locking the store for reads), and abort when finished to ensure no changes are saved.
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}
	defer s.store.Abort(ctx, txn)

	return s.getDataFromRequest(ctx, txn, req)
}

func (s *Server) UpdateData(ctx context.Context, req *loadv1.UpdateDataRequest) (*loadv1.UpdateDataResponse, error) {
	// Start a transaction, so that we can do reads/writes.
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}

	resp, err := s.updateDataFromRequest(ctx, txn, req)
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

func (s *Server) DeleteData(ctx context.Context, req *loadv1.DeleteDataRequest) (*loadv1.DeleteDataResponse, error) {
	// Start a transaction, so that we can do reads/writes.
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}

	resp, err := s.deleteDataFromRequest(ctx, txn, req)
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
