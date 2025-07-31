// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"context"

	"github.com/open-policy-agent/opa/v1/storage"
)

type Triggerer interface {
	Trigger(context.Context, storage.Transaction) error
}
