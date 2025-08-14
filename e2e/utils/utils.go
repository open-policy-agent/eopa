// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"embed"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

// Tries an open port 3x times with short delays between each time to ensure the port is really free.
func IsTCPPortBindable(port int) bool {
	portOpen := true
	for i := 0; i < 3; i++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			ln.Close()
		}
		portOpen = portOpen && err == nil
		time.Sleep(time.Millisecond) // Adjust the delay as needed
	}
	return portOpen
}

func ExplodeEmbed(t *testing.T, efs embed.FS) string {
	dir := t.TempDir()
	ds, err := efs.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range ds {
		bs, err := efs.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name()), bs, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func IncludeLicenseEnvVars(e *testscript.Env) error {
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "EOPA_") {
			e.Vars = append(e.Vars, kv)
		}
	}
	return nil
}

// TestscriptExtraFunctions returns extra functions uitable for use with the
// testscript package, which might be useful for tests. The benefit of
// implementing such functionality in this way rather than using a shell
// script, or another scripting tool like babashka or python, is that we can
// ensure that the custom commands are available on all platforms without
// needing to set up any special PATH variables or install extra depenandices,
// which can be an issue in CI, especially for cross testing.
//
// These additional functions are available:
//
// httpwait <url> <expected value> [<retries>] [<interval>] [<backoff coefficient>]
//
//	httpwait will repeatedly request thie given URL until it receives a
//	body response that equals the expected value. Both values are trimmed
//	before comparison. If the check fails, it will be retried up to
//	<retries> many times, waiting <interval> ms after each retry, with the
//	<interval> being multiplied by the backoff coefficient during each
//	iteration.
//
//	defaults:  retries=8 interval=128 backoff=2
func TestscriptExtraFunctions() map[string]func(*testscript.TestScript, bool, []string) {
	// I implemented this as a function that returns the map, since you
	// can't have a const map, and I don't want to create a global variable
	// that could potentially be mutated at runtime.
	//
	// -- CAD 2023-12-19

	return map[string]func(*testscript.TestScript, bool, []string){
		"httpwait": func(ts *testscript.TestScript, neg bool, args []string) {
			usage := func() {
				ts.Fatalf("usage: httpwait <url> <expected value> [<retries>] [<interval>] [<backoff coefficient>]")
				return
			}

			if len(args) < 2 {
				usage()
			}

			trimset := " \n\t\r"
			url := args[0]
			expect := strings.Trim(args[1], trimset)
			retries := 8
			interval := 128.0
			backoff := 2.0

			if len(args) >= 3 {
				i, err := strconv.Atoi(args[2])
				if err != nil {
					ts.Fatalf("httpwait: expected retries to be an integer: %s", err.Error())
					return
				}
				retries = i
			}

			if len(args) >= 4 {
				f, err := strconv.ParseFloat(args[3], 64)
				if err != nil {
					ts.Fatalf("httpwait: expected interval to be a float: %s", err.Error())
					return
				}
				interval = f
			}

			if len(args) >= 5 {
				f, err := strconv.ParseFloat(args[4], 64)
				if err != nil {
					ts.Fatalf("httpwait: expected backoff to be a float: %s", err.Error())
					return
				}
				backoff = f
			}

			ts.Logf("httpwait url='%s' expect='%s' retries=%d interval=%f backoff=%f", url, expect, retries, interval, backoff)

			for {
				time.Sleep(time.Duration(interval) * time.Millisecond)

				if retries < 0 {
					ts.Fatalf("httpwait: no retries remaining for URL '%s', expected value '%s'", url, expect)
					return
				}

				retries--
				interval = interval * backoff

				req, err := http.NewRequest("GET", url, nil)
				if err != nil {
					ts.Logf("httpwait: failed to create http request: %s", err.Error())
					continue
				}

				res, err := StdlibHTTPClient.Do(req)
				if err != nil {
					ts.Logf("httpwait: failed to execute http request: %s", err.Error())
					continue
				}

				defer res.Body.Close()
				body, err := io.ReadAll(res.Body)
				if err != nil {
					ts.Logf("httpwait: failed to read http response body: %s", err.Error())
					continue
				}

				bodys := strings.Trim(string(body), trimset)

				if bodys == expect {
					ts.Logf("httpwait: received expected response from URL '%s', done waiting", url)
					return
				} else {
					ts.Logf("httpwait: actual response '%s' did not match expected response '%s'", bodys, expect)
					continue
				}
			}
		},
	}
}
