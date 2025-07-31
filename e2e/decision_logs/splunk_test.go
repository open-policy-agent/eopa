// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package decisionlogs

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/open-policy-agent/eopa/e2e/wait"
)

func TestDecisionLogSplunk(t *testing.T) {
	policy := `package test`

	for _, tc := range []struct {
		note, configFmt string
		compressed      bool
	}{
		{
			note: "no compression",
			configFmt: `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
      type: splunk
      url: %[1]s/services/collector/event
      token: secret
`,
		},
		{
			note:       "with compression",
			compressed: true,
			configFmt: `
decision_logs:
  plugin: eopa_dl
plugins:
  eopa_dl:
    output:
      type: splunk
      url: %[1]s/services/collector/event
      token: secret
      batching:
        at_period: 10ms
        compress: true
`,
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			buf := bytes.Buffer{}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path != "/services/collector/event":
				case r.Method != http.MethodPost:
				case r.Header.Get("Content-Type") != "application/json":
				case r.Header.Get("Authorization") != "Splunk secret":
				default: // all matches
					var src io.ReadCloser
					if tc.compressed {
						src, _ = gzip.NewReader(r.Body)
					} else {
						src = r.Body
					}
					io.Copy(&buf, src)
					return
				}
				bs, err := httputil.DumpRequest(r, !tc.compressed)
				if err != nil {
					panic(err)
				}
				t.Logf("bad request: %s", string(bs))
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(ts.Close)
			eopa, _, eopaErr := loadEOPA(t, fmt.Sprintf(tc.configFmt, ts.URL), policy, eopaHTTPPort, false)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			{ // act: send request
				req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d/v1/data/test/p", eopaHTTPPort), strings.NewReader(`{"input": {"a": "b"}}`))
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

			type splunkPayload struct {
				Event payload
				Time  int
			}
			var s splunkPayload
			for i := 0; i <= 2; i++ {
				time.Sleep(time.Duration(i*100) * time.Millisecond)
				if err := json.NewDecoder(&buf).Decode(&s); err != nil {
					if i == 2 {
						t.Fatalf("failed to find event data in %s", buf.String())
					}
					continue
				}
				break
			}

			if s.Time == 0 {
				t.Errorf("expected time, got 0")
			}

			if exp, act := int(time.Now().Unix()), s.Time; exp < act {
				t.Errorf("expected time >= %d, got %d", exp, act)
			}
			{ // log for act 1
				dl := payload{
					Input:  map[string]any{"a": "b"},
					ID:     1,
					Labels: standardLabels,
				}
				if diff := cmp.Diff(dl, s.Event, stdIgnores); diff != "" {
					t.Errorf("diff: (-want +got):\n%s", diff)
				}
			}
		})
	}
}
