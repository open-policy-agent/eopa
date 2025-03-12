package grpc

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	datav1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/data/v1"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
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

// Handles all validation and store reads/writes. Transaction commit/abort is handled by the caller.
// preParsedValue is an optional parameter, allowing JSON parsing to be done elsewhere.
func (s *Server) createDataFromRequest(ctx context.Context, txn storage.Transaction, req *datav1.CreateDataRequest, preParsedValue any) (*datav1.CreateDataResponse, error) {
	dataDoc := req.GetData()
	path := dataDoc.GetPath()
	p, ok := storage.ParsePathEscaped("/" + strings.Trim(path, "/"))
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid path")
	}
	val := preParsedValue
	if preParsedValue == nil {
		var err error

		// val, err = bjson.NewDecoder(strings.NewReader(req.GetData())).Decode()
		val, err = bjson.New(dataDoc.GetDocument().AsInterface())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid data: %v", err)
		}
	}

	if err := s.checkPathScope(ctx, txn, p); err != nil {
		return nil, err
	}

	// Create parent paths as needed in the store.
	if _, err := s.store.Read(ctx, txn, p); err != nil {
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
	return &datav1.CreateDataResponse{}, nil
}

func (s *Server) getDataFromRequest(ctx context.Context, txn storage.Transaction, req *datav1.GetDataRequest) (*datav1.GetDataResponse, error) {
	m := metrics.New()
	path := req.GetPath()

	remoteAddr := remoteAddrFromContext(ctx)
	decisionID := s.generateDecisionID()

	rawInput := req.GetInput().GetDocument().AsMap()
	input, err := ast.InterfaceToValue(rawInput)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid input")
	}
	var goInput *any
	if input != nil {
		x, err := ast.JSON(input)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "could not marshal input: %s", err)
		}
		goInput = &x
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

	br, err := getRevisions(ctx, s.store, txn)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	logger := s.getDecisionLogger(br)

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
			_ = logger.Log(ctx, txn, decisionID, remoteAddr, path, "", goInput, input, nil, ndbCache, err, m)
			return nil, status.Errorf(codes.Internal, "failed to create Rego evaluator: %v", err)
		}

		pq, err := rego.PrepareForEval(ctx)
		if err != nil {
			_ = logger.Log(ctx, txn, decisionID, remoteAddr, path, "", goInput, input, nil, ndbCache, err, m)
			return nil, status.Errorf(codes.Internal, "failed to parse Rego query: %v", err)
		}
		preparedQuery = &pq
		s.preparedEvalQueries.Insert(pqID, preparedQuery)
	}

	evalOpts := []rego.EvalOption{
		rego.EvalTransaction(txn),
		rego.EvalParsedInput(input),
		rego.EvalMetrics(m),
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
		_ = logger.Log(ctx, txn, decisionID, remoteAddr, path, "", goInput, input, nil, ndbCache, err, m)
		return nil, status.Errorf(codes.Internal, "evaluation failed: %v", err)
	}

	// result := types.DataResponseV1{
	// 	DecisionID: decisionID,
	// }

	// TODO: Skip metrics and provenance for now...
	if len(rs) == 0 {
		err = logger.Log(ctx, txn, decisionID, remoteAddr, path, "", goInput, input, nil, ndbCache, err, m)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "evaluation failed: %v", err)
		}
		return &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: path}}, nil
	}

	resultValue := &rs[0].Expressions[0].Value
	bjsonItem, err := bjson.New(resultValue)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	// Convert through any to correctly return to using numeric types.
	var interfaceHop any
	if err := bjson.Unmarshal(bjsonItem, &interfaceHop); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	// Note(philip): We can't do `structpb.NewValue(bjsonItem.JSON())`, because json.Number fails to auto-convert.
	bv, err := structpb.NewValue(interfaceHop)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	// Log successful decision:
	if err := logger.Log(ctx, txn, decisionID, remoteAddr, path, "", goInput, input, resultValue, ndbCache, nil, m); err != nil {
		return nil, status.Errorf(codes.Internal, "evaluation failed: %v", err)
	}
	return &datav1.GetDataResponse{Result: &datav1.DataDocument{Path: path, Document: bv}}, nil
}

