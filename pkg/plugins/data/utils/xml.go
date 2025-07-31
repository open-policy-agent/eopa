// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"fmt"
	"io"

	"github.com/sbabiv/xml2map"
)

// ParseXML decodes a given xml stream to go representation
func ParseXML(r io.Reader) (ret any, rerr error) {
	defer func() {
		if r := recover(); r != nil {
			rerr = errors.Join(rerr, fmt.Errorf("xml decoder: %v", r))
		}
	}()
	decoder := xml2map.NewDecoder(r)
	data, err := decoder.Decode()
	if err != nil {
		return nil, fmt.Errorf("xml decoder: %w", err)
	}

	return data, nil
}
