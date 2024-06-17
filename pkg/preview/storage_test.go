package preview

import (
	"context"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/opa/storage"
	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	eopaStorage "github.com/styrainc/enterprise-opa-private/pkg/storage"
	"github.com/styrainc/enterprise-opa-private/pkg/vm"
)

func TestStorageGet(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	transaction := testTransaction(ctx, t, store)

	testCases := []struct {
		key      string
		expected string
	}{
		{
			key:      "key1",
			expected: "primary value 1",
		},
		{
			key:      "key2",
			expected: "preview value 1",
		},
		{
			key:      "key3",
			expected: "preview value 2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.key, func(t *testing.T) {
			v, err := store.Read(ctx, transaction, storage.Path{tc.key})
			if err != nil {
				t.Fatalf("Error reading from store: %v", err)
			}
			if extractString(v) != tc.expected {
				t.Errorf("%s should have returned %q, but %q was received", tc.key, tc.expected, extractString(v))
			}
		})
	}
}

func TestTransactionGet(t *testing.T) {
	ctx := context.Background()
	transaction := iterableTestTransaction(ctx, t, testStore(t))
	assertIterableValue(ctx, t, transaction, "key1", "primary value 1")
	assertIterableValue(ctx, t, transaction, "key2", "preview value 1")
	assertIterableValue(ctx, t, transaction, "key3", "preview value 2")
}

func TestTransactionIter(t *testing.T) {
	type kvPair struct {
		Key   string
		Value string
	}
	ctx := context.Background()
	transaction := iterableTestTransaction(ctx, t, testStore(t))
	expected := []kvPair{
		{Key: "key1", Value: "primary value 1"},
		{Key: "key2", Value: "preview value 1"},
		{Key: "key2", Value: "primary value 2"},
		{Key: "key3", Value: "preview value 2"},
	}
	found := make([]kvPair, 0, 4)
	transaction.Iter(ctx, func(k, v any) (bool, error) {
		found = append(found, kvPair{Key: extractString(k), Value: extractString(v)})
		return false, nil
	})
	sort.Slice(found, func(i, j int) bool {
		return found[i].Key+found[i].Value < found[j].Key+found[j].Value
	})

	if diff := cmp.Diff(found, expected); diff != "" {
		t.Errorf("mismatch in iterated items (-want, +got):\n%s", diff)
	}
}

func TestTransactionIterReturnsEarly(t *testing.T) {
	ctx := context.Background()
	transaction := iterableTestTransaction(ctx, t, testStore(t))
	iterations := 0
	transaction.Iter(ctx, func(_, _ any) (bool, error) {
		iterations++
		return true, nil
	})

	if iterations != 1 {
		t.Errorf("Should have iterated 1 time when returning true, but iterated %d times", iterations)
	}
}

func extractString(val any) string {
	var strVal string
	if s, ok := val.(string); ok {
		strVal = s
	} else if s, ok := val.(*bjson.String); ok && s != nil {
		strVal = s.Value()
	}

	return strVal
}

func assertIterableValue(ctx context.Context, t *testing.T, iterable vm.IterableObject, key string, value string) {
	t.Helper()
	val, present, err := iterable.Get(ctx, key)
	if err != nil {
		t.Fatalf("Error running transaction Get: %v", err)
	}
	if !present {
		t.Errorf("The key %s was not found", "key1")
		return
	}

	strVal := extractString(val)

	if strVal != value {
		t.Errorf("Expected iterable at key %q to contain value %q but it had %q", key, value, strVal)
	}
}

func testStore(t *testing.T) *PreviewStorage {
	t.Helper()
	primaryStore := eopaStorage.NewFromObject(bjson.MustNew(map[string]any{"key1": "primary value 1", "key2": "primary value 2"}))
	previewData := bjson.MustNew(map[string]any{"key2": "preview value 1", "key3": "preview value 2"})
	store := NewPreviewStorage().WithPrimaryStorage(primaryStore).WithPreviewData(previewData)
	return store
}

func testTransaction(ctx context.Context, t *testing.T, store *PreviewStorage) storage.Transaction {
	transaction, err := store.NewTransaction(ctx)
	if err != nil {
		t.Fatalf("Unable to create a storage transaction: %v", err)
	}
	return transaction
}

func iterableTestTransaction(ctx context.Context, t *testing.T, store *PreviewStorage) vm.IterableObject {
	iterable, ok := testTransaction(ctx, t, store).(vm.IterableObject)
	if !ok {
		t.Fatalf("Unable to assert transaction is an IterableObject")
	}
	return iterable
}
