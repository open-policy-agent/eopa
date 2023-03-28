//go:build e2e

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvalInstructionLimit(t *testing.T) {
	data := largeJSON()

	t.Run("limit=1", func(t *testing.T) {
		load := loadEval(t, "1", data)
		out, err := load.Output()
		if err == nil {
			t.Fatalf("expected error, got output: %s", string(out))
		}
		output := struct {
			Errors []struct {
				Message string
			}
		}{}
		if err := json.NewDecoder(bytes.NewReader(out)).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := 1, len(output.Errors); exp != act {
			t.Fatalf("expected %d errors, got %d", exp, act)
		}
		if exp, act := "instructions limit exceeded", output.Errors[0].Message; exp != act {
			t.Fatalf("expected message %q, got %q", exp, act)
		}
	})

	t.Run("limit=10000", func(t *testing.T) {
		load := loadEval(t, "10000", data)
		_, err := load.Output()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestRunInstructionLimit(t *testing.T) {
	data := largeJSON()
	config := ``
	policy := `package test
p { data.xs[_] }`
	ctx := context.Background()

	t.Run("limit=1", func(t *testing.T) {
		load, loadOut := loadRun(t, policy, data, config, "--instruction-limit", "1")
		if err := load.Start(); err != nil {
			t.Fatal(err)
		}
		waitForLog(ctx, t, loadOut, 1, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		req, err := http.NewRequest("POST", "http://localhost:38181/v1/data/test/p", nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := http.StatusInternalServerError, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
		output := struct {
			Code    string
			Message string
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := "instructions limit exceeded", output.Message; exp != act {
			t.Fatalf("expected message %q, got %q", exp, act)
		}
		if exp, act := "internal_error", output.Code; exp != act {
			t.Fatalf("expected code %q, got %q", exp, act)
		}
	})

	t.Run("limit=10000", func(t *testing.T) {
		load, loadOut := loadRun(t, policy, data, config, "--instruction-limit", "10000")
		if err := load.Start(); err != nil {
			t.Fatal(err)
		}
		waitForLog(ctx, t, loadOut, 1, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		req, err := http.NewRequest("POST", "http://localhost:38181/v1/data/test/p", nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if exp, act := http.StatusOK, resp.StatusCode; exp != act {
			t.Fatalf("expected status %d, got %d", exp, act)
		}
		output := struct {
			Result any
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatal(err)
		}
		if exp, act := true, output.Result; exp != act {
			t.Fatalf("expected result %v, got %v", exp, act)
		}
	})
}

// NOTE(sr): This isn't *all* plugins -- the data plugin isn't loading its sub-plugins.
// But it's most of them.
func TestRunWithAllPlugins(t *testing.T) {
	ctx := context.Background()
	policy := `package test`
	data := `{}`
	config := `
plugins:
  impact_analysis: {}
  grpc:
    addr: "127.0.0.1:9191"
  data: {}
`
	load, loadOut := loadRun(t, policy, data, config)
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	waitForLog(ctx, t, loadOut, 1, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)
}

func loadEval(t *testing.T, limit, data string) *exec.Cmd {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(data), 0x777); err != nil {
		t.Fatalf("write data file: %v", err)
	}
	return exec.Command(binary(), strings.Split("eval --instruction-limit "+limit+" --data "+dataPath+" data.xs[_]", " ")...)
}

func loadRun(t *testing.T, policy, data, config string, extraArgs ...string) (*exec.Cmd, *bytes.Buffer) {
	logLevel := "debug"
	buf := bytes.Buffer{}
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "eval.rego")
	if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(data), 0x777); err != nil {
		t.Fatalf("write data: %v", err)
	}

	args := []string{
		"run",
		"--server",
		"--addr", "localhost:38181",
		"--log-level", logLevel,
		"--disable-telemetry",
	}
	if config != "" {
		configPath := filepath.Join(dir, "config.yml")
		if err := os.WriteFile(configPath, []byte(config), 0x777); err != nil {
			t.Fatalf("write config: %v", err)
		}
		args = append(args, "--config-file", configPath)
	}
	args = append(args, extraArgs...)
	args = append(args,
		dataPath,
		policyPath,
	)
	load := exec.Command(binary(), args...)
	load.Stderr = &buf
	load.Env = append(load.Environ(),
		"STYRA_LOAD_LICENSE_TOKEN="+os.Getenv("STYRA_LOAD_LICENSE_TOKEN"),
		"STYRA_LOAD_LICENSE_KEY="+os.Getenv("STYRA_LOAD_LICENSE_KEY"),
	)

	t.Cleanup(func() {
		if load.Process == nil {
			return
		}
		if err := load.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		load.Wait()
		if testing.Verbose() {
			t.Logf("load output:\n%s", buf.String())
		}
	})

	return load, &buf
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "load"
	}
	return bin
}

func largeJSON() string {
	data := strings.Builder{}
	data.WriteString(`{"xs": [`)
	for i := 0; i <= 1000; i++ {
		if i != 0 {
			data.WriteRune(',')
		}
		data.WriteString(`{"a": 1, "b": 2}`)
	}
	data.WriteString(`]}`)
	return data.String()
}

func waitForLog(ctx context.Context, t *testing.T, rdr io.Reader, exp int, assert func(string) bool, dur time.Duration) {
	t.Helper()
	for i := 0; i <= 3; i++ {
		if i != 0 {
			time.Sleep(dur)
		}
		if act := retrieveMsg(ctx, t, rdr, assert); act == exp {
			return
		} else if i == 3 {
			t.Fatalf("expected %d matching, got %d", exp, act)
		}
	}
	return
}

func retrieveMsg(ctx context.Context, t *testing.T, rdr io.Reader, assert func(string) bool) int {
	t.Helper()
	c := 0
	// Skip any non-JSON logs coming from CLI arg validation
	buf := bytes.Buffer{}
	if _, err := io.Copy(&buf, rdr); err != nil {
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
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode console logs: %v", err)
		}
		if assert(m.Msg) {
			c++
		}
	}
	return c
}
