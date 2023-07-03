//go:build e2e

package decisionlogs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/google/go-cmp/cmp"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	tc_wait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

func TestDecisionLogsS3Output(t *testing.T) {
	const minioRootUser, minioRootPassword = "minioadmin", "minioadmin"
	const bucket = "logs"
	ctx := context.Background()
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
      type: memory
    output:
      type: s3
      bucket: %[2]s
      endpoint: http://127.0.0.1:%[1]s
      force_path: true
      region: nevermind
      access_key_id: %[3]s
      access_secret: %[4]s
`
	m, port := minioClient(t, ctx, minioRootUser, minioRootPassword, bucket)
	eopa, _, eopaErr := loadEnterpriseOPA(t, fmt.Sprintf(plaintextConfig, port, bucket, minioRootUser, minioRootPassword), policy, false)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

	for i := 0; i < 2; i++ { // act: send API requests
		req, err := http.NewRequest("POST", "http://localhost:28181/v1/data/test/coin",
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
	os := make(map[string]minio.ObjectInfo, 2)
	for len(os) < 2 {
		for object := range m.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			os[object.Key] = object
		}
	}
	logs := make([]payload, 0, 2)
	for k, o := range os {
		x, err := m.GetObject(ctx, bucket, o.Key, minio.GetObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var pl payload
		if err := json.NewDecoder(x).Decode(&pl); err != nil {
			t.Fatal(err)
		}
		logs = append(logs, pl)

		// check path of object
		ts := pl.Timestamp
		exp := fmt.Sprintf("%d/%02d/%02d/%02d/%s.json", ts.Year(), ts.Month(), ts.Day(), ts.Hour(), pl.DecisionID)
		if act := k; exp != act {
			t.Errorf("object key: expected %s %[1]T, got %s %[2]T", exp, act)
		}
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

func minioClient(t *testing.T, ctx context.Context, rootUser, rootPassword, bucket string) (*minio.Client, string) {
	healthPort, err := nat.NewPort("tcp", "9000")
	if err != nil {
		t.Fatal(err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "minio/minio:latest",
		ExposedPorts: []string{"9000/tcp"},
		WaitingFor:   tc_wait.ForHTTP("/minio/health/live").WithPort(healthPort),
		Env: map[string]string{
			"MINIO_ROOT_USER":     rootUser,
			"MINIO_ROOT_PASSWORD": rootPassword,
		},
		Entrypoint: []string{"sh"},
		Cmd:        []string{"-c", fmt.Sprintf("mkdir -p /data/%s && minio server /data", bucket)},
	}

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Logger:           testcontainers.TestLogger(t),
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	mappedPort, err := c.MappedPort(ctx, "9000/tcp")
	if err != nil {
		t.Fatal(err)
	}
	minioClient, err := minio.New("127.0.0.1:"+mappedPort.Port(), &minio.Options{
		Creds: credentials.NewStaticV4(rootUser, rootPassword, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	return minioClient, mappedPort.Port()
}
