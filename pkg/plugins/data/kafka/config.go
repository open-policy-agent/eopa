package kafka

// Config represents the configuration of the kafka data plugin
type Config struct {
	BrokerURLs []string `json:"brokerURLs"` // TODO(sr): should come from "services" config
	Topics     []string `json:"topics"`
	Path       string   `json:"path"`

	RegoTransformRule string `json:"rego_transform"`
	// TODO(sr): TLS
	// TODO(sr): Data transformations
}
