package kafka

import (
	"crypto/tls"

	"github.com/open-policy-agent/opa/storage"
	"github.com/twmb/franz-go/pkg/sasl"
	"golang.org/x/exp/slices"
)

// Config represents the configuration of the kafka data plugin
type Config struct {
	URLs   []string `json:"urls"`
	Topics []string `json:"topics"`
	Path   string   `json:"path"`

	RegoTransformRule string `json:"rego_transform"`

	SkipVerification bool   `json:"tls_skip_verification,omitempty"`
	Cert             string `json:"tls_client_cert,omitempty"`
	CACert           string `json:"tls_ca_cert,omitempty"`
	PrivateKey       string `json:"tls_client_private_key,omitempty"`
	// PrivateKeyPassphrase string `json:"private_key_passphrase,omitempty"` // TODO?

	SASLMechanism string `json:"sasl_mechanism,omitempty"`
	SASLUsername  string `json:"sasl_username,omitempty"`
	SASLPassword  string `json:"sasl_password,omitempty"`
	SASLToken     bool   `json:"sasl_token,omitempty"` // optional for mechanism=scram, "Delegation Tokens" in Confluent docs

	// inserted through Validate()
	tls  *tls.Config
	sasl sasl.Mechanism
	path storage.Path
}

func (c Config) Equal(other Config) bool {
	switch {
	case len(c.URLs) != len(other.URLs):
	case len(c.Topics) != len(other.Topics):
	case c.RegoTransformRule != other.RegoTransformRule:
	case c.SkipVerification != other.SkipVerification:
	case c.Cert != other.Cert:
	case c.PrivateKey != other.PrivateKey:
	case c.CACert != other.CACert:
	case c.SASLMechanism != other.SASLMechanism:
	case c.SASLUsername != other.SASLUsername:
	case c.SASLPassword != other.SASLPassword:
	case c.SASLToken != other.SASLToken:
	case c.differentBrokers(other.URLs):
	case c.differentTopics(other.Topics):
	default:
		return true
	}
	return false
}

func (c Config) differentBrokers(others []string) bool {
	return !subset(c.URLs, others) || !subset(others, c.URLs)
}

func (c Config) differentTopics(others []string) bool {
	return !subset(c.Topics, others) || !subset(others, c.Topics)
}

func subset[E comparable](a []E, b []E) bool {
	for _, v := range a {
		if !slices.Contains(b, v) {
			return false
		}
	}
	return true
}
