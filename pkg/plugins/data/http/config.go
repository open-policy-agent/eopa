package http

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"github.com/open-policy-agent/opa/v1/storage"
	"golang.org/x/exp/slices"
)

// Config represents the configuration of the http data plugin
type Config struct {
	URL             string         `json:"url"`
	Method          string         `json:"method,omitempty"`
	Body            string         `json:"body,omitempty"`
	File            string         `json:"file,omitempty"`
	Headers         map[string]any `json:"headers,omitempty"`          // to support string or []string
	Timeout         string         `json:"timeout,omitempty"`          // no timeouts by default
	FollowRedirects *bool          `json:"follow_redirects,omitempty"` // true if nt set
	Interval        string         `json:"polling_interval,omitempty"` // default 30s

	Path string `json:"path"`

	RegoTransformRule string `json:"rego_transform"`

	SkipVerification bool   `json:"tls_skip_verification,omitempty"`
	Cert             string `json:"tls_client_cert,omitempty"`
	CACert           string `json:"tls_ca_cert,omitempty"`
	PrivateKey       string `json:"tls_client_private_key,omitempty"`

	// inserted through Validate()
	tls      *tls.Config
	url      *url.URL
	method   string
	headers  http.Header
	body     []byte
	path     storage.Path
	interval time.Duration
	timeout  time.Duration
}

func compareFollowRedirects(v1, v2 *bool) bool {
	// nil is true
	if v1 == nil || (v1 != nil && *v1) { // v1 == true
		return v2 == nil || (v2 != nil && *v2)
	}
	// v1 == false
	return v2 != nil && !*v2
}

func (c Config) Equal(other Config) bool {
	switch {
	case c.URL != other.URL:
	case c.Method != other.Method:
	case c.RegoTransformRule != other.RegoTransformRule:
	case c.Body != other.Body:
	case c.File != other.File:
	case c.Timeout != other.Timeout:
	case !compareFollowRedirects(c.FollowRedirects, other.FollowRedirects):
	case c.Interval != other.Interval:
	case c.SkipVerification != other.SkipVerification:
	case c.Cert != other.Cert:
	case c.PrivateKey != other.PrivateKey:
	case c.CACert != other.CACert:
	case len(c.headers) == len(other.headers):
		// the maps.Equal function cannot be used here because some values can be a slice of strings
		for n, v1 := range c.headers {
			v2, ok := other.headers[n]
			if !ok {
				return false
			}
			if !slices.Equal(v1, v2) {
				return false
			}
		}
		return true
	default:
		return true
	}
	return false
}
