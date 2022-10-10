package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	inmem "github.com/StyraInc/load/pkg/store"

	"github.com/StyraInc/load/pkg/plugins/discovery"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/open-policy-agent/opa/runtime"
	"github.com/open-policy-agent/opa/server"
)

// Run provides the CLI entrypoint for the `run` subcommand
func Run(opa *cobra.Command) *cobra.Command {
	// Only override Run, so we keep the args and usage texts
	opa.Run = run
	return opa
}

func run(c *cobra.Command, args []string) {
	ctx := context.Background()
	params, err := newRunParams(c)
	if err != nil {
		panic(err)
	}

	rt, err := initRuntime(ctx, params, args)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	startRuntime(ctx, rt, true)
}

type runCmdParams struct {
	rt                 runtime.Params
	tlsCertFile        string
	tlsPrivateKeyFile  string
	tlsCACertFile      string
	tlsCertRefresh     time.Duration
	ignore             []string
	serverMode         bool
	skipVersionCheck   bool
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
	// NOTE(sr): We're iterating all the command line paramaters that `opa run` accepts,
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
	for _, f := range []struct {
		flag  string
		param *bool
	}{
		{"server", &p.serverMode},
		{"h2c", &p.rt.H2CEnabled},
		{"watch", &p.rt.Watch},
		{"pprof", &p.rt.PprofEnabled},
		{"bundle", &p.rt.BundleMode},
		{"skip-version-check", &p.skipVersionCheck},
		{"skip-verify", &p.skipBundleVerify},
	} {
		*f.param, err = c.Flags().GetBool(f.flag)
		if err != nil {
			return nil, err
		}
	}

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
	s, err := c.Flags().GetStringSlice("addr")
	if err != nil {
		return nil, err
	}
	p.rt.Addrs = &s
	d, err := c.Flags().GetStringSlice("diagnostic-addr")
	if err != nil {
		return nil, err
	}
	p.rt.DiagnosticAddrs = &d

	return &p, nil
}

// initRuntime is taken from OPA's cmd/run.go
func initRuntime(ctx context.Context, params *runCmdParams, args []string) (*runtime.Runtime, error) {
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

	params.rt.EnableVersionCheck = !params.skipVersionCheck

	params.rt.SkipBundleVerification = params.skipBundleVerify

	params.rt.Store = inmem.New()

	params.rt.SkipPluginRegistration = true

	rt, err := runtime.NewRuntime(ctx, params.rt)
	if err != nil {
		return nil, err
	}

	rt.SetDistributedTracingLogging()

	// register the discovery plugin
	disco, err := discovery.New(rt.Manager, discovery.Metrics(rt.Metrics()))
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}

	rt.Manager.Register(discovery.Name, disco)

	return rt, nil
}

func startRuntime(ctx context.Context, rt *runtime.Runtime, serverMode bool) {
	if serverMode {
		rt.StartServer(ctx)
	} else {
		rt.StartREPL(ctx)
	}
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
	caCertPEM, err := ioutil.ReadFile(tlsCACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert file: %v", err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caCertPEM); !ok {
		return nil, fmt.Errorf("failed to parse CA cert %q", tlsCACertFile)
	}
	return pool, nil
}
