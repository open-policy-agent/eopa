package utils

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
)

func ReadTLSConfig(insecureSkipVerify bool, certFile, privKeyFile, caCertPath string) (*tls.Config, error) {
	if !insecureSkipVerify && caCertPath == "" {
		return nil, nil
	}

	if insecureSkipVerify {
		return &tls.Config{InsecureSkipVerify: true}, nil
	}

	t := tls.Config{}

	if certFile != "" && privKeyFile != "" {
		keyPEMBlock, err := os.ReadFile(privKeyFile)
		if err != nil {
			return nil, err
		}
		block, _ := pem.Decode(keyPEMBlock)
		if block == nil {
			return nil, errors.New("PEM data could not be found")
		}

		certPEMBlock, err := os.ReadFile(certFile)
		if err != nil {
			return nil, err
		}

		cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
		if err != nil {
			return nil, err
		}
		t.Certificates = []tls.Certificate{cert}
	}

	if caCertPath != "" {
		caCert, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, err
		}
		t.RootCAs = x509.NewCertPool()
		t.RootCAs.AppendCertsFromPEM(caCert)
	}

	return &t, nil
}
