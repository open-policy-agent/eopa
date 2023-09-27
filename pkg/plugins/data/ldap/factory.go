package ldap

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/utils"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config interface{}) plugins.Plugin {
	c := config.(Config)
	d := &Data{
		Config:  c,
		log:     m.Logger(),
		exit:    make(chan struct{}),
		manager: m,
	}
	if c.RegoTransformRule != "" {
		d.Rego = transform.New(m, ast.MustParseRef(c.RegoTransformRule))
	}
	return d
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if len(c.URLs) == 0 {
		return nil, fmt.Errorf("at least one url required")
	}
	slices.Sort(c.URLs)
	for _, u := range c.URLs {
		u, err := url.Parse(u)
		if err != nil {
			return nil, err
		}
		c.urls = append(c.urls, u)

	}
	if c.tls, err = utils.ReadTLSConfig(c.SkipVerification, c.Cert, c.PrivateKey, c.CACert); err != nil {
		return nil, err
	}
	if c.path, err = utils.AddDataPrefixAndParsePath(c.Path); err != nil {
		return nil, err
	}
	if c.interval, err = utils.ParseInterval(c.Interval, utils.DefaultInterval); err != nil {
		return nil, err
	}

	if c.BaseDN == "" {
		return nil, errors.New("base_dn is required")
	}
	if c.Filter == "" {
		return nil, errors.New("filter is required")
	}
	var ok bool
	if c.scope, ok = scopeMap[strings.ToLower(c.Scope)]; !ok {
		return nil, fmt.Errorf("scope must be one of: %+v", maps.Keys(scopeMap))
	}
	if c.deref, ok = derefMap[strings.ToLower(c.Deref)]; !ok {
		return nil, fmt.Errorf("deref must be one of: %+v", maps.Keys(scopeMap))
	}
	// clean up empty attributes
	c.attributes = make([]string, 0, len(c.Attributes))
	for _, v := range c.Attributes {
		v = strings.TrimSpace(v)
		if len(v) > 0 {
			c.attributes = append(c.attributes, v)
		}
	}
	slices.Sort(c.attributes)

	if r := c.RegoTransformRule; r != "" {
		if err := transform.Validate(r); err != nil {
			return nil, err
		}
	}

	return c, nil
}
