package kafka

import (
	"crypto/tls"

	"github.com/twmb/franz-go/pkg/sasl"
)

// Config represents the configuration of the kafka data plugin
type Config struct {
	BrokerURLs []string `json:"brokerURLs"` // TODO(sr): should come from "services" config
	Topics     []string `json:"topics"`
	Path       string   `json:"path"`

	RegoTransformRule string `json:"rego_transform"`

	Cert       string `json:"tls_client_cert,omitempty"`
	CACert     string `json:"tls_ca_cert,omitempty"`
	PrivateKey string `json:"tls_client_private_key,omitempty"`
	// PrivateKeyPassphrase string `json:"private_key_passphrase,omitempty"` // TODO?

	SASLMechanism string `json:"sasl_mechanism,omitempty"`
	SASLUsername  string `json:"sasl_username,omitempty"`
	SASLPassword  string `json:"sasl_password,omitempty"`
	SASLToken     bool   `json:"sasl_token,omitempty"` // optional for mechanism=scram, "Delegation Tokens" in Confluent docs

	// inserted through Validate()
	tls  *tls.Config
	sasl sasl.Mechanism
}
