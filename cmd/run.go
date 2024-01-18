//go:build use_opa_fork

package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"

	bundleApi "github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/hooks"
	"github.com/open-policy-agent/opa/keys"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/open-policy-agent/opa/runtime"
	"github.com/open-policy-agent/opa/server"

	opa_envoy "github.com/open-policy-agent/opa-envoy-plugin/plugin"

	"github.com/styrainc/enterprise-opa-private/internal/license"
	"github.com/styrainc/enterprise-opa-private/pkg/builtins"
	"github.com/styrainc/enterprise-opa-private/pkg/ekm"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/bundle"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	dl "github.com/styrainc/enterprise-opa-private/pkg/plugins/decision_logs"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/grpc"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/impact"
	"github.com/styrainc/enterprise-opa-private/pkg/preview"
	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
	"github.com/styrainc/enterprise-opa-private/pkg/storage"
	"github.com/styrainc/enterprise-opa-private/pkg/telemetry"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

// default bind address if --addr (-a) was not provided in CLI args
const defaultBindAddress = "localhost:8181"

// Run provides the CLI entrypoint for the `run` subcommand
func initRun(opa *cobra.Command, brand string, license license.Checker, lparams *license.LicenseParams) *cobra.Command {
	fallback := opa.RunE
	// Only override Run, so we keep the args and usage texts
	opa.RunE = func(c *cobra.Command, args []string) error {
		c.SilenceErrors = true
		c.SilenceUsage = true

		ctx := context.Background()
		params, err := newRunParams(c)
		if err != nil {
			panic(err)
		}
		params.rt.Brand = brand
		logger, err := getLogger(params.rt.Logging.Level, params.rt.Logging.Format, params.rt.Logging.TimestampFormat)
		if err != nil {
			return err
		}

		strict, _ := c.Flags().GetBool("no-license-fallback")
		license.SetStrict(strict)
		if !strict { // validate license synchronously
			if err := license.ValidateLicense(lparams); err != nil { // TODO(sr): context? timeout?
				logger.Warn(err.Error())
				logger.Warn("Switching to OPA mode. Enterprise OPA functionality will be disabled.")

				c.SilenceErrors = false
				return fallback(c, args)
			}
		}
		builtins.Init()
		rego_vm.SetDefault(true)
		rt, err := initRuntime(ctx, params, args, license, lparams)
		if err != nil {
			fmt.Println("run error:", err)
			return err
		}
		return startRuntime(ctx, rt, params.serverMode)
	}
	return opa
}

type runCmdParams struct {
	rt                 runtime.Params
	tlsCertFile        string
	tlsPrivateKeyFile  string
	tlsCACertFile      string
	tlsCertRefresh     time.Duration
	ignore             []string
	serverMode         bool
	authentication     *pflag.Flag
	authorization      *pflag.Flag
	minTLSVersion      *pflag.Flag
	logLevel           *pflag.Flag
	logFormat          *pflag.Flag
	logTimestampFormat string
	algorithm          string
	scope              string
	pubKey             string
	pubKeyID           string
	skipBundleVerify   bool
	excludeVerifyFiles []string
}

