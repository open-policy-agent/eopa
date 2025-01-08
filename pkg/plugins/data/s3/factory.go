package s3

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/util"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/utils"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config any) plugins.Plugin {
	c := config.(Config)
	return &Data{
		Config:  c,
		log:     m.Logger(),
		exit:    make(chan struct{}),
		manager: m,
		Rego:    transform.New(m, c.Path, Name, c.RegoTransformRule),
	}
}

func (factory) Validate(_ *plugins.Manager, config []byte) (any, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if c.AccessID == "" {
		return nil, fmt.Errorf("access_id required")
	}
	if c.Secret == "" {
		return nil, fmt.Errorf("secret required")
	}
	if c.URL == "" {
		return nil, fmt.Errorf("url required")
	}

	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	var scheme string
	switch u.Scheme {
	case "":
		scheme = AWSScheme
		parts := strings.SplitN(u.Path, "/", 2)
		c.bucket = parts[0]
		if len(parts) == 2 {
			c.filepath = parts[1]
		}
	case AWSScheme, GCSScheme:
		scheme = u.Scheme
		c.bucket = u.Host
		c.filepath = strings.TrimPrefix(u.Path, "/")
	default:
		return nil, fmt.Errorf("unsupported bucket's schema %q in url %q, shoule be one of: %s", u.Scheme, c.URL, strings.Join([]string{AWSScheme, GCSScheme}, ","))
	}

	c.region = c.Region
	if c.region == "" {
		c.region = DefaultRegions[scheme]
	}
	if c.Endpoint == "" {
		c.endpoint = DefaultEndpoints[scheme]
	} else {
		u, err := url.Parse(c.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("incorrect endpoint %q: %w", c.Endpoint, err)
		}
		c.endpoint = u.String()
	}

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
