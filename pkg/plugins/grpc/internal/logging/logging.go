package grpc_logging

import (
	"context"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/open-policy-agent/opa/v1/logging"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func remoteAddrFromContext(ctx context.Context) string {
	p, _ := peer.FromContext(ctx)
	remoteAddr := "unknown"
	if p != nil {
		addr := p.Addr
		if tcpAddr, ok := addr.(*net.TCPAddr); ok {
			remoteAddr = tcpAddr.IP.String() + ":" + strconv.FormatInt(int64(tcpAddr.Port), 10)
		}
	}
	return remoteAddr
}

// Reduced version of the OPA HTTP request logging wrapper. The function
// generates a unary request interceptor for the gRPC server that is
// pre-loaded with the correct logger to use.
func NewLoggingUnaryServerInterceptor(counter *uint64, logger logging.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, inner grpc.UnaryHandler) (resp any, err error) {
		var rctx logging.RequestContext
		rctx.ReqID = atomic.AddUint64(counter, uint64(1))
		t0 := time.Now()

		if logging.Info <= logger.GetLevel() {
			rctx.ClientAddr = remoteAddrFromContext(ctx)
			rctx.ReqMethod = info.FullMethod
			// rctx.ReqPath = req.path

			fields := rctx.Fields()

			logger.WithFields(fields).Info("Received request.")
		}

		ctx = logging.NewContext(ctx, &rctx)
		resp, err = inner(ctx, req)
		dt := time.Since(t0)

		if logging.Info <= logger.GetLevel() {
			fields := map[string]any{
				"client_addr": rctx.ClientAddr,
				"req_id":      rctx.ReqID,
				"req_method":  rctx.ReqMethod,
				"req_path":    rctx.ReqPath,
				"resp_status": status.Code(err),
				// "resp_bytes":    recorder.bytesWritten, // TODO(philip): Figure out how to get a similar gRPC metric.
				"resp_duration": float64(dt.Nanoseconds()) / 1e6,
			}

			logger.WithFields(fields).Info("Sent response.")
		}
		return resp, err
	}
}

// Borrowed straight from the Go gRPC examples.
// Ref: https://github.com/grpc/grpc-go/examples/features/metadata_interceptor/server/main.go
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *wrappedStream) Context() context.Context {
	return s.ctx
}

// Reduced version of the OPA HTTP request logging wrapper. The function
// generates a stream request interceptor for the gRPC server that is
// pre-loaded with the correct logger to use.
func NewLoggingStreamServerInterceptor(counter *uint64, logger logging.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		var rctx logging.RequestContext
		rctx.ReqID = atomic.AddUint64(counter, uint64(1))
		ctx := ss.Context()
		t0 := time.Now()

		if logging.Info <= logger.GetLevel() {
			rctx.ClientAddr = remoteAddrFromContext(ctx)
			rctx.ReqMethod = info.FullMethod
			// rctx.ReqPath = req.path // Can be added in more easily at a lower level, when request is unpacked.

			fields := rctx.Fields()

			logger.WithFields(fields).Info("Received request.")
		}

		// Note(philip): Can't pre-load the merged logging context here.
		// gRPC streams don't let you mangle the context, unfortunately.
		updatedCtx := logging.NewContext(ctx, &rctx)
		err := handler(srv, &wrappedStream{ss, updatedCtx})
		dt := time.Since(t0)

		if logging.Info <= logger.GetLevel() {
			fields := map[string]any{
				"client_addr": rctx.ClientAddr,
				"req_id":      rctx.ReqID,
				"req_method":  rctx.ReqMethod,
				"req_path":    rctx.ReqPath,
				"resp_status": status.Code(err),
				// "resp_bytes":    recorder.bytesWritten, // TODO(philip): Figure out how to get a similar gRPC metric.
				"resp_duration": float64(dt.Nanoseconds()) / 1e6,
			}

			logger.WithFields(fields).Info("Sent response.")
		}
		return err
	}
}
