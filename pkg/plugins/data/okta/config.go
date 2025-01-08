package okta

import (
	"time"

	"github.com/okta/okta-sdk-golang/v3/okta"
	"github.com/open-policy-agent/opa/v1/storage"
)

// Config represents the configuration of the okta data plugin
type Config struct {
	URL string `json:"url"`

	// Bearer mode
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`

	// SSWS mode (Single Sign-On Web Systems)
	Token string `json:"token,omitempty"`

	// PrivateKey mode
	// ClientID is used as well
	PrivateKey   string `json:"private_key,omitempty"` // key in rsa format or a file path
	PrivateKeyID string `json:"private_key_id,omitempty"`

	// scopes
	Users  bool `json:"users,omitempty"`
	Groups bool `json:"groups,omitempty"`
	Roles  bool `json:"roles,omitempty"`
	Apps   bool `json:"apps,omitempty"`

	Interval string `json:"polling_interval,omitempty"` // default 5m, min 10s
	Path     string `json:"path"`

	RegoTransformRule string `json:"rego_transform"`

	// inserted through Validate()
	path     storage.Path
	interval time.Duration
	config   []okta.ConfigSetter
	scopes   []string
}

func (c Config) Equal(other Config) bool {
	switch {
	case c.URL != other.URL:
	case c.ClientID != other.ClientID:
	case c.ClientSecret != other.ClientSecret:
	case c.RegoTransformRule != other.RegoTransformRule:
	case c.Token != other.Token:
	case c.PrivateKey != other.PrivateKey:
	case c.PrivateKeyID != other.PrivateKeyID:
	case c.Users != other.Users:
	case c.Groups != other.Groups:
	case c.Roles != other.Roles:
	case c.Apps != other.Apps:
	case c.Interval != other.Interval:
	default:
		return true
	}
	return false
}
