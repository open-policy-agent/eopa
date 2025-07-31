// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package data

import (
	"github.com/open-policy-agent/opa/v1/plugins"
)

// Config represents the configuration for the data feature.
type Config struct {
	DataPlugins map[string]DataPlugin
}

type DataPlugin struct {
	Factory plugins.Factory
	Config  any
}
