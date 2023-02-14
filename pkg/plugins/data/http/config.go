package http

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"github.com/open-policy-agent/opa/storage"
	"golang.org/x/exp/maps"
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

func (c Config) Equal(other Config) bool {
	switch {
	case c.URL != other.URL:
	case c.Method != other.Method:
	case c.Body != other.Body:
	case c.File != other.File:
	case c.Timeout != other.Timeout:
	case c.FollowRedirects != other.FollowRedirects:
	case c.Interval != other.Interval:
	case c.SkipVerification != other.SkipVerification:
	case c.Cert != other.Cert:
	case c.PrivateKey != other.PrivateKey:
	case c.CACert != other.CACert:
	case !maps.Equal(c.Headers, other.Headers):
	default:
		return true
	}
	return false
}
