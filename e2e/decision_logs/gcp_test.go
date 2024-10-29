//go:build e2e

package decisionlogs

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/google/go-cmp/cmp"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

const gcpBucket = "logs"

func TestDecisionLogsGCPCSOutput(t *testing.T) {
	policy := `
package test
import future.keywords

coin if rand.intn("coin", 2)
`

	plaintextConfig := `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    buffer:
      type: unbuffered
    output:
      type: gcp_cloud_storage
      bucket: %[1]s
`

	srv := setupCloudStorage(t)
	t.Cleanup(srv.Stop)

	eopa, _, eopaErr := loadEnterpriseOPA(t, fmt.Sprintf(plaintextConfig, gcpBucket), policy, eopaHTTPPort, false, gcpOverrideEndpoint(srv.URL()))
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	for i := 0; i < 2; i++ { // act: send API requests
		req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/coin", eopaHTTPPort),
			strings.NewReader(fmt.Sprintf(`{"input":%d}`, i)))
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := 200, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
	}

	var dfs func(string) ([]fakestorage.ObjectAttrs, error)
	dfs = func(prefix string) ([]fakestorage.ObjectAttrs, error) {
		attrs, obs, err := srv.ListObjectsWithOptions(gcpBucket, fakestorage.ListOptions{Delimiter: "/", Prefix: prefix})
		if err != nil {
			return nil, err
		}
		os := []fakestorage.ObjectAttrs{}
		for _, p0 := range obs {
			o0, err := dfs(p0)
			if err != nil {
				return nil, err
			}
			os = append(os, o0...)
		}
		return append(attrs, os...), nil
	}

	var objects [][]byte

	var obs []fakestorage.ObjectAttrs
	var err error
	// try a few times: expecting 2 objects in the bucket
	wait.ForResult(t, func() {
		obs, err = dfs("")
	}, func() error {
		clear(objects)
		if err != nil {
			return fmt.Errorf("list objects: %w", err)
		}
		if exp, act := 2, len(obs); exp != act {
			return fmt.Errorf("expected %d logs, but got %d", exp, act)
		}
		cts := map[string]struct{}{}
		for _, o := range obs {
			cts[o.ContentType] = struct{}{}
			o0, err := srv.GetObject(gcpBucket, o.Name)
			if err != nil {
				return fmt.Errorf("get object: %w", err)
			}
			objects = append(objects, o0.Content)
		}
		_, ok := cts["application/json"]
		if !ok || len(cts) != 1 {
			return fmt.Errorf("unexpected content-types: %v", maps.Keys(cts))
		}
		return nil
	}, 5, 200*time.Millisecond)

	logs := make([]payload, 0, 2)
	for _, o := range objects {
		var pl payload
		if err := json.Unmarshal(o, &pl); err != nil {
			t.Fatal("unmarshal object: %w", err)
		}
		logs = append(logs, pl)
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })

	{ // request 1
		dl := payload{
			Result: true,
			ID:     1,
			Input:  float64(0),
			Labels: standardLabels,
		}
		if diff := cmp.Diff(dl, logs[0], stdIgnores); diff != "" {
			t.Errorf("diff: (-want +got):\n%s", diff)
		}
	}
	{ // request 2
		dl := payload{
			Result: true,
			ID:     2,
			Input:  float64(1),
			Labels: standardLabels,
		}
		if diff := cmp.Diff(dl, logs[1], stdIgnores); diff != "" {
			t.Errorf("diff: (-want +got):\n%s", diff)
		}
	}
}

func setupCloudStorage(t *testing.T) *fakestorage.Server {
	s, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		Scheme: "http",
		Host:   "127.0.0.1",
		Port:   0,
		Writer: os.Stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	s.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: gcpBucket})
	return s
}
