package benthos

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/utils"
)

type BenthosPlugin string

// benthos-based data plugins
const (
	Pulsar BenthosPlugin = "pulsar"
)

type factory struct {
	Type BenthosPlugin
}

func Factory(t BenthosPlugin) plugins.Factory {
	return &factory{Type: t}
}

func (f factory) New(m *plugins.Manager, config any) plugins.Plugin {
	// NOTE(sr): This handling of multiple different benthos inputs is uninspired.
	// When adding a second one, let's do better than an ever-growing switch.
	switch f.Type {
	case Pulsar:
		c := config.(Config)
		return &Data{
			Config:  c,
			log:     m.Logger(),
			exit:    make(chan struct{}),
			manager: m,
			Rego:    transform.New(m, c.Path, string(Pulsar), c.RegoTransformRule),
		}
	}
	panic("unreachable")
}

func (f factory) Validate(m *plugins.Manager, config []byte) (any, error) {
	switch f.Type {
	case Pulsar:
		c := Config{}
		err := util.Unmarshal(config, &c)
		if err != nil {
			return nil, err
		}
		if c.path, err = utils.AddDataPrefixAndParsePath(c.Path); err != nil {
			return nil, err
		}

		pulsar := PulsarConfig{}
		if err := util.Unmarshal(config, &pulsar); err != nil {
			return nil, err
		}
		switch {
		case len(pulsar.Topics) == 0:
			return nil, fmt.Errorf("requires at least one topic")
		case pulsar.URL == "":
			return nil, fmt.Errorf("requires pulsar URL")
		case pulsar.SubscriptionType != "":
			switch pulsar.SubscriptionType {
			case "exclusive", "failover", "shared", "key_shared": // OK
			default:
				return nil, fmt.Errorf("unknown subscription_type \"%s\"", pulsar.SubscriptionType)
			}
		}

		pulsar.SubscriptionName = or(pulsar.SubscriptionName, fmt.Sprintf("eopa_%s_%s", m.ID, c.Path))
		pulsar.SubscriptionType = or(pulsar.SubscriptionType, "exclusive")                      // benthos default is "shared"
		pulsar.SubscriptionInitialPosition = or(pulsar.SubscriptionInitialPosition, "earliest") // benthos default is "latest"

		var auth authConfig
		if err := util.Unmarshal(config, &auth); err != nil {
			return nil, err
		}
		if auth.Token != nil && auth.IssuerURL != nil {
			return nil, fmt.Errorf("auth_token and oauth2 settings are mutually exclusive")
		}
		switch {
		case auth.Token != nil:
			pulsar.Auth = &PulsarAuth{
				Token: PulsarToken{
					Token:   *auth.Token,
					Enabled: true,
				},
			}
		case auth.IssuerURL != nil:
			pulsar.Auth = &PulsarAuth{
				OAuth2: PulsarOAuth2{
					IssuerURL: *auth.IssuerURL,
					Audience:  auth.Audience,
					PrivateKeyFile: privateKeyFile(map[string]string{
						"client_id":     auth.ClientID,
						"client_secret": auth.ClientSecret,
						"issuer_url":    *auth.IssuerURL,
					}),
					Enabled: true,
					Scope:   auth.Scope,
				},
			}
		}

		c.Input = pulsar

		return c, nil
	}
	return nil, fmt.Errorf("unknown data plugin type \"%s\"", f.Type)
}

// The naming of this in Pulsar is terrible. It's a json file or data blob
// containing auth-related data.
func privateKeyFile(blob map[string]string) string {
	j, _ := json.Marshal(blob) // let encoding/json deal with escaping special chars
	b := base64.StdEncoding.EncodeToString(j)
	return `data:application/json;base64,` + b
}

// NOTE(sr): Replace with cmp.Or when we can require go1.22.
func or(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
