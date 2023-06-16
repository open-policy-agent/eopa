// Package grpc provides the implementation of Enterprise OPA's gRPC server.
// It is modeled directly off of OPA's HTTP Server implementation, and
// borrows as much code from OPA as is reasonable.
//
// Several features of the OPA HTTP Server are missing, notably:
//   - Logging
//   - Provenance
//   - Tracing/Explain
package grpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/server/types"
	"github.com/open-policy-agent/opa/topdown"
	iCache "github.com/open-policy-agent/opa/topdown/cache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/bundle"
	bulkv1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/bulk/v1"
	datav1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/data/v1"
	policyv1 "github.com/styrainc/enterprise-opa-private/proto/gen/go/eopa/policy/v1"
	"go.opentelemetry.io/otel/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
)

// AuthenticationScheme enumerates the supported authentication schemes. The
// authentication scheme determines how client identities are established.
type AuthenticationScheme int

// Set of supported authentication schemes.
const (
	AuthenticationOff AuthenticationScheme = iota
	AuthenticationToken
	AuthenticationTLS
)

var supportedTLSVersions = []uint16{tls.VersionTLS10, tls.VersionTLS11, tls.VersionTLS12, tls.VersionTLS13}

// AuthorizationScheme enumerates the supported authorization schemes. The authorization
// scheme determines how access to OPA is controlled.
type AuthorizationScheme int

// Set of supported authorization schemes.
const (
	AuthorizationOff AuthorizationScheme = iota
	AuthorizationBasic
)

const defaultMinTLSVersion = tls.VersionTLS12

// General gRPC server utilities.

// Note(philip): Logically, the running server is structured as a wrapper
// around an actual grpc.Server type. At runtime, the grpc.Server runs in
// its own goroutine, and is optionally supported by a certLoop goroutine
// that checks to see if certificates have changed on disk since the last
// refresh. If the certificate files changed, the certLoop replaces the
// in-memory certificate with an updated one. For more involved TLS
// reconfiguration, the entire gRPC plugin must be restarted.
type Server struct {
	mtx                    sync.RWMutex
	preparedEvalQueries    *cache
	manager                *plugins.Manager
	grpcServer             *grpc.Server
	decisionIDFactory      func() string
	metrics                *grpcprom.ServerMetrics
	interQueryBuiltinCache iCache.InterQueryCache

	runtimeData     *ast.Term
	ndbCacheEnabled bool
	store           storage.Store
	logger          logging.Logger // inherited from manager.

	authentication      AuthenticationScheme
	authorization       AuthorizationScheme
	cert                *tls.Certificate
	certMtx             sync.RWMutex
	certFilename        string
	certFileHash        []byte
	certKeyFilename     string
	certKeyFileHash     []byte
	certRefreshInterval time.Duration
	minTLSVersion       uint16

	tlsRootCACertFilename string
	certPool              *x509.CertPool

	certLoopHaltChannel             chan struct{}
	certLoopShutdownCompleteChannel chan struct{}

	datav1.UnimplementedDataServiceServer
	policyv1.UnimplementedPolicyServiceServer
	bulkv1.UnimplementedBulkServiceServer
}

const pqMaxCacheSize = 100

// map of unsafe builtins
var unsafeBuiltinsMap = map[string]struct{}{ast.HTTPSend.Name: {}}

