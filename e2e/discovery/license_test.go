//go:build e2e

package discovery

import (
	"encoding/json"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

func TestDiscoveryLicenseCheck(t *testing.T) {
	for _, tc := range []struct {
		note                string
		bundle              string
		expectedLogMessages []string
	}{
		{
			note:   "plain bundle",
			bundle: "disco.tar.gz",
			expectedLogMessages: []string{
				"Starting discovery license check.",
				"Discovery license check failed.",
			},
		},
		{
			note:   "BJSON bundle",
			bundle: "disco.bjson.tar.gz",
			expectedLogMessages: []string{
				"Starting discovery license check.",
				"Discovery license check failed.",
			},
		},
		{
			note:   "plain bundle - DAS license",
			bundle: "disco-license.tar.gz",
			expectedLogMessages: []string{
				"Starting discovery license check.",
				"Discovery license check successful.",
			},
		},
		{
			note:   "BJSON bundle - DAS license",
			bundle: "disco-license.bjson.tar.gz",
			expectedLogMessages: []string{
				"Starting discovery license check.",
				"Discovery license check successful.",
			},
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			config := config(tc.bundle, testserver.URL)
			eopa, eopaOut := eopaRun(t, config, eopaHTTPPort, false)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			// Note: Another valid choice would be to wait for the "server initialized" message here.
			wait.ForLog(t, eopaOut, equals("Discovery update processed successfully."), 1*time.Second)

			// Collect the log messages from the JSON-formatted EOPA logs.
			logs := retrieveLogs(t, eopaOut)
			logMessages := make([]string, 0, len(logs))
			for _, log := range logs {
				if msg, ok := log["msg"].(string); ok {
					logMessages = append(logMessages, msg)
				}
			}

			// Search through the logs, expecting messages to appear in-order,
			// possibly with other log messages interspersed between them.
			idx := 0
			for _, expMsg := range tc.expectedLogMessages {
				if found := slices.Index(logMessages[idx:], expMsg); found != -1 {
					idx += found + 1
				} else {
					t.Fatalf("Missing log message: %v", expMsg)
				}
			}
		})
	}
}

// A helper function that converts EOPA's JSON-formatted log message objects
// into a slice of map[string]any types, for easier wrangling in tests.
func retrieveLogs(t *testing.T, rdr io.Reader) []map[string]any {
	t.Helper()
	ms := []map[string]any{}
	dec := json.NewDecoder(rdr)
	for {
		m := map[string]any{}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode recorded DL: %v", err)
		}
		if len(m) > 0 {
			ms = append(ms, m)
		}
	}
	return ms
}
