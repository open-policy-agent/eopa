package git

import (
	"net/http"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/open-policy-agent/opa/v1/storage"
)

func init() {
	// Azure DevOps requires capabilities multi_ack / multi_ack_detailed,
	// The basic level is supported, but disabled by default.
	// Enabling multi_ack by removing them from UnsupportedCapabilities
	caps := make([]capability.Capability, 0, len(transport.UnsupportedCapabilities))
	for _, c := range transport.UnsupportedCapabilities {
		if c == capability.MultiACK || c == capability.MultiACKDetailed {
			continue
		}
		caps = append(caps, c)
	}
	transport.UnsupportedCapabilities = caps

	c := githttp.NewClient(&http.Client{Transport: newErrorsInterceptor(http.DefaultTransport)})
	// Override http and https protocols to enrich the errors with the response bodies
	client.InstallProtocol("http", c)
	client.InstallProtocol("https", c)
}

// Config represents the configuration of the git data plugin
type Config struct {
	URL      string `json:"url"`
	FilePath string `json:"file_path"`           // if path points to a file, then only that file is loaded, otherwise scans for any json/yaml/xml files recursively, from root if path is empty
	Commit   string `json:"commit,omitempty"`    // if empty then Branch is used
	Branch   string `json:"branch,omitempty"`    // if empty then Ref is used
	Ref      string `json:"reference,omitempty"` // if empty then `refs/heads/main`, if `main` failed, then `refs/heads/master`

	// Basic auth
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// PAT auth
	Token string `json:"token,omitempty"`

	// SSH mode
	PrivateKey string `json:"private_key,omitempty"` // key in PEM format or a file path
	Passphrase string `json:"passphrase,omitempty"`

	Interval string `json:"polling_interval,omitempty"` // default 5m, min 10s
	Path     string `json:"path"`

	RegoTransformRule string `json:"rego_transform"`

	// inserted through Validate()
	url      string
	auth     transport.AuthMethod
	commit   plumbing.Hash
	path     storage.Path
	interval time.Duration
}

func (c Config) Equal(other Config) bool {
	switch {
	case c.URL != other.URL:
	case c.FilePath != other.FilePath:
	case c.Commit != other.Commit:
	case c.Ref != other.Ref:
	case c.Username != other.Username:
	case c.Password != other.Password:
	case c.Token != other.Token:
	case c.PrivateKey != other.PrivateKey:
	case c.Passphrase != other.Passphrase:
	case c.Interval != other.Interval:
	case c.auth.String() != other.auth.String():
	default:
		return true
	}
	return false
}