// Validation of TLS config happens upstream in (factory).Validate().
func New(manager *plugins.Manager, config Config) *Server {
	options := make([]grpc.ServerOption, 0, 1)
	creds := insecure.NewCredentials()

	server := Server{
		mtx:                    sync.RWMutex{},
		preparedEvalQueries:    newCache(pqMaxCacheSize),
		manager:                manager,
		interQueryBuiltinCache: iCache.NewInterQueryCache(manager.InterQueryBuiltinCacheConfig()),
		store:                  manager.Store,
		logger:                 manager.Logger(),
		certFilename:           config.TLS.CertFile,
		certKeyFilename:        config.TLS.CertKeyFile,
		tlsRootCACertFilename:  config.TLS.RootCACertFile,
	}

	if config.MaxRecvMessageSize > 0 {
		options = append(options, grpc.MaxRecvMsgSize(config.MaxRecvMessageSize))
	}

	if config.Authentication != "" {
		server.authentication = getAuthenticationScheme(config.Authentication)
	}
	if config.Authorization != "" {
		server.authorization = getAuthorizationScheme(config.Authorization)
	}
	if config.TLS.MinVersion != "" {
		server.minTLSVersion = getMinTLSVersion(config.TLS.MinVersion)
	}
	if config.TLS.CertRefreshInterval != "" {
		// Use the "smuggled" value from upstream. Relies on (*factory).Validate.
		server.certRefreshInterval = config.TLS.validatedCertRefreshDuration
	}

	if tlsCreds := server.loadTLSCredentials(); tlsCreds != nil {
		creds = tlsCreds
	}
	options = append(options, grpc.Creds(creds))

	// Set up metrics.
	// Derived from the example at:
	//   https://github.com/grpc-ecosystem/go-grpc-middleware/blob/main/examples/server/main.go
	srvMetrics := grpcprom.NewServerMetrics(
		grpcprom.WithServerCounterOptions(
			grpcprom.CounterOption(func(o *prometheus.CounterOpts) {
				o.Namespace = "styra"
				o.Subsystem = "enterprise_opa"
			}),
		),
	)
	// Bind metrics to the manager's registerer if it exists.
	var metricsOption grpc.ServerOption
	metricsOK := false
	if reg := manager.PrometheusRegister(); reg != nil {
		// Note(philip): To avoid AlreadyRegisteredError incidents during
		// plugin.Reconfigure events, we unregister the server metrics, and
		// then re-register them immediately afterwards.
		reg.Unregister(srvMetrics)
		err := reg.Register(srvMetrics)
		if err != nil {
			server.manager.UpdatePluginStatus("grpc", &plugins.Status{State: plugins.StateErr, Message: "Failed to register Prometheus metrics: " + err.Error()})
			server.logger.Error("Failed to register Prometheus metrics: %s", err.Error())
			return nil
		}
		exemplarFromContext := func(ctx context.Context) prometheus.Labels {
			if span := trace.SpanContextFromContext(ctx); span.IsSampled() {
				return prometheus.Labels{"traceID": span.TraceID().String()}
			}
			return nil
		}
		metricsOption = grpc.ChainUnaryInterceptor(srvMetrics.UnaryServerInterceptor(grpcprom.WithExemplarFromContext(exemplarFromContext)))
		metricsOK = true
		options = append(options, metricsOption)
		server.metrics = srvMetrics // Hang on to this object for later unregistering.
	}

	// Fills in all grpc.Server-related parts of the Server struct.
	server.initGRPCServer(options...)
	if metricsOK {
		srvMetrics.InitializeMetrics(server.grpcServer)
	}

	return &server
}

func (s *Server) Serve(lis net.Listener) error {
	return s.grpcServer.Serve(lis)
}

func (s *Server) Stop() {
	if reg := s.manager.PrometheusRegister(); reg != nil {
		reg.Unregister(s.metrics)
	}
	s.grpcServer.Stop()
}

func (s *Server) GracefulStop() {
	if reg := s.manager.PrometheusRegister(); reg != nil {
		reg.Unregister(s.metrics)
	}
	s.grpcServer.GracefulStop()
}

// This function initializes a gRPC server from the Server state, namely
// certFile and certKeyFile. It should only be used by the plugin,
// certLoop, and (*Server).New() method.
func (s *Server) initGRPCServer(options ...grpc.ServerOption) error {
	var stopCertLoopChannel chan struct{}
	var certLoopShutdownCompleteChannel chan struct{}

	// Lock the server during the gRPC server swap out process.
	s.mtx.Lock()
	s.grpcServer = grpc.NewServer(options...)
	s.certLoopHaltChannel = stopCertLoopChannel
	s.certLoopShutdownCompleteChannel = certLoopShutdownCompleteChannel

	// The RegisterXXXServiceServer calls enable each service's API methods
	// individually. This allows generating code for multiple services, but
	// only using a subset of those services on a gRPC server instance.
	datav1.RegisterDataServiceServer(s.grpcServer, s)
	policyv1.RegisterPolicyServiceServer(s.grpcServer, s)
	bulkv1.RegisterBulkServiceServer(s.grpcServer, s)
	reflection.Register(s.grpcServer)
	s.mtx.Unlock()

	return nil
}

