package s3_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/testcontainers/testcontainers-go"
	tc_minio "github.com/testcontainers/testcontainers-go/modules/minio"

	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

const minioRootUser, minioRootPassword = "minioadmin", "minioadmin"
const bucket, file = "data", "data.json"

func TestDataTransform(t *testing.T) {
	ctx := context.Background()
	plaintextConfig := `
plugins:
  data:
    s3data:
      type: s3
      url: s3://%[2]s/%[5]s
      endpoint: http://%[1]s
      force_path: true # for minio
      access_id: %[3]s
      secret: %[4]s
      rego_transform: data.e2e.transform
`

	transform := `
package e2e
transform[k] := v if {
	some k, v in input.incoming
	is_array(v)
}
`
	tx, endpoint := minioContainer(ctx, t)
	t.Cleanup(func() { tx.Terminate(ctx) })
	putData(ctx, t, endpoint, map[string]any{
		"foo": []bool{true, true, false},
		"bar": true,
	})

	store := storeWithPolicy(ctx, t, transform)
	config := fmt.Sprintf(plaintextConfig, endpoint, bucket, minioRootUser, minioRootPassword, file)
	mgr := pluginMgr(t, store, config)
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	waitForStorePath(ctx, t, store, "/s3data")
	act := must(storage.ReadOne(ctx, store, storage.MustParsePath("/s3data")))(t)

	exp := map[string]any{
		"foo": []any{true, true, false},
	}
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("data value mismatch, diff:\n%s", diff)
	}

}

func minioContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	t.Helper()
	tc, err := tc_minio.Run(ctx, "minio/minio:latest", testcontainers.CustomizeRequestOption(func(r *testcontainers.GenericContainerRequest) error {
		r.ContainerRequest.Env["MINIO_ROOT_USER"] = minioRootUser
		r.ContainerRequest.Env["MINIO_ROOT_PASSWORD"] = minioRootPassword
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	cs, err := tc.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return tc, cs
}

func storeWithPolicy(ctx context.Context, t *testing.T, transform string) storage.Store {
	t.Helper()
	store := inmem.New()
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "e2e.rego", []byte(transform))
	}); err != nil {
		t.Fatalf("store transform policy: %v", err)
	}
	return store
}

func putData(ctx context.Context, t *testing.T, endpoint string, data any) {
	t.Helper()
	bs := must(json.Marshal(data))(t)
	cl := must(minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(minioRootPassword, minioRootPassword, "")}))(t)
	if err := cl.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	must(cl.PutObject(ctx, bucket, file, bytes.NewReader(bs), int64(len(bs)), minio.PutObjectOptions{ContentType: "application/octet-stream"}))(t)
}

func must[T any](x T, err error) func(t *testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
}
