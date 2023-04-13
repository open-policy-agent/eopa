package grpc_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	bjson "github.com/styrainc/load-private/pkg/json"
	_ "github.com/styrainc/load-private/pkg/rego_vm" // important! use VM for rego.Eval below
	inmem "github.com/styrainc/load-private/pkg/store"
	loadv1 "github.com/styrainc/load-private/proto/gen/go/load/v1"
)

const (
	caCertPath          = "testdata/tls/ca.pem"
	clientCertPath      = "testdata/tls/client-cert.pem"
	clientKeyPath       = "testdata/tls/client-key.pem"
	serverCertPath      = "testdata/tls/server-cert.pem"
	serverKeyPath       = "testdata/tls/server-key.pem"
	otherCACertPath     = "testdata/tls-other/ca.pem"
	otherClientCertPath = "testdata/tls-other/client-cert.pem"
	otherClientKeyPath  = "testdata/tls-other/client-key.pem"
)

func TestServerTLS(t *testing.T) {
	ctx := context.Background()

	config := fmt.Sprintf(`
    plugins:
      grpc:
        addr: ":9191"
        tls:
          cert_file: %[1]s
          cert_key_file: %[2]s
          cert_refresh_interval: %[3]s
`, serverCertPath, serverKeyPath, "1000s")

	// Create the new store with the dummy data.
	storeDataInput := `{
		"test": {
			"a": 2
		}
	}`

	store := storeWithData(ctx, t, storeDataInput)
	mgr := pluginMgr(ctx, t, store, config)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	waitForStorePath(ctx, t, store, "/test/a")

	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caCertPEM); !ok {
		t.Fatal("failed to parse CA cert")
	}
	creds := credentials.NewClientTLSFromCert(pool, "opa.example.com")
	conn, err := grpc.Dial(":9191", grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := loadv1.NewDataServiceClient(conn)

	if _, err := client.UpdateData(ctx, &loadv1.UpdateDataRequest{Data: &loadv1.DataDocument{Path: "/test/a", Document: structpb.NewNumberValue(5)}}); err != nil {
		t.Fatal(err)
	}
	response, err := client.GetData(ctx, &loadv1.GetDataRequest{Path: "/test/a"})
	if err != nil {
		t.Fatal(err)
	}
	result := response.GetResult()
	dataDoc := result.GetDocument()
	if dataDoc.GetNumberValue() != float64(5) {
		fmt.Println(response)
		t.Fatal("Wrong output.")
	}
}

