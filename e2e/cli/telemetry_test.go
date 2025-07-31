// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/open-policy-agent/eopa/e2e/wait"
)

func TestRunWithoutTelemetry(t *testing.T) {
	data, config := `{}`, ``
	policy := `package test
p := true`

	for _, flag := range []string{"--disable-telemetry", "--skip-version-check"} {
		t.Run(flag, func(t *testing.T) {
			eopa, eopaOut := eopaRun(t, policy, data, config, eopaHTTPPort, flag)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaOut, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			// TODO(sr): we can't do much better than wait for a result
			// (on linux, netns would solve this better)
			time.Sleep(3 * time.Second)

			buf := bytes.Buffer{}
			if _, err := io.Copy(&buf, eopaOut); err != nil {
				t.Fatal(err)
			}
			if _, err := buf.ReadBytes('{'); err == nil {
				if err := buf.UnreadByte(); err != nil {
					t.Fatal(err)
				}
			}
			dec := json.NewDecoder(&buf)
			for {
				m := struct {
					Msg string `json:"msg"`
				}{}
				if err := dec.Decode(&m); err == io.EOF { // ignore other err
					break
				} else if err != nil {
					t.Errorf("Decode: %v", err)
				}
				if strings.Contains(m.Msg, "is up to date.") {
					t.Fatalf("expected no message, got %v", m.Msg)
				}
			}
		})
	}
}
