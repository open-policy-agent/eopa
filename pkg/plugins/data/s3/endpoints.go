// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"context"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

type MultiSchemeEndpointResolver struct {
	originalURL string // Store the original gs://, s3://, or localhost URL
}

func NewMultiSchemeEndpointResolver(originalURL string) *MultiSchemeEndpointResolver {
	return &MultiSchemeEndpointResolver{
		originalURL: originalURL,
	}
}

func (r *MultiSchemeEndpointResolver) ResolveEndpoint(ctx context.Context, params s3.EndpointParameters) (
	smithyendpoints.Endpoint, error,
) {
	bucketName := ""
	if params.Bucket != nil {
		bucketName = *params.Bucket
	}

	// Parse the original URL to determine scheme
	parsedURL, err := url.Parse(r.originalURL)
	if err != nil {
		// Fall back to default AWS behavior
		return s3.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, params)
	}

	switch parsedURL.Scheme {
	case "gs":
		// Google Cloud Storage: gs://bucket-name/path -> https://bucket-name.storage.googleapis.com
		endpoint := smithyendpoints.Endpoint{
			URI: url.URL{
				Scheme: "https",
				Host:   bucketName + ".storage.googleapis.com",
			},
		}
		return endpoint, nil

	case "http", "https":
		// localhost or custom HTTP endpoints: http://localhost:9000 or https://custom.endpoint.com
		endpoint := smithyendpoints.Endpoint{
			URI: url.URL{
				Scheme: parsedURL.Scheme,
				Host:   parsedURL.Host,
			},
		}
		return endpoint, nil

	case "s3":
		// Standard S3: s3://bucket-name/path -> use default AWS endpoint resolution
		return s3.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, params)

	default:
		// Unknown scheme - fall back to default AWS behavior
		return s3.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, params)
	}
}

// Choose resolver based on URL scheme
func getEndpointResolver(rawURL string) func(o *s3.Options) {
	parsedURL, _ := url.Parse(rawURL)
	usePathStyle := false

	// Determine if path-style is needed based on URL scheme
	switch parsedURL.Scheme {
	case "gs":
		usePathStyle = false // GCS uses virtual-hosted style
	case "http", "https":
		if strings.Contains(parsedURL.Host, "localhost") {
			usePathStyle = true // localhost/MinIO typically uses path-style
		}
	case "s3":
		usePathStyle = false // AWS S3 default
	}

	return func(o *s3.Options) {
		o.EndpointResolverV2 = NewMultiSchemeEndpointResolver(rawURL)
		o.UsePathStyle = usePathStyle
	}
}
