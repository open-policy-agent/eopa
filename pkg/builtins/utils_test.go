package builtins

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"

	"github.com/styrainc/enterprise-opa-private/pkg/library"
	"github.com/styrainc/enterprise-opa-private/pkg/rego_vm"
)

func TestPostgresEnvSend(t *testing.T) {
	ctx := context.Background()
	be := startPostgreSQL(t)
	t.Cleanup(be.cleanup)

	if err := library.Init(); err != nil {
		t.Fatal(err)
	}

	// first we dissect the conn string into user, password, host etc
	// and set them as temporary env vars
	u, err := url.Parse(be.conn)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := u.User.Password()
	env := map[string]string{
		"PGHOST":     u.Hostname(),
		"PGPORT":     u.Port(),
		"PGUSER":     u.User.Username(),
		"PGPASSWORD": p,
		"PGDBNAME":   u.Path,
		"PGSSLMODE":  u.Query().Get("sslmode"),
	}

	// now, we'll evaluate a policy that uses our embedded utils
	// for postgres connections built from env vars
	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Runtime(ast.NewTerm(ast.MustInterfaceToValue(map[string]any{"env": env}))),
		rego.Module("example.rego", `
package example
import data.system.eopa.utils.postgres.v1.env as postgres
p := count(postgres.send("SELECT 1 FROM T1 WHERE $1", [input.x]).rows)
`),
		rego.Query(`x := data.example.p`),
		rego.Input(map[string]any{"x": true}),
		rego.StrictBuiltinErrors(true),
	)
	rs, err := r.Eval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	act := rs[0].Bindings["x"]
	exp := json.Number("1")
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
	}
}

func TestPostgresVaultSend(t *testing.T) {
	ctx := context.Background()
	be := startPostgreSQL(t)
	t.Cleanup(be.cleanup)

	if err := library.Init(); err != nil {
		t.Fatal(err)
	}

	// first we dissect the conn string into user, password, host etc
	// and set them as temporary env vars
	u, err := url.Parse(be.conn)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := u.User.Password()
	databag := map[string]string{
		"host":     u.Hostname(),
		"port":     u.Port(),
		"user":     u.User.Username(),
		"password": p,
		"dbname":   u.Path,
		"sslmode":  u.Query().Get("sslmode"),
	}

	tc, addr, token := startVault(t, "secret", "postgres", databag)
	t.Cleanup(func() { tc.Terminate(context.Background()) })

	env := map[string]string{
		"VAULT_ADDRESS": addr,
		"VAULT_TOKEN":   token,
	}

	r := rego.New(
		rego.Target(rego_vm.Target),
		rego.Runtime(ast.NewTerm(ast.MustInterfaceToValue(map[string]any{"env": env}))),
		rego.Module("example.rego", `
package example
import data.system.eopa.utils.postgres.v1.vault as postgres
p := count(postgres.send("SELECT 1 FROM T1 WHERE $1", [input.x]).rows)
`),
		rego.Query(`x := data.example.p`),
		rego.Input(map[string]any{"x": true}),
		rego.StrictBuiltinErrors(true),
	)
	rs, err := r.Eval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	act := rs[0].Bindings["x"]
	exp := json.Number("1")
	if diff := cmp.Diff(exp, act); diff != "" {
		t.Errorf("expected binding 'x': (-want, +got):\n%s", diff)
	}
}
