//go:generate go run github.com/vektra/mockery/v2@latest --srcpkg github.com/go-ldap/ldap/v3 --name Client --output mocks --disable-version-string --case underscore

package ldap_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"testing"
	"time"

	goldap "github.com/go-ldap/ldap/v3"
	"github.com/google/go-cmp/cmp"
	tmock "github.com/stretchr/testify/mock"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/plugins/discovery"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/ldap"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/ldap/mocks"
	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

var (
	//go:embed testdata/search.json
	searchData []byte
	//go:embed testdata/converted.json
	convertedData []byte
)

func TestLDAPDataMocks(t *testing.T) {
	var (
		searchResult    *goldap.SearchResult
		convertedResult []any
	)
	if err := json.Unmarshal(searchData, &searchResult); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(convertedData, &convertedResult); err != nil {
		t.Fatal(err)
	}

	bindReqWithoutPassword := goldap.NewSimpleBindRequest("test", "", nil)
	bindReqWithoutPassword.AllowEmptyPassword = true

	for _, tt := range []struct {
		name        string
		config      string
		bindRequest *goldap.SimpleBindRequest
	}{
		{
			name: "username with password",
			config: `
plugins:
  data:
    ldap.placeholder:
      type: ldap
      urls: 
        - ldaps://example.com
      base_dn: "dc=example,dc=com"
      filter: "(objectclass=*)"
      username: test
      password: testpswd
`,
			bindRequest: goldap.NewSimpleBindRequest("test", "testpswd", nil),
		},
		{
			name: "username without password",
			config: `
plugins:
  data:
    ldap.placeholder:
      type: ldap
      urls: 
        - ldaps://example.com
      base_dn: "dc=example,dc=com"
      filter: "(objectclass=*)"
      username: test
`,
			bindRequest: bindReqWithoutPassword,
		},
		{
			name: "without credentials",
			config: `
plugins:
  data:
    ldap.placeholder:
      type: ldap
      urls: 
        - ldaps://example.com
      base_dn: "dc=example,dc=com"
      filter: "(objectclass=*)"
`,
			bindRequest: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &mocks.Client{}
			defer client.AssertExpectations(t)

			ctx := ldap.WithClient(context.Background(), client)
			if tt.bindRequest != nil {
				client.On("SimpleBind", tt.bindRequest).Return(&goldap.SimpleBindResult{}, nil)
			}
			client.On("Start")
			client.On("Search", tmock.AnythingOfType("*ldap.SearchRequest")).Return(searchResult, nil)
			client.On("Close").Return(nil)

			store := inmem.New()
			mgr := pluginMgr(t, store, tt.config)

			if err := mgr.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer mgr.Stop(ctx)

			waitForStorePath(ctx, t, store, "/ldap/placeholder")
			act, err := storage.ReadOne(ctx, store, storage.MustParsePath("/ldap/placeholder"))
			if err != nil {
				t.Fatalf("read back data: %v", err)
			}
			if diff := cmp.Diff(convertedResult, act); diff != "" {
				t.Errorf("data value mismatch, diff:\n%s", diff)
			}
		})
	}
}

func TestLDAPOwned(t *testing.T) {
	config := `
plugins:
  data:         
    ldap.placeholder:
      type: ldap        
      urls:                     
        - ldaps://example.com
      base_dn: "dc=example,dc=com"
      filter: "(objectclass=*)"
      username: test
      password: testpswd
`
	client := &mocks.Client{}
	defer client.AssertExpectations(t)

	ctx := ldap.WithClient(context.Background(), client)

	client.On("Start").Maybe()
	client.On("Close").Return(nil).Maybe()
	client.On("SimpleBind", goldap.NewSimpleBindRequest("test", "testpswd", nil)).Return(&goldap.SimpleBindResult{}, nil).Maybe()
	client.On("Search", tmock.AnythingOfType("*ldap.SearchRequest")).Return(nil, nil).Maybe()

	store := inmem.New()
	mgr := pluginMgr(t, store, config)

	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	// test owned path
	err := storage.WriteOne(ctx, mgr.Store, storage.AddOp, storage.MustParsePath("/ldap/placeholder"), map[string]any{"foo": "bar"})
	if err == nil || err.Error() != `path "/ldap/placeholder" is owned by plugin "ldap"` {
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
	}, 200*time.Millisecond, 20*time.Second); err != nil {
		t.Fatalf("wait for store path %v: %v", path, err)
	}
}
