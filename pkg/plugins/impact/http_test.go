// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package impact_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/open-policy-agent/eopa/pkg/plugins/impact"
)

func TestHTTPEndpointErrors(t *testing.T) {
	ctx := context.Background()

	mgr := pluginMgr(t, `
plugins:
  impact_analysis: {}`)
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}

	lia := impact.Lookup(mgr)
	if lia == nil {
		t.Fatal("impact plugin not found")
	}

	tests := []struct {
		note   string
		method string
		body   io.Reader
		path   string
		exp    int // status
	}{
		{
			method: http.MethodGet,
			exp:    http.StatusMethodNotAllowed,
		},
		{
			method: http.MethodPut,
			exp:    http.StatusMethodNotAllowed,
		},
		{
			method: http.MethodDelete,
			exp:    http.StatusMethodNotAllowed,
		},
		{
			note:   "all params/empty body",
			method: http.MethodPost,
			path:   "/v0/impact?equals=1&duration=10s&rate=1",
			exp:    http.StatusBadRequest,
		},
		{
			note:   "all required params/empty body", // "equals" is optional
			method: http.MethodPost,
			path:   "/v0/impact?duration=10s&rate=1",
			exp:    http.StatusBadRequest,
		},
		{
			note:   "missing params/rate",
			method: http.MethodPost,
			path:   "/v0/impact?equals=1&duration=10s",
			exp:    http.StatusBadRequest,
		},
		{
			note:   "missing params/duration",
			method: http.MethodPost,
			path:   "/v0/impact?equals=1&rate=1",
			exp:    http.StatusBadRequest,
		},
	}
	for _, tc := range tests {
		name := tc.method
		if tc.note != "" {
			name = name + "/" + tc.note
		}
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			path := tc.path
			if path == "" {
				path = "/v0/impact"
			}
			req := httptest.NewRequest(tc.method, path, tc.body)
			lia.ServeHTTP(rr, req)

			if exp, act := tc.exp, rr.Result().StatusCode; exp != act {
				t.Errorf("expected status %d, got %d", exp, act)
			}
			if testing.Verbose() {
				t.Logf("response: %s", rr.Body.String())
			}
		})
	}
}

func TestStartJobViaHTTP(t *testing.T) {
	ctx := context.Background()

	mgr := pluginMgr(t, `
plugins:
  impact_analysis: {}`)
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop(ctx)

	lia := impact.Lookup(mgr)
	if lia == nil {
		t.Fatal("impact plugin not found")
	}

	path := "testdata/eopa-bundle.tar.gz"
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundle file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v0/impact?equals=1&duration=100ms&rate=1", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	before := time.Now()
	lia.ServeHTTP(rr, req)

	resp := bytes.Buffer{}
	_, _ = io.Copy(&resp, rr.Result().Body)

	if exp, act := http.StatusOK, rr.Result().StatusCode; exp != act {
		t.Fatalf("expected status %d, got %d -- response: %s", exp, act, resp.String())
	}

	dur := time.Since(before)
	if dur < 100*time.Millisecond || dur > 120*time.Millisecond {
		t.Errorf("unexpected duration: %v not ~100ms", dur)
	}

	if exp, act := 0, resp.Len(); exp != act {
		t.Errorf("unexpected response body length: %d, got: %s", act, resp.String())
	}
}
