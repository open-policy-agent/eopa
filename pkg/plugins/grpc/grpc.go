// Package grpc provides the implementation of Load's gRPC server. It is
// modeled directly off of OPA's HTTP Server implementation, and borrows as
// much code from OPA as is reasonable.
//
// Several features of the OPA HTTP Server are missing, notably:
//   - TLS support
//   - Logging
//   - Metrics
//   - Provenance
//   - Tracing/Explain
package grpc

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/server/types"
	"github.com/open-policy-agent/opa/topdown"
	iCache "github.com/open-policy-agent/opa/topdown/cache"
	"github.com/styrainc/load-private/pkg/plugins/bundle"
	loadv1 "github.com/styrainc/load-private/proto/gen/go/load/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
)

// General gRPC server utilities.
type Server struct {
	mtx                 sync.RWMutex
	preparedEvalQueries *cache
	manager             *plugins.Manager
	grpcServer          *grpc.Server
	decisionIDFactory   func() string
	// logger                 func(context.Context, *Info) error
	// metrics                Metrics
	interQueryBuiltinCache iCache.InterQueryCache
	// allPluginsOkOnce       bool
	runtimeData     *ast.Term
	ndbCacheEnabled bool
	store           storage.Store
	loadv1.UnimplementedDataServiceServer
	loadv1.UnimplementedPolicyServiceServer
	loadv1.UnimplementedBulkServiceServer
}

// type pathValuePair struct {
// 	path  string
// 	value interface{}
// }

const pqMaxCacheSize = 100

// map of unsafe builtins
var unsafeBuiltinsMap = map[string]struct{}{ast.HTTPSend.Name: {}}

func New(manager *plugins.Manager) *Server {
	grpcServer := grpc.NewServer()

	server := Server{
		mtx:                    sync.RWMutex{},
		preparedEvalQueries:    newCache(pqMaxCacheSize),
		manager:                manager,
		grpcServer:             grpcServer,
		interQueryBuiltinCache: iCache.NewInterQueryCache(manager.InterQueryBuiltinCacheConfig()),
		store:                  manager.Store,
	}

	loadv1.RegisterDataServiceServer(grpcServer, &server)
	loadv1.RegisterPolicyServiceServer(grpcServer, &server)
	loadv1.RegisterBulkServiceServer(grpcServer, &server)
	reflection.Register(grpcServer)
	return &server
}

func (s *Server) Serve(lis net.Listener) error {
	return s.grpcServer.Serve(lis)
}

func (s *Server) Stop() {
	s.grpcServer.Stop()
}

func (s *Server) GracefulStop() {
	s.grpcServer.GracefulStop()
}

// Borrowed functions from open-policy-agent/opa/server/server.go:

// WithDecisionIDFactory sets a function on the server to generate decision IDs.
func (s *Server) WithDecisionIDFactory(f func() string) *Server {
	s.decisionIDFactory = f
	return s
}

// WithRuntimeData sets the runtime data to provide to the evaluation engine.
func (s *Server) WithRuntimeData(term *ast.Term) *Server {
	s.runtimeData = term
	return s
}

// Utility function for path reachability.
func isPathOwned(path, root []string) bool {
	for i := 0; i < len(path) && i < len(root); i++ {
		if path[i] != root[i] {
			return false
		}
	}
	return true
}

// Pulls a policy out of the store by ID, and then performs a bundle safety check against its package path.
func (s *Server) checkPolicyIDScope(ctx context.Context, txn storage.Transaction, id string) error {
	bs, err := s.store.GetPolicy(ctx, txn, id)
	if err != nil {
		return err
	}

	module, err := ast.ParseModule(id, string(bs))
	if err != nil {
		return err
	}

	return s.checkPolicyPackageScope(ctx, txn, module.Package)
}

// Unravels a package's path, and delegates to checkPathScope for the bundle safety check.
func (s *Server) checkPolicyPackageScope(ctx context.Context, txn storage.Transaction, pkg *ast.Package) error {
	path, err := pkg.Path.Ptr()
	if err != nil {
		return err
	}

	spath, ok := storage.ParsePathEscaped("/" + path)
	if !ok {
		return types.BadRequestErr("invalid package path: cannot determine scope")
	}

	return s.checkPathScope(ctx, txn, spath)
}

