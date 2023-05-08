// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package azureauth

import (
	"context"
	"errors"

	"github.com/hashicorp/vault/sdk/logical"
)

func Factory(context.Context, *logical.BackendConfig) (logical.Backend, error) {
	return nil, errors.New("not implemented")
}
