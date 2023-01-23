package data

import (
	"github.com/open-policy-agent/opa/plugins"
)

// Config represents the configuration for the data feature.
type Config struct {
	DataPlugins map[string]DataPlugin
}

type DataPlugin struct {
	Factory plugins.Factory
	Config  any
}
