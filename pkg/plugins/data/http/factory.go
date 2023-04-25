package http

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/load-private/pkg/plugins/data/utils"
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
	}
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if len(c.URL) == 0 {
		return nil, fmt.Errorf("url required")
	}
	if c.url, err = url.Parse(c.URL); err != nil {
		return nil, err
	}

	if c.tls, err = utils.ReadTLSConfig(c.SkipVerification, c.Cert, c.PrivateKey, c.CACert); err != nil {
		return nil, err
	}

	c.method = c.Method
	if c.method == "" {
		c.method = http.MethodGet
	}

	if len(c.Headers) > 0 {
		c.headers = make(http.Header, len(c.Headers))
		for key, value := range c.Headers {
			switch v := value.(type) {
			case string:
				c.headers.Set(key, v)
			case []string:
				for _, h := range v {
					c.headers.Add(key, h)
				}
			case []any:
				for _, h := range v {
					if s, ok := h.(string); ok {
						c.headers.Add(key, s)
					} else {
						return nil, fmt.Errorf("unsupported type %T of one of the values of the %q header, expected string", value, key)
					}
				}
			default:
				return nil, fmt.Errorf("unsupported type %T of the %q header, expected string or []string", value, key)
			}
		}
	}

	if c.Body != "" {
		c.body = []byte(c.Body)
	} else if c.File != "" {
		if c.body, err = os.ReadFile(c.File); err != nil {
			return nil, err
		}
	}

	if c.path, err = utils.AddDataPrefixAndParsePath(c.Path); err != nil {
		return nil, err
	}

	if c.interval, err = utils.ParseInterval(c.Interval, utils.DefaultInterval); err != nil {
		return nil, err
	}

	if c.timeout, err = utils.ParseDuration(c.Timeout, 0); err != nil {
		return nil, err
	}

	return c, nil
}
