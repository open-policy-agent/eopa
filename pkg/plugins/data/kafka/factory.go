package kafka

import (
	"fmt"
	"strings"
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/styrainc/load-private/pkg/plugins/data/utils"
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
	if len(c.URLs) == 0 {
		return nil, fmt.Errorf("need at least one broker URL")
	}
	if len(c.Topics) == 0 {
		return nil, fmt.Errorf("need at least one topic")
	}
	if c.RegoTransformRule == "" {
		return nil, fmt.Errorf("rego transform rule required")
	}

	// TLS and/or SASL
	if c.tls, err = utils.ReadTLSConfig(c.SkipVerification, c.Cert, c.PrivateKey, c.CACert); err != nil {
		return nil, err
	}
	if c.SASLMechanism != "" {
		if c.sasl, err = readSASLConfig(c.SASLMechanism, c.SASLUsername, c.SASLPassword, c.SASLToken); err != nil {
			return nil, err
		}
	}
	if c.path, err = utils.AddDataPrefixAndParsePath(c.Path); err != nil {
		return nil, err
	}

	switch c.From {
	case "", "start", "end":
	default:
		duration, err := time.ParseDuration(c.From)
		if err != nil {
			return nil, fmt.Errorf("invalid \"from\" duration %q: %w", c.From, err)
		}
		if duration < 0 {
			return nil, fmt.Errorf("invalid negative \"from\" duration %q", c.From)
		}
	}

	return c, nil
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
