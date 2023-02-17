package grpc

import (
	"context"
	"io"
	"strings"

	datav1 "github.com/styrainc/load-private/gen/proto/go/apis/data/v1"
	bjson "github.com/styrainc/load-private/pkg/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/open-policy-agent/opa/storage"
)

type srv struct {
	store storage.Store
	datav1.UnimplementedLoadServiceServer
}

type pathValuePair struct {
	path  string
	value interface{}
}

func New(store storage.Store) *grpc.Server {
	s := grpc.NewServer()
	datav1.RegisterLoadServiceServer(s, &srv{store: store})
	reflection.Register(s)
	return s
}

// ----------------------------------------------------------------------------------------------------------------------------------------------------
func (s *srv) ReadData(ctx context.Context, req *datav1.ReadDataRequest) (*datav1.ReadDataResponse, error) {
	path := req.GetPath()
	p, ok := storage.ParsePath(path)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid path")
	}
	// Read single value from the store:
	v, err := storage.ReadOne(ctx, s.store, p)
	if err != nil {
		return nil, err
	}
	// Return bjson value from store:
	bjsonItem, err := bjson.New(v)
	if err != nil {
		return nil, err
	}
	bs := bjsonItem.String()
	return &datav1.ReadDataResponse{Path: path, Data: bs}, nil
}

func (s *srv) WriteData(ctx context.Context, req *datav1.WriteDataRequest) (*datav1.WriteDataResponse, error) {
	path := req.GetPath()
	p, ok := storage.ParsePath(path)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid path")
	}
	val, err := bjson.NewDecoder(strings.NewReader(req.GetData())).Decode()
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid data: %v", err)
	}
	// Write single value to the store:
	var patchOp storage.PatchOp
	op := req.GetOperation()
	switch op {
	case "add":
		patchOp = storage.AddOp
	case "patch", "replace":
		patchOp = storage.ReplaceOp
	case "remove":
		patchOp = storage.RemoveOp
	default:
		return nil, status.Errorf(codes.InvalidArgument, "invalid op: %v", op)
	}
	if err := storage.WriteOne(ctx, s.store, patchOp, p, val); err != nil {
		return nil, err
	}
	return &datav1.WriteDataResponse{Path: path}, nil
}

// Applies multiple store writes in a single store transaction.
// Because the transaction is a parameter, this allows squeezing in more store operations on an externally-managed transaction.
func (s *srv) upsertMany(ctx context.Context, txn storage.Transaction, upserts []pathValuePair) error {
	for i := range upserts {
		pvp := upserts[i]
		path := pvp.path
		value := pvp.value
		p, ok := storage.ParsePath(path)
		if !ok {
			return status.Error(codes.InvalidArgument, "invalid path")
		}
		if len(p) > 1 {
			if err := storage.MakeDir(ctx, s.store, txn, p[:len(p)-1]); err != nil {
				return err
			}
		}
		if err := s.store.Write(ctx, txn, storage.ReplaceOp, p, value); err != nil {
			if !storage.IsNotFound(err) {
				return err
			}
			if err := s.store.Write(ctx, txn, storage.AddOp, p, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *srv) readMany(ctx context.Context, txn storage.Transaction, paths []string) ([]interface{}, error) {
	results := make([]interface{}, 0, len(paths))
	for i := range paths {
		p, ok := storage.ParsePath(paths[i])
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "invalid path")
		}
		if v, err := s.store.Read(ctx, txn, p); err == nil {
			results = append(results, v)
		} else {
			if !storage.IsNotFound(err) {
				return nil, err
			}
		}
	}
	return results, nil
}

// Note(philip): We've inlined the transaction-handling logic from
// storage.Txn so that we don't have to use a closure here. The Go compiler
// still struggles with optimizing closures, and this also gives us *very*
// fine-grained control over transaction behavior.
func (s *srv) RWDataTransactionStream(stream datav1.LoadService_RWDataTransactionStreamServer) error {
	ctx := stream.Context()
	for {
		// Check context to allow cancellation.
		if err := ctx.Err(); err != nil {
			return err
		}
		resp := datav1.RWDataTransactionStreamResponse{}
		switch req, err := stream.Recv(); err {
		case nil:
			txnID := req.GetId()
			if txnID != "" {
				resp.Id = txnID
			}
			writes := req.GetWrites()
			params := storage.TransactionParams{Write: len(writes) > 0}

			txn, err := s.store.NewTransaction(ctx, params)
			if err != nil {
				return err
			}

			// Run batched writes.
			upserts := make([]pathValuePair, 0, len(writes))
			for i := range writes {
				val, err := bjson.NewDecoder(strings.NewReader(writes[i].GetData())).Decode()
				if err != nil {
					return status.Errorf(codes.InvalidArgument, "invalid data: %v", err)
				}
				upserts = append(upserts, pathValuePair{path: writes[i].GetPath(), value: val})
			}
			if err := s.upsertMany(stream.Context(), txn, upserts); err != nil {
				s.store.Abort(ctx, txn)
				return err
			}
			// Prepare write responses.
			outWrites := make([]*datav1.WriteDataResponse, 0, len(writes))
			for i := range writes {
				outWrites = append(outWrites, &datav1.WriteDataResponse{Path: writes[i].GetPath()})
			}
			resp.Writes = outWrites

			// Run batched reads, and return responses.
			reads := req.GetReads()
			readPaths := make([]string, 0, len(reads))
			outReads := make([]*datav1.ReadDataResponse, 0, len(reads))
			for i := range reads {
				readPaths = append(readPaths, reads[i].GetPath())
			}
			if results, err := s.readMany(ctx, txn, readPaths); err == nil {
				for i := range results {
					bjsonItem, err := bjson.New(results[i])
					if err != nil {
						s.store.Abort(ctx, txn)
						return err
					}
					bs := bjsonItem.String()
					outReads = append(outReads, &datav1.ReadDataResponse{Path: readPaths[i], Data: bs})
				}
				resp.Reads = outReads
			}

			s.store.Commit(ctx, txn)
			stream.Send(&resp)

		case io.EOF:
			return nil

		default:
			return err
		}
	}
}