func TestTLSIsMandatoryWhenEnabled(t *testing.T) {
	ctx := context.Background()

	config := fmt.Sprintf(`
    plugins:
      grpc:
        addr: ":9191"
        tls:
          cert_file: %[1]s
          cert_key_file: %[2]s
          cert_refresh_interval: %[3]s
`, serverCertPath, serverKeyPath, "1000s")

	// Create the new store with the dummy data.
	storeDataInput := `{
		"test": {
			"a": 2
		}
	}`

	store := storeWithData(ctx, t, storeDataInput)
	mgr := pluginMgr(ctx, t, store, config)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	waitForStorePath(ctx, t, store, "/test/a")

	creds := insecure.NewCredentials()
	conn, err := grpc.Dial(":9191", grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := loadv1.NewDataServiceClient(conn)

	if response, err := client.GetData(ctx, &loadv1.GetDataRequest{Path: "/test/a"}); err == nil {
		t.Fatalf("expected error, got: %v", response)
	} else {
		if !strings.Contains(err.Error(), "connection error") {
			t.Fatalf("expected connection error, got: %v", err)
		}
	}
}

// Tests mutual TLS by pulling in a custom root CA, and requiring the server to verify the client.
func TestMutualTLSHappyPath(t *testing.T) {
	ctx := context.Background()

	config := fmt.Sprintf(`
    plugins:
      grpc:
        addr: ":9191"
        authentication: "tls"
        tls:
          cert_file: %[1]s
          cert_key_file: %[2]s
          cert_refresh_interval: %[3]s
          ca_cert_file: %[4]s
`, serverCertPath, serverKeyPath, "1000s", caCertPath)

	// Create the new store with the dummy data.
	storeDataInput := `{
		"test": {
			"a": 2
		}
	}`

	store := storeWithData(ctx, t, storeDataInput)
	mgr := pluginMgr(ctx, t, store, config)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	waitForStorePath(ctx, t, store, "/test/a")

	keyPEMBlock, err := os.ReadFile(clientKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	certPEMBlock, err := os.ReadFile(clientCertPath)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatal(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	tc := tls.Config{
		ServerName:   "opa.example.com", // Manually override server name, so that validation checks out.
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	creds := credentials.NewTLS(&tc)
	conn, err := grpc.Dial(":9191", grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := loadv1.NewDataServiceClient(conn)

	if _, err := client.UpdateData(ctx, &loadv1.UpdateDataRequest{Data: &loadv1.DataDocument{Path: "/test/a", Document: structpb.NewNumberValue(5)}}); err != nil {
		t.Fatal(err)
	}
	response, err := client.GetData(ctx, &loadv1.GetDataRequest{Path: "/test/a"})
	if err != nil {
		t.Fatal(err)
	}
	result := response.GetResult()
	dataDoc := result.GetDocument()
	if dataDoc.GetNumberValue() != float64(5) {
		fmt.Println(dataDoc)
		t.Fatal("Wrong output.")
	}
}

// Server TLS is configured normally. Server requires mTLS authentication, but client connects without a client cert.
func TestMutualTLSFailureClientLacksCert(t *testing.T) {
	ctx := context.Background()

	config := fmt.Sprintf(`
    plugins:
      grpc:
        addr: ":9191"
        authentication: "tls"
        tls:
          cert_file: %[1]s
          cert_key_file: %[2]s
          cert_refresh_interval: %[3]s
          ca_cert_file: %[4]s
`, serverCertPath, serverKeyPath, "1000s", caCertPath)

	// Create the new store with the dummy data.
	storeDataInput := `{
		"test": {
			"a": 2
		}
	}`

	store := storeWithData(ctx, t, storeDataInput)
	mgr := pluginMgr(ctx, t, store, config)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	waitForStorePath(ctx, t, store, "/test/a")

	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(caCertPEM); !ok {
		t.Fatal("failed to parse CA cert")
	}
	creds := credentials.NewClientTLSFromCert(pool, "opa.example.com")
	conn, err := grpc.Dial(":9191", grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := loadv1.NewDataServiceClient(conn)

	if response, err := client.UpdateData(ctx, &loadv1.UpdateDataRequest{Data: &loadv1.DataDocument{Path: "/test/a", Document: structpb.NewNumberValue(5)}}); err == nil {
		t.Fatalf("expected error, got: %v", response)
	} else {
		if !strings.Contains(err.Error(), "tls: bad certificate") && !strings.Contains(err.Error(), "write: broken pipe") {
			t.Fatalf("expected tls error or broken pipe (from early client hangup), got: %v", err)
		}
	}
}

// Server will use the normal root CA cert, client will use a different client cert and root CA cert.
func TestMutualTLSFailureWrongRootCA(t *testing.T) {
	ctx := context.Background()

	config := fmt.Sprintf(`
    plugins:
      grpc:
        addr: ":9191"
        authentication: "tls"
        tls:
          cert_file: %[1]s
          cert_key_file: %[2]s
          cert_refresh_interval: %[3]s
          ca_cert_file: %[4]s
`, serverCertPath, serverKeyPath, "1000s", caCertPath)

	// Create the new store with the dummy data.
	storeDataInput := `{
		"test": {
			"a": 2
		}
	}`

	store := storeWithData(ctx, t, storeDataInput)
	mgr := pluginMgr(ctx, t, store, config)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	waitForStorePath(ctx, t, store, "/test/a")

	keyPEMBlock, err := os.ReadFile(otherClientKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	certPEMBlock, err := os.ReadFile(otherClientCertPath)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	if err != nil {
		t.Fatal(err)
	}
	otherCACert, err := os.ReadFile(otherCACertPath)
	if err != nil {
		t.Fatal(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(otherCACert)
	tc := tls.Config{
		ServerName:   "opa.example.com", // Manually override server name, so that validation checks out.
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	creds := credentials.NewTLS(&tc)
	conn, err := grpc.Dial(":9191", grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := loadv1.NewDataServiceClient(conn)

	if response, err := client.UpdateData(ctx, &loadv1.UpdateDataRequest{Data: &loadv1.DataDocument{Path: "/test/a", Document: structpb.NewNumberValue(5)}}); err == nil {
		t.Fatalf("expected error, got: %v", response)
	} else {
		if !strings.Contains(err.Error(), "x509: certificate signed by unknown authority") {
			t.Fatalf("expected x509 signing error, got: %v", err)
		}
	}
}

func storeWithData(_ context.Context, t *testing.T, data string) storage.Store {
	// OPA uses Go's standard JSON library but assumes that numbers have been
	// decoded as json.Number instead of float64. You MUST decode with UseNumber
	// enabled.
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.UseNumber()

	var dataResult map[string]interface{}
	if err := decoder.Decode(&dataResult); err != nil {
		t.Fatal(err)
	}
	store := inmem.NewFromObject(bjson.MustNew(dataResult).(bjson.Object))
	return store
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
	}, 200*time.Millisecond, 10*time.Second); err != nil {
		t.Fatalf("wait for store path %v: %v", path, err)
	}
}
