package git

import (
	"fmt"
	"os"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/util"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/utils"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config interface{}) plugins.Plugin {
	c := config.(Config)
	return &Data{
		Config:  c,
		log:     m.Logger(),
		exit:    make(chan struct{}),
		manager: m,
		Rego:    transform.New(m, c.Path, Name, c.RegoTransformRule),
	}
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if c.URL == "" {
		return nil, fmt.Errorf("url required")
	}
	e, err := transport.NewEndpoint(c.URL)
	if err != nil {
		return nil, err
	}
	c.url = e.String()

	switch {
	case c.Username != "":
		c.auth = &http.BasicAuth{
			Username: c.Username,
			Password: c.Password,
		}
	case c.Token != "":
		c.auth = &http.BasicAuth{
			Username: gitUser,
			Password: c.Token,
		}
	case c.PrivateKey != "":
		data, err := os.ReadFile(c.PrivateKey)
		if err != nil {
			data = []byte(c.PrivateKey)
		}
		c.auth, err = ssh.NewPublicKeys(gitUser, data, c.Passphrase)
		if err != nil {
			return nil, err
		}
	}
	c.commit = plumbing.NewHash(c.Commit)

	if c.path, err = utils.AddDataPrefixAndParsePath(c.Path); err != nil {
		return nil, err
	}
	if c.interval, err = utils.ParseInterval(c.Interval, 5*time.Minute, utils.DefaultMinInterval); err != nil {
		return nil, err
	}
	if r := c.RegoTransformRule; r != "" {
		if err := transform.Validate(r); err != nil {
			return nil, err
		}
	}

	return c, nil
}
