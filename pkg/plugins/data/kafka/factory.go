package kafka

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config interface{}) plugins.Plugin {

	m.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})

	c := config.(Config)
	return &Data{
		Config:        c,
		log:           m.Logger(),
		exit:          make(chan struct{}),
		path:          ast.MustParseRef("data." + config.(Config).Path),
		manager:       m,
		transformRule: ast.MustParseRef(c.RegoTransformRule),
	}
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if len(c.BrokerURLs) == 0 {
		return nil, fmt.Errorf("need at least one broker URL")
	}
	if len(c.Topics) == 0 {
		return nil, fmt.Errorf("need at least one topic")
	}
	if c.RegoTransformRule == "" {
		return nil, fmt.Errorf("rego transform rule required")
	}

	// TLS and/or SASL
	if c.Cert != "" && c.PrivateKey != "" { // TLS
		if c.tls, err = readTLSConfig(c.Cert, c.PrivateKey, c.CACert); err != nil {
			return nil, err
		}
	}
	if c.SASLMechanism != "" {
		if c.sasl, err = readSASLConfig(c.SASLMechanism, c.SASLUsername, c.SASLPassword, c.SASLToken); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func readTLSConfig(certFile, privKeyFile, caCertPath string) (*tls.Config, error) {
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
	t := tls.Config{Certificates: []tls.Certificate{cert}}

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

func readSASLConfig(mechanism, username, password string, token bool) (sasl.Mechanism, error) {
	switch strings.ToUpper(mechanism) {
	case "SCRAM-SHA-256":
		return scram.Auth{User: username, Pass: password, IsToken: token}.AsSha256Mechanism(), nil
	case "SCRAM-SHA-512":
		return scram.Auth{User: username, Pass: password, IsToken: token}.AsSha512Mechanism(), nil
	case "PLAIN":
		return plain.Auth{User: username, Pass: password}.AsMechanism(), nil
	}

	return nil, fmt.Errorf("unknown SASL mechanism")
}
