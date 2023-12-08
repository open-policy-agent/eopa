package utils

import (
	"embed"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

// Tries an open port 3x times with short delays between each time to ensure the port is really free.
func IsTCPPortBindable(port int) bool {
	portOpen := true
	for i := 0; i < 3; i++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
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
