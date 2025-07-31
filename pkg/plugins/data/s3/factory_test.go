// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"testing"

	inmem "github.com/open-policy-agent/eopa/pkg/storage"
	"github.com/open-policy-agent/opa/v1/plugins"
)

func TestS3ConfigEndpoint(t *testing.T) {
	raw := `
plugins:
  data:
    s3.foo:
`
	s3 := `      endpoint: "https://whatever"
      url: "s3://bucket"
      access_id: acc
      secret: sec
`
	path := `      path: s3.foo`

	mgr, err := plugins.New([]byte(raw+s3), "test-instance-id", inmem.New())
	if err != nil {
		t.Fatal(err)
	}
	dp, err := Factory().Validate(mgr, []byte(s3+path))
	if err != nil {
		t.Fatal(err)
	}
	act := dp.(Config)
	if exp, act := "https://whatever", act.endpoint; exp != act {
		t.Errorf("expected endpoint = %v, got %v", exp, act)
	}
}
