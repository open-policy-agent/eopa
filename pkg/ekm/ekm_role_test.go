package ekm

import (
	"bytes"
	"strings"
	"testing"

	hclog "github.com/hashicorp/go-hclog"
	kv "github.com/hashicorp/vault-plugin-secrets-kv"
	vault "github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/builtin/credential/approle"
	vaulthttp "github.com/hashicorp/vault/http"
	hclogging "github.com/hashicorp/vault/sdk/helper/logging"
	"github.com/hashicorp/vault/sdk/logical"
	hashivault "github.com/hashicorp/vault/vault"

	"github.com/open-policy-agent/opa/config"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
)

func createVaultTestRoleCluster(t *testing.T) (*hashivault.TestCluster, string, string) {
	t.Helper()

	coreConfig := &hashivault.CoreConfig{
		CredentialBackends: map[string]logical.Factory{
			"approle": approle.Factory,
		},
		LogicalBackends: map[string]logical.Factory{
			"kv": kv.Factory,
		},
	}
	cluster := hashivault.NewTestCluster(t, coreConfig, &hashivault.TestClusterOptions{
		HandlerFunc: vaulthttp.Handler,
		NumCores:    1,
		Logger:      hclogging.NewVaultLogger(hclog.Info),
	})
	cluster.Start()

	// Create KV V2 mount
	if err := cluster.Cores[0].Client.Sys().Mount("kv", &vault.MountInput{
		Type: "kv-v2",
		Options: map[string]string{
			"version": "2",
		},
	}); err != nil {
		t.Fatal(err)
	}

	vaultClient := cluster.Cores[0].Client
	vlogical := vaultClient.Logical()

	// Enable approle
	err := vaultClient.Sys().EnableAuthWithOptions("approle", &vault.EnableAuthOptions{
		Type: "approle",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create an approle
	_, err = vlogical.Write("auth/approle/role/unittest", map[string]interface{}{
		"policies": []string{"unittest"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Gets the role ID, that is basically the 'username' used to log into vault
	res, err := vlogical.Read("auth/approle/role/unittest/role-id")
	if err != nil {
		t.Fatal(err)
	}

	// Keep the roleID for later use
	roleID, ok := res.Data["role_id"].(string)
	if !ok {
		t.Fatal("Could not read the approle")
	}

	// Create a secretID that is basically the password for the approle
	res, err = vlogical.Write("auth/approle/role/unittest/secret-id", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Use thre secretID later
	secretID, ok := res.Data["secret_id"].(string)
	if !ok {
		t.Fatal("Could not generate the secret id")
	}

	// Create a broad policy to allow the approle to do whatever
	err = vaultClient.Sys().PutPolicy("unittest", `
path "*" {
  capabilities = ["create", "read", "list", "update", "delete"]
}`)
	if err != nil {
		t.Fatal(err)
	}

	test := "abc"
	if err := setKey(vlogical, "kv/data/license:data/key", map[string]string{"key": test}); err != nil {
		t.Error(err)
	}

	value, err := lookupKey(vlogical, "kv/data/license:data/key")
	if err != nil {
		t.Error(err)
	}
	if value != test {
		t.Errorf("Invalid lookup: got %v, expected %v", value, test)
	}

	if err := setKey(vlogical, "kv/data/sign/:data/private_key", map[string]string{"private_key": "private_key1"}); err != nil {
		t.Error(err)
	}
	if err := setKey(vlogical, "kv/data/acmecorp/bearer:data/token", map[string]string{"token": "token1"}); err != nil {
		t.Error(err)
	}
	if err := setKey(vlogical, "kv/data/acmecorp:data/url", map[string]string{"url": "http://127.0.0.1:9000"}); err != nil {
		t.Error(err)
	}

	_, err = lookupKey(vlogical, "kv/data/acmecorp/bearer:data/token")
	if err != nil {
		t.Error(err)
	}
	_, err = lookupKey(vlogical, "kv/data/acmecorp:data/url")
	if err != nil {
		t.Error(err)
	}
	return cluster, roleID, secretID
}

func TestRoleEKM(t *testing.T) {
	cluster, roleID, secretID := createVaultTestRoleCluster(t)
	defer cluster.Cleanup()

	addr := cluster.Cores[0].Listeners[0].Address.String()

	t.Run("EKM", func(t *testing.T) {
		conf := config.Config{
			EKM:      []byte(`{"vault": {"license": {"key": "kv/data/license:data/key"}, "rootca": "` + strings.ReplaceAll(string(cluster.CACertPEM), "\n", "\\n") + `", "url": "https://` + addr + `", "access_type": "approle", "approle": {"role_id": "` + roleID + `", "secret_id": "` + secretID + `"}, "keys": {"jwt_signing.key": "kv/data/sign:data/private_key"}, "services": {"acmecorp.url": "kv/data/acmecorp:data/url", "acmecorp.credentials.bearer.token": "kv/data/acmecorp/bearer:data/token"} } }`),
			Services: []byte(`{"acmecorp": {"credentials": {"bearer": {"token": "bear"} } } }`),
			Keys:     []byte(`{"jwt_signing": {"key": "test"} }`),
		}

		e := NewEKM(nil, nil)
		cnf, err := e.ProcessEKM(plugins.EkmPlugins, logging.Get(), &conf)
		if err != nil {
			t.Error(err)
		}
		if !bytes.Equal(cnf.Services, []byte(`{"acmecorp":{"credentials":{"bearer":{"token":"token1"}},"url":"http://127.0.0.1:9000"}}`)) {
			t.Errorf("invalid services: got %v", string(cnf.Services))
		}

		if !bytes.Equal(cnf.Keys, []byte(`{"jwt_signing":{"key":"private_key1"}}`)) {
			t.Errorf("invalid keys: got %v", string(cnf.Keys))
		}
	})
}
