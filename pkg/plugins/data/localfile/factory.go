package localfile

import (
	"fmt"
	"path/filepath"

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
	p := &Data{
		Config:  c,
		log:     m.Logger(),
		exit:    make(chan struct{}),
		manager: m,
		Rego:    transform.New(m, c.Path, Name, c.RegoTransformRule),
	}
	return p
}

func (factory) Validate(_ *plugins.Manager, config []byte) (interface{}, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	if len(c.FilePath) == 0 {
		return nil, fmt.Errorf("file_path required")
	}
	if _, err := getFileInfo(c.FilePath); err != nil {
		return nil, fmt.Errorf("invalid file_path %q, (%w)", c.FilePath, err)
	}
	if c.FileType == "" {
		switch filepath.Ext(c.FilePath) {
		case ".xml":
			c.fileType = "xml"
		case ".yaml":
			c.fileType = "yaml"
		default:
			c.fileType = "json"
		}
	} else {
		c.fileType = c.FileType
	}
	if c.ChangeDetectionStrategy == "" {
		c.ChangeDetectionStrategy = "hash"
	}

	if c.path, err = utils.AddDataPrefixAndParsePath(c.Path); err != nil {
		return nil, err
	}
	if c.interval, err = utils.ParseInterval(c.Interval, utils.DefaultInterval, utils.DefaultMinInterval); err != nil {
		return nil, err
	}
	if r := c.RegoTransformRule; r != "" {
		if err := transform.Validate(r); err != nil {
			return nil, err
		}
	}

	return c, nil
}
