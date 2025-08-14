// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

type customEndpointResolver struct {
	endpoint string // Endpoint value provided from plugin config logic.
}

func newCustomEndpointResolver(endpoint string) *customEndpointResolver {
	return &customEndpointResolver{
		endpoint: endpoint,
	}
}

// Note(philip): This resolver implementation is the workaround needed with the
// AWS SDK v2 to set the endpoint directly like how we were doing in AWS SDK v1.
// If we ever decide to not compute the exact endpoint in our plugin config
// logic, then this implementation might need to change.
func (r *customEndpointResolver) ResolveEndpoint(ctx context.Context, params s3.EndpointParameters) (
	smithyendpoints.Endpoint, error,
) {
	if r.endpoint != "" {
		params.Endpoint = aws.String(r.endpoint)
	}

	return s3.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, params)
}
