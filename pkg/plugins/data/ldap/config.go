package ldap

import (
	"crypto/tls"
	"net/url"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/open-policy-agent/opa/v1/storage"
	"golang.org/x/exp/slices"
)

var (
	scopeMap = map[string]int{
		"base-object":   ldap.ScopeBaseObject,
		"single-level":  ldap.ScopeSingleLevel,
		"whole-subtree": ldap.ScopeWholeSubtree,
		"":              ldap.ScopeWholeSubtree, // default value
	}
	derefMap = map[string]int{
		"":          ldap.NeverDerefAliases, // default value
		"never":     ldap.NeverDerefAliases,
		"searching": ldap.DerefInSearching,
		"finding":   ldap.DerefFindingBaseObj,
		"always":    ldap.DerefAlways,
	}
)

// Config represents the configuration of the ldap data plugin
type Config struct {
	RegoTransformRule string `json:"rego_transform"`

	URLs     []string `json:"urls"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`

	// Search settings
	BaseDN     string   `json:"base_dn"`
	Filter     string   `json:"filter,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Deref      string   `json:"deref,omitempty"`
	Attributes []string `json:"attributes,omitempty"`

	// TLS config
	SkipVerification bool   `json:"tls_skip_verification,omitempty"`
	Cert             string `json:"tls_client_cert,omitempty"`
	CACert           string `json:"tls_ca_cert,omitempty"`
	PrivateKey       string `json:"tls_client_private_key,omitempty"`

	Interval string `json:"polling_interval,omitempty"` // default 30s
	Path     string `json:"path"`

	// inserted through Validate()
	urls       []*url.URL
	tls        *tls.Config
	path       storage.Path
	interval   time.Duration
	scope      int
	deref      int
	attributes []string
}

func (c Config) Equal(other Config) bool {
	switch {
	case !slices.Equal(c.URLs, other.URLs): // the values are sorted through Validate()
	case c.Username != other.Username:
	case c.Password != other.Password:
	case c.RegoTransformRule != other.RegoTransformRule:
	case c.BaseDN != other.BaseDN:
	case c.Filter != other.Filter:
	case c.scope != other.scope: // compare integers are faster
	case c.deref != other.deref: // ^^^
	case !slices.Equal(c.attributes, other.attributes): // compare the final versions of attributes
	case c.Interval != other.Interval:
	default:
		return true
	}
	return false
}
