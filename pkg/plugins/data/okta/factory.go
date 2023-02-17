package okta

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/okta/okta-sdk-golang/v3/okta"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
	"gopkg.in/square/go-jose.v2"

	"github.com/styrainc/load-private/pkg"
	"github.com/styrainc/load-private/pkg/plugins/data/utils"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config interface{}) plugins.Plugin {

	m.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})

	c := config.(Config)
	return &Data{
		Config:  c,
		log:     m.Logger(),
		exit:    make(chan struct{}),
		manager: m,
	}
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if c.URL == "" {
		return nil, fmt.Errorf("tenant url required")
	}
	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	c.config = append(c.config, okta.WithOrgUrl(u.String()))

	switch {
	case c.ClientSecret != "":
		c.config = append(
			c.config,
			okta.WithAuthorizationMode("Bearer"),
			okta.WithClientId(c.ClientID),
			okta.WithToken("dummy"), // will be recreated later
		)
	case c.Token != "":
		c.config = append(
			c.config,
			okta.WithAuthorizationMode("SSWS"),
			okta.WithToken(c.Token),
		)
	case c.PrivateKey != "":
		conf := okta.NewConfiguration(okta.WithPrivateKey(c.PrivateKey))
		priv := []byte(strings.ReplaceAll(conf.Okta.Client.PrivateKey, `\n`, "\n"))
		privPem, _ := pem.Decode(priv)
		if privPem == nil {
			return nil, errors.New("invalid private key")
		}
		var (
			parsedKey any
			err       error
		)
		switch privPem.Type {
		case "RSA PRIVATE KEY":
			parsedKey, err = x509.ParsePKCS1PrivateKey(privPem.Bytes)
		case "PRIVATE KEY":
			parsedKey, err = x509.ParsePKCS8PrivateKey(privPem.Bytes)
		default:
			err = fmt.Errorf("RSA private key is of the wrong type: %s", privPem.Type)
		}
		if err != nil {
			return nil, err
		}

		var signerOptions *jose.SignerOptions
		if c.PrivateKeyID != "" {
			signerOptions = (&jose.SignerOptions{}).WithHeader("kid", c.PrivateKeyID)
		}

		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: parsedKey}, signerOptions)
		if err != nil {
			return "", err
		}
		c.config = append(
			c.config,
			okta.WithAuthorizationMode("PrivateKey"),
			okta.WithClientId(c.ClientID),
			okta.WithPrivateKeySigner(signer),
		)
	}

	if !c.Users && !c.Groups && !c.Roles && !c.Apps {
		return nil, fmt.Errorf("at least on of the resources should be selected: users, groups, roles or apps")
	}
	if c.Users {
		c.scopes = append(c.scopes, "okta.users.read")
	}
	if c.Groups {
		c.scopes = append(c.scopes, "okta.groups.read")
	}
	if c.Roles {
		c.scopes = append(c.scopes, "okta.roles.read")
	}
	if c.Apps {
		c.scopes = append(c.scopes, "okta.apps.read")
	}
	c.config = append(c.config, okta.WithScopes(c.scopes))

	if c.path, err = utils.AddDataPrefixAndParsePath(c.Path); err != nil {
		return nil, err
	}
	if c.interval, err = utils.ParseInterval(c.Interval, 5*time.Minute); err != nil {
		return nil, err
	}

	c.config = append(c.config, okta.WithUserAgentExtra(pkg.GetUserAgent()), okta.WithCache(false))

	return c, nil
}