func (s *Server) updateDataFromRequest(ctx context.Context, txn storage.Transaction, req *datav1.UpdateDataRequest, preParsedValue any) (*datav1.UpdateDataResponse, error) {
	dataDoc := req.GetData()
	path := dataDoc.GetPath()
	p, ok := storage.ParsePathEscaped("/" + strings.Trim(path, "/"))
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid path")
	}
	val := preParsedValue
	if preParsedValue == nil {
		var err error
		// val, err = bjson.NewDecoder(strings.NewReader(req.GetData())).Decode()
		val, err = bjson.New(dataDoc.GetDocument().AsInterface())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid data: %v", err)
		}
	}
	var patchOp storage.PatchOp
	op := req.GetOp()
	switch op {
	case datav1.PatchOp_PATCH_OP_UNSPECIFIED:
		patchOp = storage.ReplaceOp // Default to replace.
	case datav1.PatchOp_PATCH_OP_ADD:
		patchOp = storage.AddOp
	case datav1.PatchOp_PATCH_OP_REMOVE:
		patchOp = storage.RemoveOp
	case datav1.PatchOp_PATCH_OP_REPLACE:
		patchOp = storage.ReplaceOp
	default:
		return nil, status.Errorf(codes.InvalidArgument, "invalid op: %v", op)
	}

	if err := s.checkPathScope(ctx, txn, p); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Write single value to the store:
	if err := s.store.Write(ctx, txn, patchOp, p, val); err != nil {
		if storage.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &datav1.UpdateDataResponse{}, nil
}

func (s *Server) deleteDataFromRequest(ctx context.Context, txn storage.Transaction, req *datav1.DeleteDataRequest) (*datav1.DeleteDataResponse, error) {
	path := req.GetPath()
	p, ok := storage.ParsePathEscaped("/" + strings.Trim(path, "/"))
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

	return &datav1.DeleteDataResponse{}, nil
}

// --------------------------------------------------------
// Low-level streaming gRPC API request handlers and utils

// Parsing function for individual Data write payloads.
// Returns a bjson.BJSON under-the-hood.
func StreamingDataRWParseDataFromRequest(req *datav1.StreamingDataRWRequest_WriteRequest) (any, error) {
	var data *structpb.Value

	switch x := req.GetReq().(type) {
	case *datav1.StreamingDataRWRequest_WriteRequest_Create:
		wr := x.Create
		data = wr.GetData().GetDocument()
	case *datav1.StreamingDataRWRequest_WriteRequest_Update:
		wr := x.Update
		data = wr.GetData().GetDocument()
	default:
		// All other types.
		return nil, nil
	}

	// val, err := bjson.NewDecoder(strings.NewReader(data)).Decode()
	val, err := bjson.New(data.AsInterface())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid data: %v", err)
	}

	return val, nil
}

func (s *Server) streamingDataRWHandleWritesParallel(ctx context.Context, txn storage.Transaction, writes []*datav1.StreamingDataRWRequest_WriteRequest) ([]*datav1.StreamingDataRWResponse_WriteResponse, error) {
	// Process data writes sequentially.
	// Errors coming from the xFromRequest functions should all be
	// pre-wrapped with status.Error, so we can simply forward them
	// up the call chain.
	out := make([]*datav1.StreamingDataRWResponse_WriteResponse, len(writes))

	// Unpack writes in parallel.
	parsedData := make([]any, len(writes))
	// Cap the worker pool to GOMAXPROCS, since additional concurrency won't help.
	wg, errCtx := errgroup.WithContext(ctx)
	wg.SetLimit(runtime.GOMAXPROCS(0))
	for i := range writes {
		wg.Go(func() error {
			select {
			case <-errCtx.Done():
				return errCtx.Err()
			default:
				parsedDataItem, err := StreamingDataRWParseDataFromRequest(writes[i])
				if err != nil {
					return err
				}
				parsedData[i] = parsedDataItem
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
		case *datav1.StreamingDataRWRequest_WriteRequest_Create:
			wr := x.Create
			resp, err := s.createDataFromRequest(ctx, txn, wr, parsedData[i])
			if err != nil {
				return nil, err // txn will be aborted further up the call chain.
			}
			out[i] = &datav1.StreamingDataRWResponse_WriteResponse{Resp: &datav1.StreamingDataRWResponse_WriteResponse_Create{Create: resp}}
		case *datav1.StreamingDataRWRequest_WriteRequest_Update:
			wr := x.Update
			resp, err := s.updateDataFromRequest(ctx, txn, wr, parsedData[i])
			if err != nil {
				return nil, err // txn will be aborted further up the call chain.
			}
			out[i] = &datav1.StreamingDataRWResponse_WriteResponse{Resp: &datav1.StreamingDataRWResponse_WriteResponse_Update{Update: resp}}
		case *datav1.StreamingDataRWRequest_WriteRequest_Delete:
			wr := x.Delete
			resp, err := s.deleteDataFromRequest(ctx, txn, wr)
			if err != nil {
				return nil, err // txn will be aborted further up the call chain.
			}
			out[i] = &datav1.StreamingDataRWResponse_WriteResponse{Resp: &datav1.StreamingDataRWResponse_WriteResponse_Delete{Delete: resp}}
		case nil:
			// Field was not set.

			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("empty data write request at index: %d", i)) // txn will be aborted further up the call chain.
		default:
			// Unknown type?
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("unknown type for data write request at index: %d", i)) // txn will be aborted further up the call chain.
		}
	}

	// All writes successful? Return results.
	return out, nil
}

