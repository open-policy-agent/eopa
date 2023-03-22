//go:build e2e

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalInstructionLimit(t *testing.T) {
	data := strings.Builder{}
	data.WriteString(`{"xs": [`)
	for i := 0; i <= 1000; i++ {
		if i != 0 {
			data.WriteRune(',')
		}
		data.WriteString(`{"a": 1, "b": 2}`)
	}
	data.WriteString(`]}`)

	t.Run("limit=1", func(t *testing.T) {
		load := loadEval(t, "1", data.String())
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
		load := loadEval(t, "10000", data.String())
		_, err := load.Output()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func loadEval(t *testing.T, limit, data string) *exec.Cmd {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(dataPath, []byte(data), 0x777); err != nil {
		t.Fatalf("write data file: %v", err)
	}
	return exec.Command(binary(), strings.Split("eval --instruction-limit "+limit+" --data "+dataPath+" data.xs[_]", " ")...)
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "load"
	}
	return bin
}
