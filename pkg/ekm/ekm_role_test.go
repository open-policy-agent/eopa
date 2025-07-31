// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package ekm

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	hcvault "github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	testcontainervault "github.com/testcontainers/testcontainers-go/modules/vault"

	"github.com/open-policy-agent/opa/v1/config"
	"github.com/open-policy-agent/opa/v1/logging"
)

func createVaultTestRoleCluster(t *testing.T) (*testcontainervault.VaultContainer, *hcvault.Client, string, string) {
	t.Helper()

	opts := []testcontainers.ContainerCustomizer{
		testcontainervault.WithToken(token),
		testcontainervault.WithInitCommand("secrets enable -version=2 -path=kv kv"),
	}

	vault, err := testcontainervault.Run(context.Background(), "hashicorp/vault:1.15.4", opts...)
	if err != nil {
		t.Fatal(err)
	}

	address, err := vault.HttpHostAddress(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	vaultCfg := hcvault.DefaultConfig()
	vaultCfg.Address = address
	vaultClient, err := hcvault.NewClient(vaultCfg)
	if err != nil {
		t.Fatal(err)
	}
	vaultClient.SetToken(token)

	vlogical := vaultClient.Logical()

	// Enable approle
	err = vaultClient.Sys().EnableAuthWithOptions("approle", &hcvault.EnableAuthOptions{
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
	if err := setKey(vlogical, "kv/data/acmecorp:data/url", map[string]string{"url": address}); err != nil {
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
	return vault, vaultClient, roleID, secretID
}

func TestRoleEKM(t *testing.T) {
	vault, vaultClient, roleID, secretID := createVaultTestRoleCluster(t)
	defer func() {
		vlogical := vaultClient.Logical()
		vlogical.Delete("auth/approle/role/unittest")
		vault.Terminate(context.Background())
	}()

	address, _ := vault.HttpHostAddress(context.Background())

	t.Run("EKM", func(t *testing.T) {
		conf := config.Config{
			Extra:    map[string]json.RawMessage{"ekm": []byte(`{"vault": {"license": {"key": "kv/data/license:data/key"}, "url": "` + address + `", "access_type": "approle", "approle": {"role_id": "` + roleID + `", "secret_id": "` + secretID + `"}, "keys": {"jwt_signing.key": "kv/data/sign:data/private_key"}, "services": {"acmecorp.url": "kv/data/acmecorp:data/url", "acmecorp.credentials.bearer.token": "kv/data/acmecorp/bearer:data/token"} } }`)},
			Services: []byte(`{"acmecorp": {"credentials": {"bearer": {"token": "bear"} } } }`),
			Keys:     []byte(`{"jwt_signing": {"key": "test"} }`),
		}

		e := NewEKM()
		e.SetLogger(logging.New())
		cnf, err := e.OnConfig(context.Background(), &conf)
		if err != nil {
			t.Error(err)
		}
		if !bytes.Equal(cnf.Services, []byte(`{"acmecorp":{"credentials":{"bearer":{"token":"token1"}},"url":"`+address+`"}}`)) {
			t.Errorf("invalid services: got %v", string(cnf.Services))
		}

		if !bytes.Equal(cnf.Keys, []byte(`{"jwt_signing":{"key":"private_key1"}}`)) {
			t.Errorf("invalid keys: got %v", string(cnf.Keys))
		}
	})
}
