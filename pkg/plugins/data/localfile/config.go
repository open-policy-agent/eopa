// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package localfile

import (
	"time"

	"github.com/open-policy-agent/opa/v1/storage"
)

// Config represents the configuration of the localfile data plugin
type Config struct {
	FilePath                string `json:"file_path"`
	FileType                string `json:"file_type,omitempty"`                 // default "json"
	Timeout                 string `json:"timeout,omitempty"`                   // no timeouts by default
	Interval                string `json:"polling_interval,omitempty"`          // default 30s
	ChangeDetectionStrategy string `json:"change_detection_strategy,omitempty"` // default "hash"

	Path string `json:"path"`

	RegoTransformRule string `json:"rego_transform"`

	// inserted through Validate()
	path     storage.Path
	fileType string
	interval time.Duration
}

func (c Config) Equal(other Config) bool {
	switch {
	case c.FilePath != other.FilePath:
	case c.FileType != other.FileType:
	case c.RegoTransformRule != other.RegoTransformRule:
	case c.Interval != other.Interval:
	default:
		return true
	}
	return false
}