func newRunParams(c *cobra.Command) (*runCmdParams, error) {
	// NOTE(sr): We're iterating all the command line parameters that `opa run` accepts,
	// read them out into the runCmdParams struct, to later drive the runtime initialization.
	// This seems somewhat redundant and annoying, but it seems like a good compromise:
	// The parameters in the OPA run command are private, so we cannot access them. However,
	// we can simulate it to some extent: We don't have to declare the flags and vars again
	// but look up what they have been set to. That way, if a default changes in OPA, we'll
	// get the change carried over automatically.
	// New run flags, however, need to be added here, too. But since we're highjacking "run",
	// that seems unavoidable.
	var err error
	p := runCmdParams{
		rt: runtime.NewParams(),
		// enum flags get special handling, like in OPA's cmd/run.go
		authentication: c.Flag("authentication"),
		authorization:  c.Flag("authorization"),
		minTLSVersion:  c.Flag("min-tls-version"),
		logLevel:       c.Flag("log-level"),
		logFormat:      c.Flag("log-format"),
	}

	// Shovel over string parameters into runtime parameters
	for _, f := range []struct {
		flag  string
		param *string
	}{
		{"config-file", &p.rt.ConfigFile},
		{"history", &p.rt.HistoryPath},
		{"format", &p.rt.OutputFormat},
		{"log-timestamp-format", &p.logTimestampFormat},

		// TLS
		{"tls-cert-file", &p.tlsCertFile},
		{"tls-private-key-file", &p.tlsPrivateKeyFile},
		{"tls-ca-cert-file", &p.tlsCACertFile},

		// bundle verification
		{"verification-key", &p.pubKey},
		{"verification-key-id", &p.pubKeyID},
		{"signing-alg", &p.algorithm},
		{"scope", &p.scope},
	} {
		// Strings flags that don't match any of OPA's parameters will cause an error.
		*f.param, err = c.Flags().GetString(f.flag)
		if err != nil {
			return nil, err
		}
	}

	// bools
	// NOTE(sr): there's two ways to disable telemetry, check both, combine them
	var skipVersionCheck, disableTelemetry bool
	for _, f := range []struct {
		flag  string
		param *bool
	}{
		{"server", &p.serverMode},
		{"h2c", &p.rt.H2CEnabled},
		{"watch", &p.rt.Watch},
		{"pprof", &p.rt.PprofEnabled},
		{"bundle", &p.rt.BundleMode},
		{"skip-version-check", &skipVersionCheck},
		{"disable-telemetry", &disableTelemetry},
		{"skip-verify", &p.skipBundleVerify},
	} {
		*f.param, err = c.Flags().GetBool(f.flag)
		if err != nil {
			return nil, err
		}
	}
	p.rt.EnableVersionCheck = !(disableTelemetry || skipVersionCheck) // either one of these disables

	// ints
	for _, f := range []struct {
		flag  string
		param *int
	}{
		{"max-errors", &p.rt.ErrorLimit},
		{"ready-timeout", &p.rt.ReadyTimeout},
		{"shutdown-grace-period", &p.rt.GracefulShutdownPeriod},
		{"shutdown-wait-period", &p.rt.ShutdownWaitPeriod},
	} {
		*f.param, err = c.Flags().GetInt(f.flag)
		if err != nil {
			return nil, err
		}
	}

	// string slices
	for _, f := range []struct {
		flag  string
		param *[]string
	}{
		{"ignore", &p.ignore},
		{"exclude-files-verify", &p.excludeVerifyFiles},
	} {
		s, err := c.Flags().GetStringSlice(f.flag)
		if err != nil {
			return nil, err
		}
		*f.param = s
	}

	// string arrays
	for _, f := range []struct {
		flag  string
		param *[]string
	}{
		{"set", &p.rt.ConfigOverrides},
		{"set-file", &p.rt.ConfigOverrideFiles},
	} {
		s, err := c.Flags().GetStringArray(f.flag)
		if err != nil {
			return nil, err
		}
		*f.param = s
	}

	// misc
	p.tlsCertRefresh, err = c.Flags().GetDuration("tls-cert-refresh-period")
	if err != nil {
		return nil, err
	}

	// NOTE(sr): We can't wrap these into the stringslice loop above because p.rt.Addrs and
	// p.rt.DiagnosticAddrs are pointers to slices

	// NOTE: Enterprise OPA has a different default: if the --addr parameter was NOT provided,
	// we'll default to localhost:8181 instead of 0.0.0.0:8181 (OPA).
	if c.Flags().Lookup("addr").Changed {
		s, err := c.Flags().GetStringSlice("addr")
		if err != nil {
			return nil, err
		}
		p.rt.Addrs = &s
	} else {
		p.rt.Addrs = &[]string{defaultBindAddress}
	}

	d, err := c.Flags().GetStringSlice("diagnostic-addr")
	if err != nil {
		return nil, err
	}
	p.rt.DiagnosticAddrs = &d
	p.rt.ConfigOverrides = append(p.rt.ConfigOverrides, "labels.type=enterprise-opa")

	return &p, nil
}

