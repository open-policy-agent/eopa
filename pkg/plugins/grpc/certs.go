// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package grpc

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"io"
	"os"

	"github.com/open-policy-agent/opa/v1/plugins"

	ftime "github.com/open-policy-agent/eopa/pkg/plugins/grpc/utils"
)

func (s *Server) getCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.certMtx.RLock()
	defer s.certMtx.RUnlock()
	return s.cert, nil
}

// Loop will contain all the calls from the server that we'll be listening on.
type Loop func(chan struct{}, chan struct{}) error

// This method is intended to work identically to OPA's server/certs.go
// `certLoop()` function, but crucially, this function accepts a stop
// message, allowing for graceful termination.
func (s *Server) certLoop() Loop {
	return func(stopC chan struct{}, shutdownCompleteC chan struct{}) error {
		for {
			timer, cancel := ftime.TimerWithCancel(s.certRefreshInterval)
			select {
			case <-stopC:
				cancel()
				shutdownCompleteC <- struct{}{}
				return nil
			case <-timer.C:
				if s.certFilename == "" || s.certKeyFilename == "" {
					// This condition can happen during misconfigurations,
					// and will be resolved when the goroutine receives a
					// stop message during plugin reconfiguration.
					continue
				}

				// Read file contents off disk just once, use up to twice.
				certPEMBlock, err := os.ReadFile(s.certFilename)
				if err != nil {
					s.manager.Logger().Info("Failed to reload server certificate: %s", err.Error())
					continue
				}
				keyPEMBlock, err := os.ReadFile(s.certKeyFilename)
				if err != nil {
					s.manager.Logger().Info("Failed to reload server certificate key: %s", err.Error())
					continue
				}

				// Compute hashes of each file's contents.
				certHash, err := hash(bytes.NewReader(certPEMBlock))
				if err != nil {
					s.manager.Logger().Info("Failed to refresh server certificate: %s", err.Error())
					continue
				}
				certKeyHash, err := hash(bytes.NewReader(keyPEMBlock))
				if err != nil {
					s.manager.Logger().Info("Failed to refresh server certificate: %s", err.Error())
					continue
				}

				// If there's a difference between hashes, then the
				// certificate was updated, and we need to update the cert
				// the gRPC server is using.
				different := !bytes.Equal(s.certFileHash, certHash) ||
					!bytes.Equal(s.certKeyFileHash, certKeyHash)

				// Update server certificate. It will be automatically
				// picked up on future TLS connections, courtesy of the
				// TLSConfig's getCertificate() parameter.
				if different {
					cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
					if err != nil {
						s.manager.UpdatePluginStatus("grpc", &plugins.Status{State: plugins.StateErr, Message: "Failed to load public/private key pair: " + err.Error()})
						return nil
					}
					s.certMtx.Lock()
					s.cert = &cert
					s.certMtx.Unlock()
					s.manager.Logger().Debug("Refreshed server certificate")
				}
			}
		}
	}
}

func hash(src io.Reader) ([]byte, error) {
	h := sha256.New()
	if _, err := io.Copy(h, src); err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}
