package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
)

const PluginName = "grpc"

type Config struct {
	Addr string `json:"addr"` // bind address for the gRPC server.

	// Authentication is the type of authentication scheme to use.
	Authentication string `json:"authentication,omitempty"`

	// Authorization is the type of authorization scheme to use.
	Authorization string `json:"authorization,omitempty"`

	TLS struct {
		CertFile            string `json:"cert_file,omitempty"`
		CertKeyFile         string `json:"cert_key_file,omitempty"`
		CertRefreshInterval int    `json:"cert_refresh_interval,omitempty"` // duration between cert hash checks
		RootCACertFile      string `json:"ca_cert_file,omitempty"`          // Path to the root CA certificate for verifying clients. If not provided, this defaults to TLS using the host’s root CA set.
		// SystemCARequired    bool   `json:"system_ca_required,omitempty"`    // require system certificate appended with root CA certificate.
		MinVersion string `json:"min_version,omitempty"`
	} `json:"tls,omitempty"`
}

type grpcServerPlugin struct {
	manager          *plugins.Manager
	mtx              sync.Mutex
	config           Config
	server           *Server
	logger           logging.Logger
	shutdownComplete chan struct{} // Signal channel for when GracefulShutdwon completes.
}

func (p *grpcServerPlugin) Start(context.Context) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	// Set up TCP listener, and then launch server goroutine.
	addr := p.config.Addr
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		panic(fmt.Errorf("failed to listen on %s: %w", addr, err))
	}
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

	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (p *grpcServerPlugin) Stop(context.Context) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateNotReady})

	p.logger.Info("Starting graceful shutdown of gRPC server.")

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
		p.Start(ctx)
	}
}

func (p *grpcServerPlugin) GetServer() *Server {
	return p.server
}
