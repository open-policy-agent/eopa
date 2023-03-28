package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/open-policy-agent/opa/plugins"
)

const PluginName = "grpc"

// Note(philip): Not much to configure yet, but once we add aTLS options
// and metrics, things will get a bit more exciting.
type Config struct {
	Addr string `json:"addr"`
}

type grpcServerPlugin struct {
	manager          *plugins.Manager
	mtx              sync.Mutex
	config           Config
	server           *Server
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
	go func() {
		p.server.Serve(listener)
		p.shutdownComplete <- struct{}{}
	}()

	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (p *grpcServerPlugin) Stop(context.Context) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateNotReady})

	// Initiate a graceful stop, then wait for completion.
	p.server.GracefulStop()
	<-p.shutdownComplete
}

func (p *grpcServerPlugin) Reconfigure(ctx context.Context, config interface{}) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	newConf := config.(Config)
	oldAddr := p.config.Addr

	// Early-exit if no change.
	if oldAddr == newConf.Addr {
		return
	}

	// Reinitialize the gRPC server's state, then restart it.
	p.Stop(ctx)
	p.server = New(p.manager)
	p.config = newConf
	p.Start(ctx)
}
