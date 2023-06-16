package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	iCache "github.com/open-policy-agent/opa/topdown/cache"
)

const PluginName = "grpc"

type Config struct {
	MaxRecvMessageSize int          `json:"max_recv_message_size"` // Max size can be up to ~2.1 GB. Default is 4 MB.
	Addr               string       `json:"addr"`                  // bind address for the gRPC server.
	listener           net.Listener // intentionally unexported.

	// Authentication is the type of authentication scheme to use.
	Authentication string `json:"authentication,omitempty"`

	// Authorization is the type of authorization scheme to use.
	Authorization string `json:"authorization,omitempty"`

	TLS struct {
		CertFile            string `json:"cert_file,omitempty"`
		CertKeyFile         string `json:"cert_key_file,omitempty"`
		CertRefreshInterval string `json:"cert_refresh_interval,omitempty"` // duration to wait between cert hash checks.
		RootCACertFile      string `json:"ca_cert_file,omitempty"`          // Path to the root CA certificate for verifying clients. If not provided, this defaults to TLS using the host’s root CA set.
		// SystemCARequired    bool   `json:"system_ca_required,omitempty"`    // require system certificate appended with root CA certificate.
		MinVersion                   string        `json:"min_version,omitempty"`
		validatedCertRefreshDuration time.Duration // intentionally unexported
	} `json:"tls,omitempty"`
}

func (c *Config) SetListener(lis net.Listener) {
	c.listener = lis
}

type grpcServerPlugin struct {
	manager          *plugins.Manager
	mtx              sync.Mutex
	config           Config
	server           *Server
	logger           logging.Logger
	trigger          storage.TriggerHandle
	shutdownComplete chan struct{} // Signal channel for when GracefulShutdwon completes.
}

func (p *grpcServerPlugin) Start(context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	var listener net.Listener

	// Use existing listener if one is already present.
	if p.config.listener != nil {
		listener = p.config.listener
	} else {
		var err error
		// Set up TCP listener if none exists yet.
		addr := p.config.Addr
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			panic(fmt.Errorf("failed to listen on %s: %w", addr, err))
		}
	}
	// Fire up server goroutine.
	p.logger.Info("Starting gRPC server on port: " + p.config.Addr)
	go func() {
		_ = p.server.Serve(listener)
		p.shutdownComplete <- struct{}{}
	}()
	// Launch new certLoop goroutine if we need it.
	if p.server.certLoopHaltChannel != nil {
		go func() {
			loop := p.server.certLoop()
			_ = loop(p.server.certLoopHaltChannel, p.server.certLoopShutdownCompleteChannel)
		}()
	}

	ctx := context.TODO()
	txn, err := p.server.store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		p.logger.Error("failed to setup store transaction: %w", err)
		return err
	}

	// Note(philip): This may be the source of the bug.
	// Register triggers so that if the runtime reloads the policies, the
	// server sees the change.
	config := storage.TriggerConfig{
		OnCommit: func(_ context.Context, _ storage.Transaction, ev storage.TriggerEvent) {
			// Note(philip): Write-lock server instance before replacing the cache.
			p.server.mtx.Lock()
			defer p.server.mtx.Unlock()
			p.server.preparedEvalQueries = newCache(pqMaxCacheSize)
		},
	}
	triggerHandle, err := p.server.store.Register(ctx, txn, config)
	if err != nil {
		p.server.store.Abort(ctx, txn)
		p.logger.Error("failed to register store trigger: %w", err)
		return err
	}
	p.trigger = triggerHandle
	p.server.store.Commit(ctx, txn)

	// Note(philip): Register compiler trigger here so that on store
	// updates, we correctly clear all caches.
	p.manager.RegisterCompilerTrigger(func(storage.Transaction) {
		p.mtx.Lock()
		p.server.preparedEvalQueries = newCache(pqMaxCacheSize)
		p.server.interQueryBuiltinCache = iCache.NewInterQueryCache(p.manager.InterQueryBuiltinCacheConfig())
		p.mtx.Unlock()
	})

	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (p *grpcServerPlugin) Stop(context.Context) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateNotReady})

	p.logger.Info("Starting graceful shutdown of gRPC server.")

	// Unregister the store trigger.
	ctx := context.TODO()
	txn, err := p.manager.Store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		p.logger.Error("failed to setup store transaction: %w", err)
		p.logger.Error("grpc store trigger could not be unregistered")
		return
	}

	p.trigger.Unregister(ctx, txn)
	p.trigger = nil
	p.manager.Store.Commit(ctx, txn)

	// Stop any live certLoop goroutine; it'll be respawned if we need it later.
	if p.server.certLoopHaltChannel != nil {
		p.server.certLoopHaltChannel <- struct{}{}
		<-p.server.certLoopShutdownCompleteChannel
	}

	// Initiate a graceful stop, then wait for completion.
	p.server.GracefulStop()
	<-p.shutdownComplete
	p.logger.Info("Done with graceful shutdown of gRPC server.")
}

func (p *grpcServerPlugin) Reconfigure(ctx context.Context, config interface{}) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	newConf := config.(Config)

	// Reinitialize the gRPC server's state, then restart it.
	p.Stop(ctx)
	p.config = newConf
	p.server = New(p.manager, newConf)

	// Log any warnings/errors. Only restart on successful plugin states.
	pluginState := p.manager.PluginStatus()["grpc"]
	switch pluginState.State {
	case plugins.StateErr:
		p.logger.Error(pluginState.Message)
	case plugins.StateWarn:
		p.logger.Warn(pluginState.Message)
	default:
		p.logger.Info("gRPC server successfully reconfigured.")
		if err := p.Start(ctx); err != nil {
			p.logger.Error(err.Error())
		}
	}
}

func (p *grpcServerPlugin) GetServer() *Server {
	return p.server
}
