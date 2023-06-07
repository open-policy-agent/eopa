package pkg

import (
	iversion "github.com/styrainc/enterprise-opa-private/pkg/internal/version"
)

// GetUserAgent returns the current UserAgent
func GetUserAgent() string {
	return iversion.UserAgent
}