// Safety check, used to prevent overwriting parts of bundles in the store.
func (s *Server) checkPathScope(ctx context.Context, txn storage.Transaction, path storage.Path) error {
	names, err := bundle.ReadBundleNamesFromStore(ctx, s.store, txn)
	if err != nil {
		if !storage.IsNotFound(err) {
			return err
		}
		return nil
	}

	bundleRoots := map[string][]string{}
	for _, name := range names {
		roots, err := bundle.ReadBundleRootsFromStore(ctx, s.store, txn, name)
		if err != nil && !storage.IsNotFound(err) {
			return err
		}
		bundleRoots[name] = roots
	}

	spath := strings.Trim(path.String(), "/")

	if spath == "" && len(bundleRoots) > 0 {
		return types.BadRequestErr("can't write to document root with bundle roots configured")
	}

	spathParts := strings.Split(spath, "/")

	for name, roots := range bundleRoots {
		if roots == nil {
			return types.BadRequestErr(fmt.Sprintf("all paths owned by bundle %q", name))
		}
		for _, root := range roots {
			if root == "" {
				return types.BadRequestErr(fmt.Sprintf("all paths owned by bundle %q", name))
			}
			if isPathOwned(spathParts, strings.Split(root, "/")) {
				return types.BadRequestErr(fmt.Sprintf("path %v is owned by bundle %q", spath, name))
			}
		}
	}

	return nil
}

func (s *Server) loadModules(ctx context.Context, txn storage.Transaction) (map[string]*ast.Module, error) {
	ids, err := s.store.ListPolicies(ctx, txn)
	if err != nil {
		return nil, err
	}

	modules := make(map[string]*ast.Module, len(ids))

	for _, id := range ids {
		bs, err := s.store.GetPolicy(ctx, txn, id)
		if err != nil {
			return nil, err
		}

		parsed, err := ast.ParseModule(id, string(bs))
		if err != nil {
			return nil, err
		}

		modules[id] = parsed
	}

	return modules, nil
}

func (s *Server) getCompiler() *ast.Compiler {
	return s.manager.GetCompiler()
}

// func (s *Server) getCachedPreparedEvalQuery(key string, m metrics.Metrics) (*rego.PreparedEvalQuery, bool) {
func (s *Server) getCachedPreparedEvalQuery(key string) (*rego.PreparedEvalQuery, bool) {
	pq, ok := s.preparedEvalQueries.Get(key)
	// m.Counter(metrics.ServerQueryCacheHit) // Creates the counter on the metrics if it doesn't exist, starts at 0
	if ok {
		// m.Counter(metrics.ServerQueryCacheHit).Incr() // Increment counter on hit
		return pq.(*rego.PreparedEvalQuery), true
	}
	return nil, false
}

func stringPathToRef(s string) (r ast.Ref) {
	if len(s) == 0 {
		return r
	}
	p := strings.Split(s, "/")
	for _, x := range p {
		if x == "" {
			continue
		}
		if y, err := url.PathUnescape(x); err == nil {
			x = y
		}
		i, err := strconv.Atoi(x)
		if err != nil {
			r = append(r, ast.StringTerm(x))
		} else {
			r = append(r, ast.IntNumberTerm(i))
		}
	}
	return r
}

// func validateQuery(query string) (ast.Body, error) {
// 	return ast.ParseBody(query)
// }

func stringPathToDataRef(s string) (r ast.Ref) {
	result := ast.Ref{ast.DefaultRootDocument}
	result = append(result, stringPathToRef(s)...)
	return result
}

func (s *Server) makeRego(ctx context.Context,
	strictBuiltinErrors bool,
	txn storage.Transaction,
	input ast.Value,
	urlPath string,
	// m metrics.Metrics,
	instrument bool,
	tracer topdown.QueryTracer,
	opts []func(*rego.Rego),
) (*rego.Rego, error) {
	queryPath := stringPathToDataRef(urlPath).String()

	opts = append(
		opts,
		rego.Transaction(txn),
		rego.Query(queryPath),
		rego.ParsedInput(input),
		// rego.Metrics(m),
		rego.QueryTracer(tracer),
		rego.Instrument(instrument),
		rego.Runtime(s.runtimeData),
		rego.UnsafeBuiltins(unsafeBuiltinsMap),
		rego.StrictBuiltinErrors(strictBuiltinErrors),
		rego.PrintHook(s.manager.PrintHook()),
		// rego.DistributedTracingOpts(s.distributedTracingOpts),
	)

	return rego.New(opts...), nil
}

// func (s *Server) generateDecisionID() string {
// 	if s.decisionIDFactory != nil {
// 		return s.decisionIDFactory()
// 	}
// 	return ""
// }
