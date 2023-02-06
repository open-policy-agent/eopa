package pkg

import (
	iversion "github.com/styrainc/load-private/pkg/internal/version"
)

// GetUserAgent returns the current UserAgent
func GetUserAgent() string {
	return iversion.UserAgent
}
