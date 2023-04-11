package grpc

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"io"
	"os"
	"time"

	ftime "github.com/styrainc/load-private/pkg/plugins/grpc/utils"
	"google.golang.org/grpc/credentials"
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
			expiryDuration := s.certRefreshInterval
			timer, cancel := ftime.TimerWithCancel(time.Duration(expiryDuration) * time.Second)
			select {
			case <-stopC:
				cancel()
				shutdownCompleteC <- struct{}{}
				return nil
			case <-timer.C:
				var creds credentials.TransportCredentials

				if s.certFilename == "" || s.certKeyFilename == "" {
					// This condition can happen during misconfigurations,
					// and will be resolved when the goroutine receives a
					// stop message during plugin reconfiguration.
					continue
				}

				certHash, err := hash(s.certFilename)
				if err != nil {
					s.logger.Info("Failed to refresh server certificate: %s", err.Error())
					continue
				}
				certKeyHash, err := hash(s.certKeyFilename)
				if err != nil {
					s.logger.Info("Failed to refresh server certificate: %s", err.Error())
					continue
				}

				s.certMtx.Lock()

				different := !bytes.Equal(s.certFileHash, certHash) ||
					!bytes.Equal(s.certKeyFileHash, certKeyHash)

				if different { // create new credentials
					if tlsCreds := s.getTLSCredentials(); tlsCreds != nil {
						creds = tlsCreds
					}
					s.logger.Debug("Refreshed server certificate")
				}

				s.certMtx.Unlock()

				if different {
					return s.initGRPCServer(creds)
				}
			}
		}
	}
}

func hash(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}
