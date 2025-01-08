package preview

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/open-policy-agent/opa/v1/storage"

	"github.com/styrainc/enterprise-opa-private/pkg/json"
	eopaStorage "github.com/styrainc/enterprise-opa-private/pkg/storage"
	storageErrors "github.com/styrainc/enterprise-opa-private/pkg/storage/errors"
)

// PreviewStorage sits over top of two possible store.Store instances: a
// primary store and a preview store. Preview Storage is compatible with the
// store.Store interface. If data is available in the preview store, that data
// is preferred. If the data is not present, data from the primary store is
// returned instead. If either store is nil, they are skipped.
//
// The PreviewStorage struct also supplies extra policies so they are compiled
// prior to the query and available for the preview request.
type PreviewStorage struct {
	primaryStore storage.Store
	previewStore storage.Store
	xid          uint64
}

// NewPreviewStorage create an empty PreviewStorage struct.
func NewPreviewStorage() *PreviewStorage {
	return &PreviewStorage{}
}

// WithPrimaryStore set the store.Store instance to use as the fallback store
// when data is not present in the Preview store. Once added, this store is
// read only.
func (s *PreviewStorage) WithPrimaryStorage(primaryStorage storage.Store) *PreviewStorage {
	s.primaryStore = primaryStorage
	return s
}

// WithPreviewData takes arbitrary JSON data and creates a new store.Store from
// the JSON object. This is used as the primary data source when data is requested.
func (s *PreviewStorage) WithPreviewData(previewData json.Json) *PreviewStorage {
	if previewData != nil {
		s.previewStore = eopaStorage.NewFromObject(previewData)
	}
	return s
}

// Register is unsupported in the preview store and always returns and error
func (*PreviewStorage) Register(context.Context, storage.Transaction, storage.TriggerConfig) (storage.TriggerHandle, error) {
	return nil, errors.New("registering is not supported by preview storage as data is read only")
}

// ListPolicies is unsupported in the preview store and returns nil (no policies available)
func (*PreviewStorage) ListPolicies(context.Context, storage.Transaction) ([]string, error) {
	return nil, nil
}

// GetPolicy is unsupported in the preview store and always returns an error.
func (*PreviewStorage) GetPolicy(context.Context, storage.Transaction, string) ([]byte, error) {
	return nil, errors.New("getting polices is not supported by preview storage")
}

// UpsertPolicy is unsupported in the preview store and always returns an error.
func (*PreviewStorage) UpsertPolicy(context.Context, storage.Transaction, string, []byte) error {
	return errors.New("upserting policies is not supported by preview storage")
}

// DeletePolicy is unsupported in the preview store and always returns an error.
func (*PreviewStorage) DeletePolicy(context.Context, storage.Transaction, string) error {
	return errors.New("deleting a policy is not supported by preview storage")
}

// NewTransaction generates a new PreviewTransaction, which abstracts over transactions
// in both the primary and preview stores when defined.
func (s *PreviewStorage) NewTransaction(ctx context.Context, params ...storage.TransactionParams) (storage.Transaction, error) {
	var err error
	var primary storage.Transaction
	var preview storage.Transaction

	// Write params are filtered out to ensure the primary store does not get write locked.
	filteredParams := make([]storage.TransactionParams, 0, len(params))
	for _, param := range params {
		if !param.Write {
			filteredParams = append(filteredParams, param)
		}
	}

	if s.primaryStore != nil {
		primary, err = s.primaryStore.NewTransaction(ctx, filteredParams...)
		if err != nil {
			return nil, err
		}
	}
	if s.previewStore != nil {
		preview, err = s.previewStore.NewTransaction(ctx, filteredParams...)
		if err != nil {
			return nil, err
		}
	}

	xid := atomic.AddUint64(&s.xid, uint64(1))

	return &PreviewTransaction{
		primaryTransaction: primary,
		previewTransaction: preview,
		store:              s,
		xid:                xid,
	}, nil
}

// Read will first attempt to read data from the preview store, if defined, and if it is
// not defined or the value is not present, the primary store is used.
func (s *PreviewStorage) Read(ctx context.Context, txn storage.Transaction, path storage.Path) (interface{}, error) {
	if previewTxn, ok := txn.(*PreviewTransaction); ok {
		if s.previewStore != nil {
			v, err := s.previewStore.Read(ctx, previewTxn.previewTransaction, path)
			if err == nil && v != nil {
				return v, nil
			}
			if err != nil && !storage.IsNotFound(err) {
				return nil, err
			}
		}
		if s.primaryStore != nil {
			return s.primaryStore.Read(ctx, previewTxn.primaryTransaction, path)
		}
	}

	return nil, storageErrors.NewNotFoundError(path)
}

// Write is not supported by the preview store and always returns an error.
func (s *PreviewStorage) Write(context.Context, storage.Transaction, storage.PatchOp, storage.Path, interface{}) error {
	return errors.New("write is not supported by preview storage")
}

// Commit will call Abort to close the storage transaction. This ensures the
// transactions are closed and no mutation is taking place from the preview store.
func (s *PreviewStorage) Commit(ctx context.Context, txn storage.Transaction) error {
	s.Abort(ctx, txn)
	return nil
}

// Truncate will proxy a truncate call to the primary store if defined, otherwise it
// is a noop.
func (s *PreviewStorage) Truncate(ctx context.Context, txn storage.Transaction, params storage.TransactionParams, iter storage.Iterator) error {
	if previewTxn, ok := txn.(*PreviewTransaction); ok {
		if s.primaryStore != nil {
			return s.primaryStore.Truncate(ctx, previewTxn.primaryTransaction, params, iter)
		}
	}

	return nil
}

// Abort will abort the transactions on both the preview and primary stores.
func (s *PreviewStorage) Abort(ctx context.Context, txn storage.Transaction) {
	if previewTxn, ok := txn.(*PreviewTransaction); ok {
		if s.previewStore != nil {
			s.previewStore.Abort(ctx, previewTxn.previewTransaction)
		}
		if s.primaryStore != nil {
			s.primaryStore.Abort(ctx, previewTxn.primaryTransaction)
		}
	}
}
