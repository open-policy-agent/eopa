package grpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/storage"
	"golang.org/x/sync/errgroup"

	bjson "github.com/styrainc/load-private/pkg/json"
	loadv1 "github.com/styrainc/load-private/proto/gen/go/load/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BulkRW implements a fixed-structure, one-off transaction format.
// During execution, the handler performs the following operations in order:
//   - Open write transaction on the store.
//     -- Parse Policy payloads in parallel.
//     -- Write all Policy update requests sequentially to the store.
//     -- Parse all Data JSON payloads in parallel.
//     -- Write all Data update requests sequentially to the store.
//     -- If any failures occur, return error, abort transaction.
//   - Commit write transaction.
//   - Perform all Policy read operations in parallel, with per-request read transactions.
//   - Perform all Data read operations in parallel, with per-request read transactions.
//   - Return results to caller.
//
// This 1 + many transaction sequence improves write performance, since
// there is less mutex-wrangling involved overall. The Read operations are
// run in parallel to allow for greater throughput.

// Parsing function for individual Policy write payloads.
func bulkRWParsePolicyFromRequest(req *loadv1.BulkRWRequest_WritePolicyRequest) (*ast.Module, error) {
	var path string
	var rawPolicy string

	switch x := req.GetReq().(type) {
	case *loadv1.BulkRWRequest_WritePolicyRequest_Create:
		wr := x.Create
		path = wr.GetPath()
		rawPolicy = wr.GetPolicy()
	case *loadv1.BulkRWRequest_WritePolicyRequest_Update:
		wr := x.Update
		path = wr.GetPath()
		rawPolicy = wr.GetPolicy()
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

// Parsing function for individual Data write payloads.
// Returns a bjson.BJSON under-the-hood.
func bulkRWParseDataFromRequest(req *loadv1.BulkRWRequest_WriteDataRequest) (interface{}, error) {
	var data string

	switch x := req.GetReq().(type) {
	case *loadv1.BulkRWRequest_WriteDataRequest_Create:
		wr := x.Create
		data = wr.GetData()
	case *loadv1.BulkRWRequest_WriteDataRequest_Update:
		wr := x.Update
		data = wr.GetData()
	default:
		// All other types.
		return nil, nil
	}

	val, err := bjson.NewDecoder(strings.NewReader(data)).Decode()
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid data: %v", err)
	}

	return val, nil
}

// BulkRW endpoint handler.
func (s *Server) BulkRW(ctx context.Context, req *loadv1.BulkRWRequest) (*loadv1.BulkRWResponse, error) {
	out := loadv1.BulkRWResponse{}
	// Open initial write transaction.
	{
		txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
		if err != nil {
			return nil, status.Error(codes.Internal, "write transaction failed")
		}
		// Parse policy code in parallel.
		// TODO(philip): We're doing this the naive way initially for
		// simplicity, parsing everything up front before starting writes
		// to the store. Later, a better architecture would be to do the
		// parsing in parallel *while writes are also happening*, blocking
		// until a parsed module is available if needed.
		writesPolicy := req.GetWritesPolicy()
		if len(writesPolicy) > 0 {
			parsedPolicies := make([]*ast.Module, len(writesPolicy))
			wg, errCtx := errgroup.WithContext(ctx)
			for i := range writesPolicy {
				// Note(philip): This local copy of i is necessary,
				// otherwise the goroutine will refer to the loop's
				// iterator variable directly, which will mutate over time
				// unpredictably.
				// Reference: https://github.com/golang/go/wiki/CommonMistakes#using-reference-to-loop-iterator-variable
				i := i
				wg.Go(func() error {
					select {
					case <-errCtx.Done():
						return errCtx.Err()
					default:
						parsedMod, err := bulkRWParsePolicyFromRequest(writesPolicy[i])
						if err != nil {
							<-errCtx.Done()
							return err
						}
						parsedPolicies[i] = parsedMod
						//<-ctx.Done()
						return nil
					}
				})
			}
			if err := wg.Wait(); err != nil {
				s.store.Abort(ctx, txn)
				return nil, err
			}

			// Process policy writes sequentially.
			// Errors coming from the xFromRequest functions should all be
			// pre-wrapped with status.Error, so we can simply forward them
			// up the call chain.
			out.WritesPolicy = make([]*loadv1.BulkRWResponse_WritePolicyResponse, len(writesPolicy))
			for i := range writesPolicy {
				switch x := writesPolicy[i].GetReq().(type) {
				case *loadv1.BulkRWRequest_WritePolicyRequest_Create:
					wr := x.Create
					resp, err := s.createPolicyFromRequest(ctx, txn, wr, parsedPolicies[i])
					if err != nil {
						s.store.Abort(ctx, txn)
						return nil, err
					}
					out.WritesPolicy[i] = &loadv1.BulkRWResponse_WritePolicyResponse{Resp: &loadv1.BulkRWResponse_WritePolicyResponse_Create{Create: resp}}
				case *loadv1.BulkRWRequest_WritePolicyRequest_Update:
					wr := x.Update
					resp, err := s.updatePolicyFromRequest(ctx, txn, wr, parsedPolicies[i])
					if err != nil {
						s.store.Abort(ctx, txn)
						return nil, err
					}
					out.WritesPolicy[i] = &loadv1.BulkRWResponse_WritePolicyResponse{Resp: &loadv1.BulkRWResponse_WritePolicyResponse_Update{Update: resp}}
				case *loadv1.BulkRWRequest_WritePolicyRequest_Delete:
					wr := x.Delete
					resp, err := s.deletePolicyFromRequest(ctx, txn, wr)
					if err != nil {
						s.store.Abort(ctx, txn)
						return nil, err
					}
					out.WritesPolicy[i] = &loadv1.BulkRWResponse_WritePolicyResponse{Resp: &loadv1.BulkRWResponse_WritePolicyResponse_Delete{Delete: resp}}
				case nil:
					// Field was not set.
					s.store.Abort(ctx, txn)
					return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("empty policy write request at index: %d", i))
				default:
					// Unknown type?
					s.store.Abort(ctx, txn)
					return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("unknown type for policy write request at index: %d", i))
				}
			}
		}
		// Process data writes sequentially.
		// Errors coming from the xFromRequest functions should all be
		// pre-wrapped with status.Error, so we can simply forward them
		// up the call chain.
		writesData := req.GetWritesData()
		if len(writesData) > 0 {
			parsedData := make([]interface{}, len(writesData))
			wg, errCtx := errgroup.WithContext(ctx)
			for i := range writesData {
				// Note(philip): This local copy of i is necessary,
				// otherwise the goroutine will refer to the loop's
				// iterator variable directly, which will mutate over time
				// unpredictably.
				// Reference: https://github.com/golang/go/wiki/CommonMistakes#using-reference-to-loop-iterator-variable
				i := i
				wg.Go(func() error {
					select {
					case <-errCtx.Done():
						return errCtx.Err()
					default:
						parsedDataItem, err := bulkRWParseDataFromRequest(writesData[i])
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
				s.store.Abort(ctx, txn)
				return nil, err
			}

			out.WritesData = make([]*loadv1.BulkRWResponse_WriteDataResponse, len(writesData))
			for i := range writesData {
				switch x := writesData[i].GetReq().(type) {
				case *loadv1.BulkRWRequest_WriteDataRequest_Create:
					wr := x.Create
					resp, err := s.createDataFromRequest(ctx, txn, wr, parsedData[i])
					if err != nil {
						s.store.Abort(ctx, txn)
						return nil, err
					}
					out.WritesData[i] = &loadv1.BulkRWResponse_WriteDataResponse{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Create{Create: resp}}
				case *loadv1.BulkRWRequest_WriteDataRequest_Update:
					wr := x.Update
					resp, err := s.updateDataFromRequest(ctx, txn, wr, parsedData[i])
					if err != nil {
						s.store.Abort(ctx, txn)
						return nil, err
					}
					out.WritesData[i] = &loadv1.BulkRWResponse_WriteDataResponse{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Update{Update: resp}}
				case *loadv1.BulkRWRequest_WriteDataRequest_Delete:
					wr := x.Delete
					resp, err := s.deleteDataFromRequest(ctx, txn, wr)
					if err != nil {
						s.store.Abort(ctx, txn)
						return nil, err
					}
					out.WritesData[i] = &loadv1.BulkRWResponse_WriteDataResponse{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Delete{Delete: resp}}
				case nil:
					// Field was not set.
					s.store.Abort(ctx, txn)
					return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("empty data write request at index: %d", i))
				default:
					// Unknown type?
					s.store.Abort(ctx, txn)
					return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("unknown type for data write request at index: %d", i))
				}
			}
		}
		if err := s.store.Commit(ctx, txn); err != nil {
			s.store.Abort(ctx, txn)
			return nil, err
		}
	}
	// Open read transaction(s).
	{
		// Process policy reads in parallel.
		// We skip all allocs / worker pool creation if no policy reads are required.
		readsPolicy := req.GetReadsPolicy()
		if len(readsPolicy) > 0 {
			out.ReadsPolicy = make([]*loadv1.BulkRWResponse_ReadPolicyResponse, len(readsPolicy))
			wg, errCtx := errgroup.WithContext(ctx)
			for i := range readsPolicy {
				// Note(philip): This local copy of i is necessary,
				// otherwise the goroutine will refer to the loop's
				// iterator variable directly, which will mutate over time
				// unpredictably.
				// Reference: https://github.com/golang/go/wiki/CommonMistakes#using-reference-to-loop-iterator-variable
				i := i
				wg.Go(func() error {
					select {
					case <-errCtx.Done():
						return errCtx.Err()
					default:
						txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
						if err != nil {
							return status.Error(codes.Internal, "read transaction failed")
						}
						policyReadReq := readsPolicy[i].GetReq() // TODO: Nil-checks here?
						resp, err := s.getPolicyFromRequest(ctx, txn, policyReadReq)
						if err != nil {
							s.store.Abort(ctx, txn)
							out.ReadsPolicy[i] = &loadv1.BulkRWResponse_ReadPolicyResponse{Errors: &loadv1.ErrorList{Errors: []string{err.Error()}}}
							return nil
						}
						out.ReadsPolicy[i] = &loadv1.BulkRWResponse_ReadPolicyResponse{Resp: resp}
						s.store.Abort(ctx, txn)
						return nil
					}
				})
			}
			if err := wg.Wait(); err != nil {
				return nil, err
			}
		}

		// Process data reads in parallel.
		// We skip all allocs / worker pool creation if no data reads are required.
		readsData := req.GetReadsData()
		if len(readsData) > 0 {
			out.ReadsData = make([]*loadv1.BulkRWResponse_ReadDataResponse, len(readsData))
			wg, errCtx := errgroup.WithContext(ctx)
			for i := range readsData {
				// Note(philip): This local copy of i is necessary,
				// otherwise the goroutine will refer to the loop's
				// iterator variable directly, which will mutate over time
				// unpredictably.
				// Reference: https://github.com/golang/go/wiki/CommonMistakes#using-reference-to-loop-iterator-variable
				i := i
				wg.Go(func() error {
					select {
					case <-errCtx.Done():
						return errCtx.Err()
					default:
						txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
						if err != nil {
							return status.Error(codes.Internal, "read transaction failed")
						}
						dataReadReq := readsData[i].GetReq() // TODO: Nil-checks here?
						resp, err := s.getDataFromRequest(ctx, txn, dataReadReq)
						if err != nil {
							s.store.Abort(ctx, txn)
							out.ReadsData[i] = &loadv1.BulkRWResponse_ReadDataResponse{Errors: &loadv1.ErrorList{Errors: []string{err.Error()}}}
							return nil
						}
						out.ReadsData[i] = &loadv1.BulkRWResponse_ReadDataResponse{Resp: resp}
						s.store.Abort(ctx, txn)
						return nil
					}
				})
			}
			if err := wg.Wait(); err != nil {
				return nil, err
			}
		}
	}

	return &out, nil
}
