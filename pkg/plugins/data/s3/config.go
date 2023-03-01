package s3

import (
	"time"

	"github.com/open-policy-agent/opa/storage"
)

const (
	AWSScheme = "s3"
	GCSScheme = "gs"
)

var (
	DefaultRegions = map[string]string{
		AWSScheme: "us-east-1",
		GCSScheme: "auto",
	}
	DefaultEndpoints = map[string]string{
		AWSScheme: "",
		GCSScheme: "https://storage.googleapis.com",
	}
)

// Config represents the configuration of the s3 data plugin
type Config struct {
	URL      string `json:"url"`
	Region   string `json:"region,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	AccessID string `json:"access_id"`
	Secret   string `json:"secret"`

	Interval string `json:"polling_interval,omitempty"` // default 5m, min 10s
	Path     string `json:"path"`

	// inserted through Validate()
	bucket   string
	filepath string
	region   string
	endpoint string
	path     storage.Path
	interval time.Duration
}

func (c Config) Equal(other Config) bool {
	switch {
	case c.AccessID != other.AccessID:
	case c.Secret != other.Secret:
	case c.bucket != other.bucket:
	case c.filepath != other.filepath:
	case c.region != other.region:
	case c.endpoint != other.endpoint:
	case c.Interval != other.Interval:
	default:
		return true
	}
	return false
}
