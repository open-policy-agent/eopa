package s3_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jarcoal/httpmock"
	"go.uber.org/goleak"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/discovery"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

type Responder struct {
	Method    string
	URL       string
	Responder httpmock.Responder
}

func TestS3Data(t *testing.T) {
	for _, tt := range []struct {
		name       string
		config     string
		responders []Responder
		expected   any
	}{
		{
			name: "aws one file",
			config: `
plugins:
  data:
    s3.placeholder:
      type: s3
      url: s3://test-aws-s3-plugin/ds-test.json
      access_id: foo
      secret: bar
`,
			responders: []Responder{
				{
					Method: http.MethodGet,
					URL:    "https://test-aws-s3-plugin.s3.amazonaws.com/?prefix=ds-test.json",
					Responder: func(*http.Request) (*http.Response, error) {
						resp := httpmock.NewStringResponse(http.StatusOK, `
<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>test-aws-s3-plugin</Name>
  <Prefix>ds-test.json</Prefix>
  <Marker></Marker>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>ds-test.json</Key>
    <LastModified>2023-02-24T18:47:19.000Z</LastModified>
    <ETag>&quot;768ae5a7531a99b361010afae6870599&quot;</ETag>
    <Size>16</Size>
    <Owner>
	  <ID>8775d31c6f84c7378cb08964a629796b119f605f67b9ca22310834adf5b2ec2f</ID>
	  <DisplayName>platform+env-sergey-728919d27e</DisplayName>
    </Owner>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>
`)
						resp.Header.Set("Content-Type", "application/xml")
						resp.Header.Set("Date", "Tue, 28 Feb 2023 23:45:32 GMT")
						resp.Header.Set("Server", "AmazonS3")
						resp.Header.Set("X-Amz-Bucket-Region", "us-east-1")
						resp.Header.Set("X-Amz-Id-2", "sruTRxruMPTzBFL8dkWuuQ65J8v76dCJf3nIo1nGYKiAhuE2DHQ+9b2/f4ngg8oqfvgyd5T50rTd23fnMlUOlA==")
						resp.Header.Set("X-Amz-Request-Id", "163YWGA142637C0N")
						return resp, nil
					},
				},
				{
					Method: http.MethodGet,
					URL:    "https://test-aws-s3-plugin.s3.amazonaws.com/ds-test.json",
					Responder: func(*http.Request) (*http.Response, error) {
						resp := httpmock.NewStringResponse(http.StatusPartialContent, "{\"foo1\":\"bar1\"}\n")
						resp.Header.Set("Accept-Ranges", "bytes")
						resp.Header.Set("Content-Length", "16")
						resp.Header.Set("Content-Range", "bytes 0-15/16")
						resp.Header.Set("Content-Type", "application/json")
						resp.Header.Set("Date", "Tue, 28 Feb 2023 23:45:32 GMT")
						resp.Header.Set("Etag", `"768ae5a7531a99b361010afae6870599"`)
						resp.Header.Set("Last-Modified", `Fri, 24 Feb 2023 18:47:19 GMT`)
						resp.Header.Set("Server", "AmazonS3")
						resp.Header.Set("X-Amz-Bucket-Region", "us-east-1")
						resp.Header.Set("X-Amz-Id-2", "5QPh5vTcVs3bXJP9If06gvmeGC+oPDLWmX3Uc3zB2Zc17TCT2Un3kBo7rgf+PaBkQ61ox2TcinaS9OBb3VuK7Q==")
						resp.Header.Set("X-Amz-Request-Id", "163SRGNK6GHAAXR6")
						resp.Header.Set("X-Amz-Server-Side-Encryption", "AES256")
						return resp, nil
					},
				},
			},
			expected: map[string]any{"foo1": "bar1"},
		},
		{
			name: "gcs data folder",
			config: `
plugins:
  data:
    s3.placeholder:
      type: s3
      url: gs://test-datasources/data/
      access_id: foo
      secret: bar
`,
			responders: []Responder{
				{
					Method: http.MethodGet,
					URL:    "https://test-datasources.storage.googleapis.com/?prefix=data%2F",
					Responder: func(*http.Request) (*http.Response, error) {
						resp := httpmock.NewStringResponse(http.StatusOK, `
<?xml version='1.0' encoding='UTF-8'?>
<ListBucketResult xmlns='http://doc.s3.amazonaws.com/2006-03-01'>
    <Name>test-datasources</Name>
    <Prefix>data/</Prefix>
    <Marker></Marker>
    <IsTruncated>false</IsTruncated>
    <Contents>
        <Key>data/ds-test.json</Key>
        <Generation>1677264514124860</Generation>
        <MetaGeneration>1</MetaGeneration>
        <LastModified>2023-02-24T18:48:34.169Z</LastModified>
        <ETag>"768ae5a7531a99b361010afae6870599"</ETag>
        <Size>16</Size>
    </Contents>
    <Contents>
        <Key>data/ds-test.xml</Key>
        <Generation>1677264514443685</Generation>
        <MetaGeneration>1</MetaGeneration>
        <LastModified>2023-02-24T18:48:34.488Z</LastModified>
        <ETag>"73069ca25a7f2dedb970f8e37c91e956"</ETag>
        <Size>18</Size>
    </Contents>
    <Contents>
        <Key>data/ds-test.yaml</Key>
        <Generation>1677264513991387</Generation>
        <MetaGeneration>1</MetaGeneration>
        <LastModified>2023-02-24T18:48:34.035Z</LastModified>
        <ETag>"97f5157d6743d78116364595b6c73b10"</ETag>
        <Size>11</Size>
    </Contents>
    <Contents>
        <Key>data/ds-test.yml</Key>
        <Generation>1677264514072786</Generation>
        <MetaGeneration>1</MetaGeneration>
        <LastModified>2023-02-24T18:48:34.117Z</LastModified>
        <ETag>"a1ad1b3222a42892d43f4f9ef505e6a0"</ETag>
        <Size>11</Size>
    </Contents>
</ListBucketResult>
`)
						resp.Header.Set("Alt-Svc", `h3=":443"; ma=2592000,h3-29=":443"; ma=2592000,h3-Q050=":443"; ma=2592000,h3-Q046=":443"; ma=2592000,h3-Q043=":443"; ma=2592000,quic=":443"; ma=2592000; v="46,43"`)
						resp.Header.Set("Cache-Control", "private, max-age=0")
						resp.Header.Set("Content-Type", "application/xml; charset=UTF-8")
						resp.Header.Set("Date", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Expires", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Server", "UploadServer")
						resp.Header.Set("X-Goog-Metageneration", "2")
						resp.Header.Set("X-Guploader-Uploadid", "ADPycdvwhx3oK5LNm1ioM3CKTSr6NIHs1vDtk0LHa-sYWyVw_HFr-1XINFNr4Hu5et_0xh81TfHDho3pPUv1FgGZvsako_nNiUPq")
						return resp, nil
					},
				},
				{
					Method: http.MethodGet,
					URL:    "https://test-datasources.storage.googleapis.com/data/ds-test.json",
					Responder: func(*http.Request) (*http.Response, error) {
						resp := httpmock.NewStringResponse(http.StatusPartialContent, "{\"foo1\":\"bar1\"}\n")
						resp.Header.Set("Accept-Ranges", "bytes")
						resp.Header.Set("Alt-Svc", `h3=":443"; ma=2592000,h3-29=":443"; ma=2592000,h3-Q050=":443"; ma=2592000,h3-Q046=":443"; ma=2592000,h3-Q043=":443"; ma=2592000,quic=":443"; ma=2592000; v="46,43"`)
						resp.Header.Set("Cache-Control", "private, max-age=0")
						resp.Header.Set("Content-Length", "16")
						resp.Header.Set("Content-Range", "bytes 0-15/16")
						resp.Header.Set("Content-Type", "application/json")
						resp.Header.Set("Date", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Etag", `"768ae5a7531a99b361010afae6870599"`)
						resp.Header.Set("Expires", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Last-Modified", "Fri, 24 Feb 2023 18:48:34 GMT")
						resp.Header.Set("Server", "UploadServer")
						resp.Header.Set("X-Goog-Generation", "1677264514124860")
						resp.Header.Set("X-Goog-Hash", "crc32c=LWY6TQ==")
						resp.Header.Add("X-Goog-Hash", "md5=dorlp1MambNhAQr65ocFmQ==")
						resp.Header.Set("X-Goog-Metageneration", "1")
						resp.Header.Set("X-Goog-Storage-Class", "STANDARD")
						resp.Header.Set("X-Goog-Stored-Content-Encoding", "identity")
						resp.Header.Set("X-Goog-Stored-Content-Length", "16")
						resp.Header.Set("X-Guploader-Uploadid", "ADPycdv6icby8mMeFX6rde71ZBxXVsN3CU6SkJcyil4UNyzKtIZA4EfFzo3UUrlaZf8eDnw3EzV3dWuu_b85RHzZdZAqkPGPTqAN")
						return resp, nil
					},
				},
				{
					Method: http.MethodGet,
					URL:    "https://test-datasources.storage.googleapis.com/data/ds-test.xml",
					Responder: func(*http.Request) (*http.Response, error) {
						resp := httpmock.NewStringResponse(http.StatusPartialContent, "<foo3>bar3</foo3>\n")
						resp.Header.Set("Accept-Ranges", "bytes")
						resp.Header.Set("Alt-Svc", `h3=":443"; ma=2592000,h3-29=":443"; ma=2592000,h3-Q050=":443"; ma=2592000,h3-Q046=":443"; ma=2592000,h3-Q043=":443"; ma=2592000,quic=":443"; ma=2592000; v="46,43"`)
						resp.Header.Set("Cache-Control", "private, max-age=0")
						resp.Header.Set("Content-Length", "18")
						resp.Header.Set("Content-Range", "bytes 0-17/18")
						resp.Header.Set("Content-Type", "text/xml")
						resp.Header.Set("Date", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Etag", `"73069ca25a7f2dedb970f8e37c91e956"`)
						resp.Header.Set("Expires", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Last-Modified", "Fri, 24 Feb 2023 18:48:34 GMT")
						resp.Header.Set("Server", "UploadServer")
						resp.Header.Set("X-Goog-Generation", "1677264514443685")
						resp.Header.Set("X-Goog-Hash", "crc32c=ro96uw==")
						resp.Header.Add("X-Goog-Hash", "md5=cwacolp/Le25cPjjfJHpVg==")
						resp.Header.Set("X-Goog-Metageneration", "1")
						resp.Header.Set("X-Goog-Storage-Class", "STANDARD")
						resp.Header.Set("X-Goog-Stored-Content-Encoding", "identity")
						resp.Header.Set("X-Goog-Stored-Content-Length", "18")
						resp.Header.Set("X-Guploader-Uploadid", "ADPycdvw4ivkR23g-2o0dFyuFhs7E5he33Y9IVkYIi2PyoG8ToU18TEMYrNXxEr6vOtq1Xem66IS9LGW2WGfuRn7DR3xhBAeLBLW")
						return resp, nil
					},
				},
				{
					Method: http.MethodGet,
					URL:    "https://test-datasources.storage.googleapis.com/data/ds-test.yaml",
					Responder: func(*http.Request) (*http.Response, error) {
						resp := httpmock.NewStringResponse(http.StatusPartialContent, "foo3: bar3\n")
						resp.Header.Set("Accept-Ranges", "bytes")
						resp.Header.Set("Alt-Svc", `h3=":443"; ma=2592000,h3-29=":443"; ma=2592000,h3-Q050=":443"; ma=2592000,h3-Q046=":443"; ma=2592000,h3-Q043=":443"; ma=2592000,quic=":443"; ma=2592000; v="46,43"`)
						resp.Header.Set("Cache-Control", "private, max-age=0")
						resp.Header.Set("Content-Length", "11")
						resp.Header.Set("Content-Range", "bytes 0-10/11")
						resp.Header.Set("Content-Type", "application/x-yaml")
						resp.Header.Set("Date", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Etag", `"97f5157d6743d78116364595b6c73b10"`)
						resp.Header.Set("Expires", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Last-Modified", "Fri, 24 Feb 2023 18:48:34 GMT")
						resp.Header.Set("Server", "UploadServer")
						resp.Header.Set("X-Goog-Generation", "1677264513991387")
						resp.Header.Set("X-Goog-Hash", "crc32c=VvbKlA==")
						resp.Header.Add("X-Goog-Hash", "md5=l/UVfWdD14EWNkWVtsc7EA==")
						resp.Header.Set("X-Goog-Metageneration", "1")
						resp.Header.Set("X-Goog-Storage-Class", "STANDARD")
						resp.Header.Set("X-Goog-Stored-Content-Encoding", "identity")
						resp.Header.Set("X-Goog-Stored-Content-Length", "11")
						resp.Header.Set("X-Guploader-Uploadid", "ADPycdv4IJdi-ZPg5Y5P6NsgyDX72sxOK0UPPeyRWz6f-zzU-n3AmYU-QIcuOX6Bga3tSJwerSywKEh3VggqQqxfmuQsKZIIz1fa")
						return resp, nil
					},
				},
				{
					Method: http.MethodGet,
					URL:    "https://test-datasources.storage.googleapis.com/data/ds-test.yml",
					Responder: func(*http.Request) (*http.Response, error) {
						resp := httpmock.NewStringResponse(http.StatusPartialContent, "foo2: bar2\n")
						resp.Header.Set("Accept-Ranges", "bytes")
						resp.Header.Set("Alt-Svc", `h3=":443"; ma=2592000,h3-29=":443"; ma=2592000,h3-Q050=":443"; ma=2592000,h3-Q046=":443"; ma=2592000,h3-Q043=":443"; ma=2592000,quic=":443"; ma=2592000; v="46,43"`)
						resp.Header.Set("Cache-Control", "private, max-age=0")
						resp.Header.Set("Content-Length", "11")
						resp.Header.Set("Content-Range", "bytes 0-10/11")
						resp.Header.Set("Content-Type", "application/x-yaml")
						resp.Header.Set("Date", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Etag", `"a1ad1b3222a42892d43f4f9ef505e6a0"`)
						resp.Header.Set("Expires", "Wed, 01 Mar 2023 18:11:36 GMT")
						resp.Header.Set("Last-Modified", "Fri, 24 Feb 2023 18:48:34 GMT")
						resp.Header.Set("Server", "UploadServer")
						resp.Header.Set("X-Goog-Generation", "1677264514072786")
						resp.Header.Set("X-Goog-Hash", "crc32c=DGgvxA==")
						resp.Header.Add("X-Goog-Hash", "md5=oa0bMiKkKJLUP0+e9QXmoA==")
						resp.Header.Set("X-Goog-Metageneration", "1")
						resp.Header.Set("X-Goog-Storage-Class", "STANDARD")
						resp.Header.Set("X-Goog-Stored-Content-Encoding", "identity")
						resp.Header.Set("X-Goog-Stored-Content-Length", "11")
						resp.Header.Set("X-Guploader-Uploadid", "ADPycdtBnhQ3PASnN2XXAyw42c8lbTBgBsiQZshqn24RjG30DnEKYwgSBc9z_TvChpdLzDzPmf4v7MDx4RMoqz3m8f9jC1UfkFH4")
						return resp, nil
					},
				},
			},
			expected: map[string]any{
				"data": map[string]any{
					"ds-test.json": map[string]any{"foo1": "bar1"},
					"ds-test.xml":  map[string]any{"foo3": "bar3"},
					"ds-test.yaml": map[string]any{"foo3": "bar3"},
					"ds-test.yml":  map[string]any{"foo2": "bar2"},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)

			httpmock.Activate()
			defer httpmock.DeactivateAndReset()
			for _, r := range tt.responders {
				httpmock.RegisterResponder(r.Method, r.URL, r.Responder)
			}

			ctx := context.Background()
			store := inmem.New()
			mgr := pluginMgr(t, store, tt.config)

			if err := mgr.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer mgr.Stop(ctx)

			waitForStorePath(ctx, t, store, "/s3/placeholder")
			act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/s3/placeholder"))
			if err != nil {
				t.Fatalf("read back data: %v", err)
			}
			if diff := cmp.Diff(tt.expected, act); diff != "" {
				t.Errorf("data value mismatch, diff:%s", diff)
			}
		})
	}
}

func TestS3Owned(t *testing.T) {
	config := `
plugins:
  data:         
    s3.placeholder:
      type: s3 
      url: s3://test-aws-s3-plugin/ds-test.json
      access_id: foo
      secret: bar
`
	defer goleak.VerifyNone(t)

	ctx := context.Background()
	store := inmem.New()
	mgr := pluginMgr(t, store, config)

	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	// test owned path
	err := storage.WriteOne(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/s3/placeholder"), map[string]any{"foo": "bar"})
	if err == nil || err.Error() != `path "/s3/placeholder" is owned by plugin "s3"` {
		t.Errorf("owned check failed, got %v", err)
	}
}

func pluginMgr(t *testing.T, store storage.Store, config string) *plugins.Manager {
	t.Helper()
	h := topdown.NewPrintHook(os.Stderr)
	opts := []func(*plugins.Manager){
		plugins.PrintHook(h),
		plugins.EnablePrintStatements(true),
	}
	if !testing.Verbose() {
		opts = append(opts, plugins.Logger(logging.NewNoOpLogger()))
		opts = append(opts, plugins.ConsoleLogger(logging.NewNoOpLogger()))
	}

	mgr, err := plugins.New([]byte(config), "test-instance-id", store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	disco, err := discovery.New(mgr,
		discovery.Factories(map[string]plugins.Factory{data.Name: data.Factory()}),
	)
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(discovery.Name, disco)
	return mgr
}

func waitForStorePath(ctx context.Context, t *testing.T, store storage.Store, path string) {
	t.Helper()
	if err := util.WaitFunc(func() bool {
		act, err := storage.ReadOne(ctx, store, storage.MustParsePath(path))
		if err != nil {
			if storage.IsNotFound(err) {
				return false
			}
			t.Fatalf("read back data: %v", err)
		}
		if cmp.Diff(map[string]any{}, act) == "" { // empty obj
			return false
		}
		return true
	}, 200*time.Millisecond, 30*time.Second); err != nil {
		t.Fatalf("wait for store path %v: %v", path, err)
	}
}
