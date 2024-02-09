//go:build e2e

// package git is for testing Enterprise OPA as container, running as server,
// interacting with remote git repositories.
package git

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	gittransport "github.com/go-git/go-git/v5/plumbing/transport"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-cmp/cmp"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	"github.com/styrainc/enterprise-opa-private/e2e/wait"
)

const (
	configTemplate = `
plugins:
  data:
    git.e2e:
      type: git
      url: %s
      username: %s
      password: %s
      file_path: data/ds-test.json
      branch: %s
      polling_interval: 10s
`
)

var eopaHTTPPort int

func TestMain(m *testing.M) {
	r := rand.New(rand.NewSource(2908))
	for {
		port := r.Intn(38181) + 1
		if utils.IsTCPPortBindable(port) {
			eopaHTTPPort = port
			break
		}
	}

	// Azure DevOps requires capabilities multi_ack / multi_ack_detailed,
	// The basic level is supported, but disabled by default.
	// Enabling multi_ack by removing them from UnsupportedCapabilities
	caps := make([]capability.Capability, 0, len(gittransport.UnsupportedCapabilities))
	for _, c := range gittransport.UnsupportedCapabilities {
		if c == capability.MultiACK || c == capability.MultiACKDetailed {
			continue
		}
		caps = append(caps, c)
	}
	gittransport.UnsupportedCapabilities = caps

	c := githttp.NewClient(&http.Client{Transport: http.DefaultTransport})
	// Override http and https protocols to enrich the errors with the response bodies
	gitclient.InstallProtocol("http", c)
	gitclient.InstallProtocol("https", c)

	os.Exit(m.Run())
}

func TestGitPlugin(t *testing.T) {
	for _, tt := range []struct {
		name     string
		url      string
		username string
		token    string
	}{
		{
			name:     "github",
			url:      "https://github.com/StyraInc/e2e-git-datasource.git",
			username: "git",
			token:    os.Getenv("GIT_GITHUB_TOKEN"),
		},
		{
			name:     "azure",
			url:      "https://styrainc@dev.azure.com/styrainc/integration-testing/_git/integration-testing",
			username: "sergey", //  TODO: change to styra-automation user
			token:    os.Getenv("GIT_AZURE_TOKEN"),
		},
		// temporary disable because of the read only token
		//{
		//	name:     "gitlab",
		//	url:      "https://gitlab.com/styrainc/e2e-git-datasource.git",
		//	username: "git",
		//	token:    os.Getenv("GIT_GITLAB_TOKEN"),
		//},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.token == "" {
				t.Skip("the environment variables are not set")
			}

			branch := "e2e-eopa-" + randString(10)
			refName := plumbing.ReferenceName("refs/heads/" + branch)
			refSpec := gitconfig.RefSpec(refName + ":" + refName)
			auth := &githttp.BasicAuth{
				Username: tt.username,
				Password: tt.token,
			}
			dir := t.TempDir()

			// clone and create remote branch
			repo, err := git.PlainClone(dir, false, &git.CloneOptions{
				Auth: auth,
				URL:  tt.url,
			})
			if err != nil {
				t.Fatal(err)
			}
			headRef, err := repo.Head()
			if err != nil {
				t.Fatal(err)
			}
			ref := plumbing.NewHashReference(refName, headRef.Hash())
			err = repo.Storer.SetReference(ref)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Push(&git.PushOptions{
				RemoteName: "origin",
				RefSpecs:   []gitconfig.RefSpec{refSpec},
				Auth:       auth,
			}); err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				// remove the remote branch
				if err := repo.Storer.RemoveReference(refName); err != nil {
					t.Fatal(err)
				}
				if err := repo.Push(&git.PushOptions{
					RemoteName: "origin",
					RefSpecs:   []gitconfig.RefSpec{refSpec},
					Auth:       auth,
					Prune:      true,
					Force:      true,
				}); err != nil {
					t.Fatal(err)
				}
			})

			// run enterprise OPA on the new branch
			config := fmt.Sprintf(configTemplate, tt.url, tt.username, tt.token, branch)
			eopa, eopaErr := eopaRun(t, config, "", eopaHTTPPort)
			if err := eopa.Start(); err != nil {
				t.Fatal(err)
			}
			wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)

			host := fmt.Sprintf("localhost:%d", eopaHTTPPort)
			checkEnterpriseOPA(t, host, map[string]any{"foo1": "bar1"})

			// update remote branch and check for the updates
			w, err := repo.Worktree()
			if err != nil {
				t.Fatal(err)
			}

			if err := w.Checkout(&git.CheckoutOptions{
				Branch: refName,
			}); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(path.Join(dir, "data", "ds-test.json"), []byte(`{"foo2":"bar2"}`), 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := w.Add(path.Join("data", "ds-test.json")); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Commit("update data/ds-test.json", &git.CommitOptions{
				Author: &object.Signature{
					Name:  "John Doe",
					Email: "john@doe.org",
					When:  time.Now(),
				},
			}); err != nil {
				t.Fatal(err)
			}
			if err := repo.Push(&git.PushOptions{
				RemoteName: "origin",
				RefSpecs:   []gitconfig.RefSpec{refSpec},
				Auth:       auth,
			}); err != nil {
				t.Fatal(err)
			}

			checkEnterpriseOPA(t, host, map[string]any{"foo2": "bar2"})
		})
	}
}

func checkEnterpriseOPA(t *testing.T, host string, exp any) {
	if err := wait.Func(func() bool {
		// check store response (TODO: check metrics/status when we have them)
		req, err := http.NewRequest("GET", "http://"+host+"/v1/data/git/e2e", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := utils.StdlibHTTPClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		act := map[string]any{}
		if err := json.Unmarshal(body, &act); err != nil {
			t.Fatal(err)
		}
		return cmp.Diff(exp, act["result"]) == ""
	}, 50*time.Millisecond, 30*time.Second); err != nil {
		t.Error(err)
	}
}

var letterRunes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func eopaRun(t *testing.T, config, policy string, httpPort int, extra ...string) (*exec.Cmd, *bytes.Buffer) {
	buf := bytes.Buffer{}
	dir := t.TempDir()
	args := []string{
		"run",
		"--server",
		"--addr", fmt.Sprintf("localhost:%d", httpPort),
		"--disable-telemetry",
		"--log-level", "debug",
	}
	if config != "" {
		configPath := filepath.Join(dir, "config.yml")
		if err := os.WriteFile(configPath, []byte(config), 0x777); err != nil {
			t.Fatalf("write config: %v", err)
		}
		args = append(args, "--config-file", configPath)
	}
	if policy != "" {
		policyPath := filepath.Join(dir, "policy.rego")
		if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
			t.Fatalf("write policy: %v", err)
		}
		args = append(args, policyPath)
	}
	if len(extra) > 0 {
		args = append(args, extra...)
	}
	eopa := exec.Command(binary(), args...)
	eopa.Stderr = &buf
	eopa.Env = append(eopa.Environ(),
		"EOPA_LICENSE_TOKEN="+os.Getenv("EOPA_LICENSE_TOKEN"),
		"EOPA_LICENSE_KEY="+os.Getenv("EOPA_LICENSE_KEY"),
	)

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		if err := eopa.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		eopa.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("enterprise OPA output:\n%s", buf.String())
		}
	})

	return eopa, &buf
}

func binary() string {
	bin := os.Getenv("BINARY")
	if bin == "" {
		return "eopa"
	}
	return bin
}
