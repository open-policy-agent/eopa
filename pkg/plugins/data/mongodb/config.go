package mongodb

import (
	"reflect"
	"time"

	"github.com/open-policy-agent/opa/storage"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/exp/slices"
)

type Config struct {
	URI         string                 `json:"uri,omitempty"`
	Auth        Auth                   `json:"auth,omitempty"`
	Database    string                 `json:"database"`
	Collection  string                 `json:"collection"`
	FindOptions map[string]interface{} `json:"find_options"`
	Filter      map[string]interface{} `json:"filter"`
	Keys        []string               `json:"keys"`
	Canonical   bool                   `json:"canonical"`
	Interval    string                 `json:"polling_interval,omitempty"` // default 30s
	Path        string                 `json:"path"`

	RegoTransformRule string `json:"rego_transform"`

	// inserted through Validate()
	credentials []byte
	findOptions *options.FindOptions
	path        storage.Path
	interval    time.Duration
}

// TODO: Consider exporting this from mongodb builtin?
type Auth struct {
	// AuthMechanism defines the mechanism to use for authentication. Supported values include "SCRAM-SHA-256", "SCRAM-SHA-1",
	// "MONGODB-CR", "PLAIN", "GSSAPI", "MONGODB-X509", and "MONGODB-AWS". For more details,
	// https://www.mongodb.com/docs/manual/core/authentication-mechanisms/.
	AuthMechanism string `json:"auth_mechanism"`
	// AuthMechanismProperties can be used to specify additional configuration options for certain mechanisms.
	// See https://www.mongodb.com/docs/manual/reference/connection-string/#mongodb-urioption-urioption.authMechanismProperties
	AuthMechanismProperties map[string]string `json:"auth_mechanism_properties"`
	// AuthSource sets the name of the database to use for authentication.
	// https://www.mongodb.com/docs/manual/reference/connection-string/#mongodb-urioption-urioption.authSource
	AuthSource string `json:"auth_source"`
	// Username is the username for authentication.
	Username string `json:"username"`
	// Password is the password for authentication.
	Password string `json:"password"`
	// PasswordSet is for GSSAPI, this must be true if a password is specified, even if the password is the empty string, and
	// false if no password is specified, indicating that the password should be taken from the context of the running
	// process. For other mechanisms, this field is ignored.
	PasswordSet bool `json:"password_set"`
}

func (c Config) Equal(other Config) bool {
	switch {
	case c.URI != other.URI:
	case !c.Auth.Equal(other.Auth):
	case c.Database != other.Database:
	case c.Collection != other.Collection:
	case !reflect.DeepEqual(c.FindOptions, other.FindOptions):
	case !reflect.DeepEqual(c.Filter, other.Filter):
	case slices.Compare(c.Keys, other.Keys) != 0:
	case c.Canonical == other.Canonical:
	case c.Interval != other.Interval:
		// Path excluded.
	default:
		return true
	}
	return false
}

func (a Auth) Equal(other Auth) bool {
	switch {
	case a.AuthMechanism != other.AuthMechanism:
	case !equalMapStringOfStrings(a.AuthMechanismProperties, other.AuthMechanismProperties):
	case a.AuthSource != other.AuthSource:
	case a.Username != other.Username:
	case a.Password != other.Password:
	case a.PasswordSet != other.PasswordSet:
	default:
		return true
	}

	return false
}

func equalMapStringOfStrings(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v1 := range a {
		if v2, ok := b[k]; !ok || v1 != v2 {
			return false
		}
	}

	return true
}
