package benthos

import (
	"slices"

	"github.com/open-policy-agent/opa/storage"
)

type Config struct {
	Input any // "benthos input"

	// TODO(sr): TLS
	Path              string `json:"path"`
	RegoTransformRule string `json:"rego_transform"`

	path storage.Path
}

func (c Config) Equal(other Config) bool {
	switch {
	case c.RegoTransformRule != other.RegoTransformRule:
	default: // check Input configs
		p1, ok1 := c.Input.(PulsarConfig)
		p2, ok2 := other.Input.(PulsarConfig)
		return ok1 && ok2 && p1.Equal(p2)
	}
	return false
}

type PulsarConfig struct {
	URL                         string   `json:"url"`
	Topics                      []string `json:"topics"`
	SubscriptionName            string   `json:"subscription_name"`
	SubscriptionType            string   `json:"subscription_type"`
	SubscriptionInitialPosition string   `json:"subscription_initial_position"`

	Auth *PulsarAuth `json:"auth,omitempty"`
}

type PulsarAuth struct {
	OAuth2 PulsarOAuth2 `json:"oauth2,omitempty"`
	Token  PulsarToken  `json:"token,omitempty"`
}

type PulsarOAuth2 struct {
	Enabled        bool   `json:"enabled"`
	Audience       string `json:"audience"`
	IssuerURL      string `json:"issuer_url"`
	Scope          string `json:"scope"`
	PrivateKeyFile string `json:"private_key_file"`
}

type PulsarToken struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
}

type authConfig struct {
	Token        *string `json:"auth_token"`
	IssuerURL    *string `json:"issuer_url"`
	Audience     string  `json:"audience"`
	ClientID     string  `json:"client_id"`
	ClientSecret string  `json:"client_secret"`
	Scope        string  `json:"scope"`
}

func (c PulsarConfig) Equal(other PulsarConfig) bool {
	switch {
	case c.URL != other.URL:
	case c.SubscriptionName != other.SubscriptionName:
	case c.SubscriptionType != other.SubscriptionType:
	case c.SubscriptionInitialPosition != other.SubscriptionInitialPosition:
	case len(c.Topics) != len(other.Topics):
	case c.differentTopics(other.Topics):
	case c.Auth != nil && other.Auth == nil:
	case c.Auth == nil && other.Auth != nil:
	default:
		return c.Auth.Equal(other.Auth)
	}
	return false
}

func (c PulsarConfig) differentTopics(others []string) bool {
	slices.Sort(c.Topics)
	slices.Sort(others)
	return !slices.Equal(c.Topics, others)
}

func (c *PulsarAuth) Equal(other *PulsarAuth) bool {
	switch {
	case c.OAuth2.IssuerURL != other.OAuth2.IssuerURL:
	case c.OAuth2.Audience != other.OAuth2.Audience:
	case c.OAuth2.Scope != other.OAuth2.Scope:
	case c.OAuth2.PrivateKeyFile != other.OAuth2.PrivateKeyFile:
	default:
		return true
	}
	return false
}
