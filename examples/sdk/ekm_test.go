// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package sdk_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	eopa_sdk "github.com/open-policy-agent/eopa/pkg/sdk"
	"github.com/open-policy-agent/opa/v1/sdk"
	"github.com/open-policy-agent/opa/v1/storage"

	// These dependencies are only for demonstration purposes
	hcvault "github.com/hashicorp/vault/api"
	"github.com/testcontainers/testcontainers-go"
	testcontainervault "github.com/testcontainers/testcontainers-go/modules/vault"
)

const token = "demonstration-token"

func ExampleEKM() {
	ctx := context.Background()
	vault := startVaultServer(ctx)
	defer vault.Terminate(ctx)
	vaultAddr, err := vault.HttpHostAddress(ctx)
	if err != nil {
		panic(err)
	}

	configFmt := `
ekm:
  vault:
    access_type: token
    token: demonstration-token
    url: "%[2]s"
    httpsend:
      "%[1]s":
        headers:
          Authorization:
            bearer: "kv/data/httpsend/bearer:data/token"
            scheme: "kv/data/httpsend/bearer:data/scheme"
`
	opts := eopa_sdk.DefaultOptions()
	opts.Config = strings.NewReader(fmt.Sprintf(configFmt, ekmTestSrv.URL, vaultAddr))

	store := opts.Store
	if err := storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		return store.UpsertPolicy(ctx, txn, "demo", []byte(`package demo
body := http.send({"method": "GET", "url": sprintf("%s/test", [input.url])}).body
`))
	}); err != nil {
		panic(err)
	}

	o, err := sdk.New(ctx, opts)
	if err != nil {
		panic(err)
	}

	res, err := o.Decision(ctx, sdk.DecisionOptions{
		Path:  "demo/body",
		Input: map[string]any{"url": ekmTestSrv.URL},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("result: %v", res.Result)
	// Output:
	// result: true
}

var ekmTestSrv = srv(func(w http.ResponseWriter, r *http.Request) error {
	x := false
	w.Header().Add("content-type", "application/json")
	if hdr, ok := r.Header["Authorization"]; ok {
		x = hdr[0] == "Bearer sesame"
	}
	return json.NewEncoder(w).Encode(x)
})

func startVaultServer(ctx context.Context) *testcontainervault.VaultContainer {
	opts := []testcontainers.ContainerCustomizer{
		testcontainervault.WithToken(token),
		testcontainervault.WithInitCommand("secrets enable -version=2 -path=kv kv"),
	}

	vault, err := testcontainervault.Run(ctx, "hashicorp/vault:1.18", opts...)
	if err != nil {
		panic(err)
	}

	vaultCfg := hcvault.DefaultConfig()
	address, err := vault.HttpHostAddress(ctx)
	if err != nil {
		panic(err)
	}
	vaultCfg.Address = address
	vaultClient, err := hcvault.NewClient(vaultCfg)
	if err != nil {
		panic(err)
	}
	vaultClient.SetToken(token)

	vlogical := vaultClient.Logical()

	// initialize database
	if err := setKey(vlogical, "kv/data/httpsend/bearer:data/token", map[string]any{"token": "sesame", "scheme": "Bearer"}); err != nil {
		panic(err)
	}
	return vault
}

// TODO(sr): replace with "kv put" init commands?
func setKey(logical *hcvault.Logical, p string, value map[string]any) error {
	p = strings.TrimSpace(p)
	arr := strings.Split(p, ":")
	if len(arr) != 2 {
		return fmt.Errorf("Invalid path: %v", p)
	}

	path := arr[0]
	field := arr[1]

	f := strings.Split(field, "/")
	if len(arr) != 2 {
		return fmt.Errorf("Invalid field: %v", f)
	}
	secretData := map[string]any{f[0]: value}

	_, err := logical.Write(path, secretData)
	return err
}
