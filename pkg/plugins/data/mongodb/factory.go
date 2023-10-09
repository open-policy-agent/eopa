package mongodb

import (
	"encoding/json"
	"errors"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/styrainc/enterprise-opa-private/pkg/builtins"
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
		Rego:    transform.New(m, c.RegoTransformRule),
	}
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{Canonical: false}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}

	if c.URI == "" {
		return nil, errors.New("uri is required")
	}
	if c.Database == "" {
		return nil, errors.New("database is required")
	}
	if c.Collection == "" {
		return nil, errors.New("collection is required")
	}
	if len(c.Keys) < 1 {
		return nil, errors.New("one or more key required")
	}
	if c.Path == "" {
		return nil, errors.New("path is required")
	}

	if c.credentials, err = json.Marshal(c.Auth); err != nil {
		return nil, err
	}

	if c.FindOptions != nil {
		data, err := json.Marshal(builtins.ToSnakeCase(c.FindOptions))
		if err != nil {
			return nil, err
		}

		var findOptions options.FindOptions
		if err := json.Unmarshal(data, &findOptions); err != nil {
			return nil, err
		}

		c.findOptions = &findOptions
	}

	if c.path, err = utils.AddDataPrefixAndParsePath(c.Path); err != nil {
		return nil, err
	}
	if c.interval, err = utils.ParseInterval(c.Interval, utils.DefaultInterval); err != nil {
		return nil, err
	}
	if r := c.RegoTransformRule; r != "" {
		if err := transform.Validate(r); err != nil {
			return nil, err
		}
	}

	return c, nil
}
