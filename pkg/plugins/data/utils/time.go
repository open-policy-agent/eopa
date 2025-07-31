// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"time"
)

const (
	DefaultInterval    = 30 * time.Second
	DefaultMinInterval = 1 * time.Second
)

// ParseInterval parses the given string into a time.Duration.
// minDuration ensures that a misconfiguration will not result in excessive polling.
func ParseInterval(s string, defaultDuration time.Duration, minDuration time.Duration) (time.Duration, error) {
	v, err := ParseDuration(s, defaultDuration)
	if err != nil {
		return v, err
	}

	if v < minDuration {
		return minDuration, nil
	}

	return v, nil
}

func ParseDuration(s string, defaultDuration time.Duration) (time.Duration, error) {
	if s == "" {
		return defaultDuration, nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return defaultDuration, err
	}
	if v == 0 {
		return defaultDuration, nil
	}
	return v, nil
}
