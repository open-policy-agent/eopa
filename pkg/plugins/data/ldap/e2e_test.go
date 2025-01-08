package ldap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	rhttp "github.com/hashicorp/go-retryablehttp"
	"github.com/testcontainers/testcontainers-go"
	tc_wait "github.com/testcontainers/testcontainers-go/wait"

	"github.com/open-policy-agent/opa/v1/storage"

	inmem "github.com/styrainc/enterprise-opa-private/pkg/storage"
)

const (
	rootUser     = "uid=admin,ou=people,dc=example,dc=com"
	rootPassword = "password"
)

var retryClient = rhttp.NewClient().StandardClient()

func TestDataLDAP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	addr, tx := testServer(ctx, t)
	t.Cleanup(func() { tx.Terminate(ctx) })

	config := `
plugins:
  data:
    entities: # arbitrary!
      type: ldap
      urls: [%[3]s]
      base_dn: "dc=example,dc=com"
      filter: "(objectclass=*)"
      username: %[1]s
      password: %[2]s
      polling_interval: 10s
      rego_transform: data.e2e.transform
`

	transform := `package e2e
transform.users[id] := y if {
	some entry in input.incoming
	"inetOrgPerson" in entry.objectclass
	id := entry.uid[0]
	y := {
		"name": entry.cn[0],
	}
}

transform.groups[id] := members if {
	some entry in input.incoming
	"groupOfUniqueNames" in entry.objectclass
	id := entry.cn[0]
	members := member_ids(entry.uniquemember)
	not startswith(id, "lldap_")
}

member_ids(uids) := { id |
	some entry in input.incoming
	"inetOrgPerson" in entry.objectclass
	entry.dn._raw in uids
	id := entry.uid[0]
}
`

	transform1 := `package e2e.transform
game := "over"
`

	store := storeWithPolicy(ctx, t, transform)
	mgr := pluginMgr(t, store, fmt.Sprintf(config, rootUser, rootPassword, addr))
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	{
		waitForStorePath(ctx, t, store, "/entities/users/alice")
		act := must(storage.ReadOne(ctx, store, storage.MustParsePath("/entities")))(t)

		exp := map[string]any{
			"users": map[string]any{
				"admin": map[string]any{"name": "Administrator"},
				"alice": map[string]any{"name": "Alice"},
				"bob":   map[string]any{"name": "Bob"},
			},
			"groups": map[string]any{
				"app-admins":      []any{"alice", "bob"},
				"app-superadmins": []any{"alice"},
			},
		}
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch, diff:\n%s", diff)
		}
	}
	{ // replace transform0 by transform1
		if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
			return store.UpsertPolicy(ctx, txn, "e2e.rego", []byte(transform1))
		}); err != nil {
			t.Fatalf("store transform policy: %v", err)
		}
	}
	{
		waitForStorePath(ctx, t, store, "/entities/game")
		act := must(storage.ReadOne(ctx, store, storage.MustParsePath("/entities/game")))(t)

		exp := "over"
		if diff := cmp.Diff(exp, act); diff != "" {
			t.Errorf("data value mismatch, diff:\n%s", diff)
		}
	}
}

func testServer(ctx context.Context, t *testing.T) (string, testcontainers.Container) {
	req := testcontainers.ContainerRequest{
		Image:        "lldap/lldap:v0.6.0",
		ExposedPorts: []string{"3890/tcp", "17170/tcp"},
		WaitingFor:   tc_wait.ForLog("DB Cleanup Cron started"),
		Env: map[string]string{
			"LLDAP_LDAP_USER_PASS": rootPassword,
		},
	}

	c := must(testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Logger:           testcontainers.TestLogger(t),
		Started:          true,
	}))(t)
	mappedPort := must(c.MappedPort(ctx, "3890/tcp"))(t)
	port := must(c.MappedPort(ctx, "17170/tcp"))(t)
	api := fmt.Sprintf("http://localhost:%s", port.Port())
	tok := token(t, api)
	createUser(t, api, tok, "alice", "alice@example.com", "Alice")
	createUser(t, api, tok, "bob", "bob@example.com", "Bob")
	admins := createGroup(t, api, tok, "app-admins")
	superAdmins := createGroup(t, api, tok, "app-superadmins")

	addUserToGroup(t, api, tok, "alice", admins)
	addUserToGroup(t, api, tok, "bob", admins)
	addUserToGroup(t, api, tok, "alice", superAdmins)
	return fmt.Sprintf("ldap://localhost:%s", mappedPort.Port()), c
}

func token(t *testing.T, api string) string {
	payload := fmt.Sprintf(`{"username": "admin", "password": "%s"}`, rootPassword)
	resp := must(retryClient.Post(api+"/auth/simple/login", "application/json", strings.NewReader(payload)))(t)
	defer resp.Body.Close()
	var m struct {
		Token string
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m.Token
}

func createUser(t *testing.T, api, token, id, email, displayName string) {
	query(t, api, token,
		fmt.Sprintf(`{"query":"mutation{createUser(user:{id:\"%s\",email:\"%s\",displayName:\"%s\"}){id email displayName}}"}`,
			id, email, displayName),
	)
}

func createGroup(t *testing.T, api, token, name string) int {
	data := query(t, api, token,
		fmt.Sprintf(`{"query":"mutation{createGroup(name:\"%s\"){id displayName}}"}`, name),
	)
	return int(data["createGroup"].(map[string]any)["id"].(float64))
}

func addUserToGroup(t *testing.T, api, token, id string, group int) {
	query(t, api, token,
		fmt.Sprintf(`{"variables":{"user":"%s","group":%d},"query":"mutation AddUserToGroup($user: String!, $group: Int!) {\n  addUserToGroup(userId: $user, groupId: $group) {\n    ok\n  }\n}\n","operationName":"AddUserToGroup"}`,
			id, group),
	)
}

func query(t *testing.T, api, token, query string) map[string]any {
	req, _ := http.NewRequest("POST", api+"/api/graphql", strings.NewReader(query))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+token)
	resp := must(retryClient.Do(req))(t)
	defer resp.Body.Close()
	var data struct {
		Data map[string]any
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	t.Logf("query %s => %v", query, data.Data)
	return data.Data
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

func must[T any](x T, err error) func(t *testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
}