// This function handles transaction lifetimes internally. It creates an individual read transaction for each read request in the list.
func (s *Server) streamingDataRWHandleReadsParallel(ctx context.Context, reads []*datav1.StreamingDataRWRequest_ReadRequest) ([]*datav1.StreamingDataRWResponse_ReadResponse, error) {
	out := make([]*datav1.StreamingDataRWResponse_ReadResponse, len(reads))
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
				dataReadReq := reads[i].GetGet() // TODO: Nil-checks here?
				resp, err := s.getDataFromRequest(ctx, txn, dataReadReq)
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
					out[i] = &datav1.StreamingDataRWResponse_ReadResponse{Errors: &datav1.ErrorList{Errors: []*anypb.Any{serializedErr}}}
					return nil
				}
				out[i] = &datav1.StreamingDataRWResponse_ReadResponse{Get: resp}
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

// Creates or overwrites a data document, creating any necessary containing documents to make the path valid.
// Equivalent to the Data REST API's PUT method.
func (s *Server) CreateData(ctx context.Context, req *datav1.CreateDataRequest) (*datav1.CreateDataResponse, error) {
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}

	resp, err := s.createDataFromRequest(ctx, txn, req, nil)
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

// Retrieves/evaluates a document requiring input.
// Equivalent to the Data REST API's GET (with Input) method.
func (s *Server) GetData(ctx context.Context, req *datav1.GetDataRequest) (*datav1.GetDataResponse, error) {
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

// Creates/Updates/Deletes a document. Roughly equivalent to the Data REST API's PATCH method.
func (s *Server) UpdateData(ctx context.Context, req *datav1.UpdateDataRequest) (*datav1.UpdateDataResponse, error) {
	// Start a transaction, so that we can do reads/writes.
	txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
	if err != nil {
		return nil, status.Error(codes.Internal, "transaction failed")
	}

	resp, err := s.updateDataFromRequest(ctx, txn, req, nil)
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

// Deletes a document. Equivalent to the Data REST API's DELETE method.
func (s *Server) DeleteData(ctx context.Context, req *datav1.DeleteDataRequest) (*datav1.DeleteDataResponse, error) {
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

// Handles streaming Data read/write operations.
// Only truly fatal errors should cause it to return a non-nil error to the gRPC client.
func (s *Server) StreamingDataRW(stream datav1.DataService_StreamingDataRWServer) error {
	ctx := stream.Context()
	for {
		// Check context to allow cancellation.
		if err := ctx.Err(); err != nil {
			return err
		}

		resp := datav1.StreamingDataRWResponse{}
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
				resp.Writes, err = s.streamingDataRWHandleWritesParallel(ctx, txn, writes)
				if err != nil {
					s.store.Abort(ctx, txn)
					return err
				}

				s.store.Commit(ctx, txn)
			}
			// Process reads in parallel, if present.
			reads := req.GetReads()
			if len(reads) > 0 {
				resp.Reads, err = s.streamingDataRWHandleReadsParallel(ctx, reads)
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
