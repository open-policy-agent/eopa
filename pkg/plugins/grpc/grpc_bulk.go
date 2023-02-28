package grpc

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/storage"

	loadv1 "github.com/styrainc/load-private/proto/gen/go/load/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BulkRW implements a fixed-structure, one-off transaction format.
// During execution, the handler performs the following operations in order:
//   - Open write transaction on the store.
//     -- Write all Policy update requests sequentially to the store.
//     -- Write all Data update requests sequentially to the store.
//     -- If any failures occur, return error, abort transaction.
//   - Commit write transaction.
//   - Open read transaction on the store.
//     -- Perform all Policy read operations in arbitrary order.
//     -- Perform all Data read operations in arbitrary order.
//   - Abort/commit read transaction.
//   - Return results to caller.
//
// This 2x transaction sequence is expected to improve write performance,
// since there will be less mutex-wrangling involved overall. The second
// transaction is used for the reads/query execution stage, because those
// can take arbitrary amounts of time to complete, and we'd like to not
// lock up the store for any longer than strictly necessary.
//
// There is an opportunity for intra-request parallelism on the Data reads
// and ad-hoc queries, since we only need to get the results reassembled
// correctly at the end. A fun option for this might be to allow a
// max-request-parallelism option for the transaction, which will launch a
// worker pool of goroutines to chew through the read/query job list. As
// long as the results are reassembled correctly at the end for returning
// to the client, this could work nicely.
func (s *Server) BulkRW(ctx context.Context, req *loadv1.BulkRWRequest) (*loadv1.BulkRWResponse, error) {
	out := loadv1.BulkRWResponse{}
	// Open initial write transaction.
	{
		txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext(), Write: true})
		if err != nil {
			return nil, status.Error(codes.Internal, "write transaction failed")
		}
		// Process policy writes sequentially.
		// Errors coming from the xFromRequest functions should all be
		// pre-wrapped with status.Error, so we can simply forward them
		// up the call chain.
		writesPolicy := req.GetWritesPolicy()
		out.WritesPolicy = make([]*loadv1.BulkRWResponse_WritePolicyResponse, len(writesPolicy))
		for i := range writesPolicy {
			switch x := writesPolicy[i].GetReq().(type) {
			case *loadv1.BulkRWRequest_WritePolicyRequest_Create:
				wr := x.Create
				resp, err := s.createPolicyFromRequest(ctx, txn, wr)
				if err != nil {
					s.store.Abort(ctx, txn)
					return nil, err
				}
				out.WritesPolicy[i] = &loadv1.BulkRWResponse_WritePolicyResponse{Resp: &loadv1.BulkRWResponse_WritePolicyResponse_Create{Create: resp}}
			case *loadv1.BulkRWRequest_WritePolicyRequest_Update:
				wr := x.Update
				resp, err := s.updatePolicyFromRequest(ctx, txn, wr)
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
		// Process data writes sequentially.
		// Errors coming from the xFromRequest functions should all be
		// pre-wrapped with status.Error, so we can simply forward them
		// up the call chain.
		writesData := req.GetWritesData()
		out.WritesData = make([]*loadv1.BulkRWResponse_WriteDataResponse, len(writesData))
		for i := range writesData {
			switch x := writesData[i].GetReq().(type) {
			case *loadv1.BulkRWRequest_WriteDataRequest_Create:
				wr := x.Create
				resp, err := s.createDataFromRequest(ctx, txn, wr)
				if err != nil {
					s.store.Abort(ctx, txn)
					return nil, err
				}
				out.WritesData[i] = &loadv1.BulkRWResponse_WriteDataResponse{Resp: &loadv1.BulkRWResponse_WriteDataResponse_Create{Create: resp}}
			case *loadv1.BulkRWRequest_WriteDataRequest_Update:
				wr := x.Update
				resp, err := s.updateDataFromRequest(ctx, txn, wr)
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
		if err := s.store.Commit(ctx, txn); err != nil {
			s.store.Abort(ctx, txn)
			return nil, err
		}
	}
	// Open read transaction.
	{
		txn, err := s.store.NewTransaction(ctx, storage.TransactionParams{Context: storage.NewContext()})
		if err != nil {
			return nil, status.Error(codes.Internal, "read transaction failed")
		}

		// Process policy reads sequentially.
		readsPolicy := req.GetReadsPolicy()
		out.ReadsPolicy = make([]*loadv1.BulkRWResponse_ReadPolicyResponse, len(readsPolicy))
		for i := range readsPolicy {
			policyReadReq := readsPolicy[i].GetReq() // TODO: Nil-checks here?
			resp, err := s.getPolicyFromRequest(ctx, txn, policyReadReq)
			if err != nil {
				// s.store.Abort(ctx, txn)
				// return nil, err
				out.ReadsPolicy[i] = &loadv1.BulkRWResponse_ReadPolicyResponse{Errors: &loadv1.ErrorList{Errors: []string{err.Error()}}}
				continue
			}
			out.ReadsPolicy[i] = &loadv1.BulkRWResponse_ReadPolicyResponse{Resp: resp}
		}
		// Process data reads sequentially.
		readsData := req.GetReadsData()
		out.ReadsData = make([]*loadv1.BulkRWResponse_ReadDataResponse, len(readsData))
		for i := range readsData {
			dataReadReq := readsData[i].GetReq() // TODO: Nil-checks here?
			resp, err := s.getDataFromRequest(ctx, txn, dataReadReq)
			if err != nil {
				// s.store.Abort(ctx, txn)
				// return nil, err
				out.ReadsData[i] = &loadv1.BulkRWResponse_ReadDataResponse{Errors: &loadv1.ErrorList{Errors: []string{err.Error()}}}
				continue
			}
			out.ReadsData[i] = &loadv1.BulkRWResponse_ReadDataResponse{Resp: resp}
		}

		if err := s.store.Commit(ctx, txn); err != nil {
			s.store.Abort(ctx, txn)
			return nil, err
		}
	}

	return &out, nil
}
