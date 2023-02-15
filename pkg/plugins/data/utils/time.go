package utils

import (
	"time"
)

const DefaultInterval = 30 * time.Second

func ParseInterval(s string, def time.Duration) (time.Duration, error) {
	v, err := ParseDuration(s, def)
	if err != nil {
		return v, err
	}

	if v < 10*time.Second {
		return 10 * time.Second, nil
	}

	return v, nil
}

func ParseDuration(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return def, err
	}
	if v == 0 {
		return def, nil
	}
	return v, nil
}
