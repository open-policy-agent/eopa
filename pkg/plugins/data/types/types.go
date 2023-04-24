package types

import (
	"context"

	"github.com/open-policy-agent/opa/storage"
)

type Triggerer interface {
	Trigger(context.Context, storage.Transaction) error
}