// initRuntime is taken from OPA's cmd/run.go
func initRuntime(ctx context.Context, params *runCmdParams, args []string, lic license.Checker, lparams *license.LicenseParams) (*runtime.Runtime, error) {
	authenticationSchemes := map[string]server.AuthenticationScheme{
		"token": server.AuthenticationToken,
		"tls":   server.AuthenticationTLS,
		"off":   server.AuthenticationOff,
	}

	authorizationScheme := map[string]server.AuthorizationScheme{
		"basic": server.AuthorizationBasic,
		"off":   server.AuthorizationOff,
	}

	minTLSVersions := map[string]uint16{
		"1.0": tls.VersionTLS10,
		"1.1": tls.VersionTLS11,
		"1.2": tls.VersionTLS12,
		"1.3": tls.VersionTLS13,
	}

	cert, err := loadCertificate(params.tlsCertFile, params.tlsPrivateKeyFile)
	if err != nil {
		return nil, err
	}

	params.rt.CertificateFile = params.tlsCertFile
	params.rt.CertificateKeyFile = params.tlsPrivateKeyFile
	params.rt.CertificateRefresh = params.tlsCertRefresh

	if params.tlsCACertFile != "" {
		pool, err := loadCertPool(params.tlsCACertFile)
		if err != nil {
			return nil, err
		}
		params.rt.CertPool = pool
	}

	params.rt.Authentication = authenticationSchemes[params.authentication.Value.String()]
	params.rt.Authorization = authorizationScheme[params.authorization.Value.String()]
	params.rt.MinTLSVersion = minTLSVersions[params.minTLSVersion.Value.String()]
	params.rt.Certificate = cert

	timestampFormat := params.logTimestampFormat
	if timestampFormat == "" {
		timestampFormat = os.Getenv("OPA_LOG_TIMESTAMP_FORMAT")
	}
	params.rt.Logging = runtime.LoggingConfig{
		Level:           params.logLevel.Value.String(),
		Format:          params.logFormat.Value.String(),
		TimestampFormat: timestampFormat,
	}
	params.rt.Paths = args
	params.rt.Filter = loaderFilter{
		Ignore: params.ignore,
	}.Apply

	params.rt.SkipBundleVerification = params.skipBundleVerify

	bvc, err := buildVerificationConfig(params.pubKey, params.pubKeyID, params.algorithm, params.scope, params.excludeVerifyFiles)
	if err != nil {
		return nil, err
	}
	params.rt.BundleVerificationConfig = bvc

	params.rt.StoreBuilder = storage.New2

	params.rt.SkipPluginRegistration = true

	params.rt.BundleLazyLoadingMode = params.rt.BundleMode

	params.rt.NDBCacheEnabled = true // We need this for LIA.

	params.rt.AddrSetByUser = true // silence the warning: Enterprise OPA's default isn't unsafe

	a := &bundle.CustomActivator{}
	bundleApi.RegisterActivator("_enterprise_opa", a)
	params.rt.BundleActivatorPlugin = "_enterprise_opa"

	ekmHook := ekm.NewEKM(lic, lparams)
	previewHook := preview.NewHook()
	evalCacheHook := vm.NewCacheHook()
	hs := hooks.New(ekmHook, previewHook, evalCacheHook)
	params.rt.Hooks = hs

	params.rt.Router = loadRouter()

	logger, err := getLogger(params.rt.Logging.Level, params.rt.Logging.Format, params.rt.Logging.TimestampFormat)
	if err != nil {
		return nil, err
	}
	params.rt.Logger = logger
	lic.SetLogger(logger)
	ekmHook.SetLogger(logger)

	params.rt.TelemetryGatherers = map[string]func(context.Context) (any, error){
		"license": func(context.Context) (any, error) {
			return lic.ID(), nil
		},
		"bundle_sizes": telemetry.GatherBundleSizes,
	}
	runtime.RegisterGatherers(params.rt.TelemetryGatherers)

	rt, err := runtime.NewRuntime(ctx, params.rt)
	if err != nil {
		return nil, err
	}

	plugins.InitBundles(nil)(rt.Manager) // To release memory holding the init bundles.

	rt.SetDistributedTracingLogging()

	previewHook.Init(rt.Manager)

	// TODO(sr): This trick won't do for SDK usage. Hence, SDK users will currently
	//           NOT see OTel traces for decision logs or sql.send.
	if tp := rt.Manager.TracerProvider(); tp != nil {
		otel.SetTracerProvider(tp)
	}

	// register the discovery plugin
	disco, err := discovery.New(rt.Manager,
		discovery.Metrics(rt.Metrics()),
		discovery.Factories(map[string]plugins.Factory{
			data.Name:            data.Factory(),
			impact.Name:          impact.Factory(),
			grpc.PluginName:      grpc.Factory(),
			dl.DLPluginName:      dl.Factory(),
			opa_envoy.PluginName: &opa_envoy.Factory{}, // Hack(philip): This is ugly, but necessary because upstream lacks the Factory() function.
		}),
		discovery.Hooks(hs),
	)
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}

	rt.Manager.Register(discovery.Name, disco)
	telemetry.Setup(rt.Manager, disco)

	signalTelemetry(rt.Manager)

	return rt, nil
}

func startRuntime(ctx context.Context, rt *runtime.Runtime, serverMode bool) error {
	if serverMode {
		return rt.StartServer(ctx)
	}
	return rt.StartREPL(ctx)
}

func loadCertificate(tlsCertFile, tlsPrivateKeyFile string) (*tls.Certificate, error) {
	if tlsCertFile != "" && tlsPrivateKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tlsCertFile, tlsPrivateKeyFile)
		if err != nil {
			return nil, err
		}
		return &cert, nil
	} else if tlsCertFile != "" || tlsPrivateKeyFile != "" {
		return nil, fmt.Errorf("--tls-cert-file and --tls-private-key-file must be specified together")
	}

	return nil, nil
}

func loadCertPool(tlsCACertFile string) (*x509.CertPool, error) {
	caCertPEM, err := os.ReadFile(tlsCACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert file: %v", err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caCertPEM); !ok {
		return nil, fmt.Errorf("failed to parse CA cert %q", tlsCACertFile)
	}
	return pool, nil
}

func loadRouter() *mux.Router {
	m := mux.NewRouter()
	m.Use(impact.HTTPMiddleware)
	return m
}

func buildVerificationConfig(pubKey, pubKeyID, alg, scope string, excludeFiles []string) (*bundleApi.VerificationConfig, error) {
	if pubKey == "" {
		return nil, nil
	}

	keyConfig, err := keys.NewKeyConfig(pubKey, alg, scope)
	if err != nil {
		return nil, err
	}
	bs, err := bundleApi.NewVerificationConfig(map[string]*keys.Config{pubKeyID: keyConfig}, pubKeyID, scope, excludeFiles), nil
	return bs, err
}