func (s *Server) loadTLSCredentials() credentials.TransportCredentials {
	// Load normal TLS public/private key pair.
	// Note(philip): All other TLS options are gated behind the server
	// being able to load up its own TLS config. As best I can tell, this
	// roughly mimics the OPA HTTPS connection logic.
	if s.certFilename != "" && s.certKeyFilename != "" {
		var tlsConfig tls.Config
		var err error
		// Attempt to load the server's initial cert key pair.
		// Note(philip): Loading the files up manually here instead of
		// using tls.LoadX509KeyPair() allows us to hit the disk just once,
		// and reuse the file contents for both use cases (cert, hashes).
		certPEMBlock, err := os.ReadFile(s.certFilename)
		if err != nil {
			s.logger.Error("Failed to reload server certificate: %s.", err.Error())
			s.manager.UpdatePluginStatus("grpc", &plugins.Status{State: plugins.StateErr, Message: "Failed to reload server certificate: " + err.Error()})
			return nil
		}
		keyPEMBlock, err := os.ReadFile(s.certKeyFilename)
		if err != nil {
			s.logger.Error("Failed to reload server certificate key: %s.", err.Error())
			s.manager.UpdatePluginStatus("grpc", &plugins.Status{State: plugins.StateErr, Message: "Failed to reload server certificate key: " + err.Error()})
			return nil
		}
		cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
		if err != nil {
			s.manager.UpdatePluginStatus("grpc", &plugins.Status{State: plugins.StateErr, Message: "Failed to load public/private key pair: " + err.Error()})
			return nil
		}
		s.certMtx.Lock()
		s.cert = &cert
		s.certMtx.Unlock()
		tlsConfig.GetCertificate = s.getCertificate

		certHash, err := hash(bytes.NewReader(certPEMBlock))
		if err != nil {
			s.logger.Error("Failed to refresh server certificate: %s.", err.Error())
			s.manager.UpdatePluginStatus("grpc", &plugins.Status{State: plugins.StateErr, Message: "Failed to refresh server certificate: " + err.Error()})
			return nil
		}
		s.certFileHash = certHash
		certKeyHash, err := hash(bytes.NewReader(keyPEMBlock))
		if err != nil {
			s.logger.Error("Failed to refresh server certificate: %s.", err.Error())
			s.manager.UpdatePluginStatus("grpc", &plugins.Status{State: plugins.StateErr, Message: "Failed to refresh server certificate: " + err.Error()})
			return nil
		}
		s.certKeyFileHash = certKeyHash

		// Enterprise OPA custom root CA Cert for mTLS use cases.
		// Note(philip): This currently only loads up a custom root CA cert *on
		// startup*. Cert refresh will not pick up changes to this certificate.
		if s.tlsRootCACertFilename != "" {
			pool, err := loadCertPool(s.tlsRootCACertFilename)
			if err != nil {
				s.manager.UpdatePluginStatus("grpc", &plugins.Status{State: plugins.StateErr, Message: "Failed to load root CA certificate: " + s.tlsRootCACertFilename})
				return nil
			}
			s.certPool = pool // TODO(philip): Do we even need this after initial TLS setup?
			tlsConfig.ClientCAs = pool
		}

		if s.authentication == AuthenticationTLS {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}

		if s.minTLSVersion != 0 {
			tlsConfig.MinVersion = s.minTLSVersion
		} else {
			tlsConfig.MinVersion = defaultMinTLSVersion
		}

		return credentials.NewTLS(&tlsConfig)
	}
	return nil
}

// Note(philip): Relies on (factory).Validate being called upstream.
func getAuthenticationScheme(k string) AuthenticationScheme {
	switch k {
	case "token":
		return AuthenticationToken
	case "tls":
		return AuthenticationTLS
	case "off":
		return AuthenticationOff
	default:
		return AuthenticationOff
	}
}

// Note(philip): Relies on (factory).Validate being called upstream.
func getAuthorizationScheme(k string) AuthorizationScheme {
	switch k {
	case "basic":
		return AuthorizationBasic
	case "off":
		return AuthorizationOff
	default:
		return AuthorizationOff
	}
}

// Note(philip): Relies on (factory).Validate being called upstream.
func getMinTLSVersion(k string) uint16 {
	switch k {
	case "1.0":
		return tls.VersionTLS10
	case "1.1":
		return tls.VersionTLS11
	case "1.2":
		return tls.VersionTLS12
	case "1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12
	}
}

// Borrowed functions from open-policy-agent/opa/server/server.go:

// WithAuthentication sets authentication scheme to use on the server.
func (s *Server) WithAuthentication(scheme AuthenticationScheme) *Server {
	s.authentication = scheme
	return s
}

// WithAuthorization sets authorization scheme to use on the server.
func (s *Server) WithAuthorization(scheme AuthorizationScheme) *Server {
	s.authorization = scheme
	return s
}

// WithCertificate sets the server-side certificate that the server will use.
func (s *Server) WithCertificate(cert *tls.Certificate) *Server {
	s.cert = cert
	return s
}

// WithCertificatePaths sets the server-side certificate and key-file paths
// that the server will periodically check for changes, and reload if necessary.
func (s *Server) WithCertificatePaths(certFilename, keyFilename string, refresh time.Duration) *Server {
	s.certFilename = certFilename
	s.certKeyFilename = keyFilename
	s.certRefreshInterval = refresh
	return s
}

// WithCertPool sets the server-side cert pool that the server will use.
func (s *Server) WithCertPool(pool *x509.CertPool) *Server {
	s.certPool = pool
	return s
}

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

func (s *Server) WithMinTLSVersion(minTLSVersion uint16) *Server {
	if isMinTLSVersionSupported(minTLSVersion) {
		s.minTLSVersion = minTLSVersion
	} else {
		s.minTLSVersion = defaultMinTLSVersion
	}
	return s
}

func isMinTLSVersionSupported(TLSVersion uint16) bool {
	for _, version := range supportedTLSVersions {
		if TLSVersion == version {
			return true
		}
	}
	return false
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
	s.mtx.RLock() // Note(philip): cache could be replaced out from under us unless we read-lock access first.
	defer s.mtx.RUnlock()
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

func (s *Server) makeRego(_ context.Context,
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

func loadCertPool(tlsCACertFile string) (*x509.CertPool, error) {
	caCertPEM, err := os.ReadFile(tlsCACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert file: %s", err.Error())
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caCertPEM); !ok {
		return nil, fmt.Errorf("failed to parse CA cert %q", tlsCACertFile)
	}
	return pool, nil
}
