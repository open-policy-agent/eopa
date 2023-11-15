//go:build e2e

// package git is for testing Enterprise OPA as container, running as server,
// interacting with remote git repositories.
package git

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-cmp/cmp"
	"github.com/open-policy-agent/opa/util"
	"github.com/ory/dockertest/v3"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	_ "github.com/styrainc/enterprise-opa-private/pkg/plugins/data/git"
)

const (
	defaultImage   = "ko.local/enterprise-opa-private:edge" // built via `make build-local`
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

	os.Exit(m.Run())
}

var dockerPool = func() *dockertest.Pool {
	p, err := dockertest.NewPool("")
	if err != nil {
		panic(err)
	}

	if err = p.Client.Ping(); err != nil {
		panic(err)
	}
	return p
}()

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
			cleanupPrevious(t)
			image := os.Getenv("IMAGE")
			if image == "" {
				image = defaultImage
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
			eopa := loadEnterpriseOPA(t, config, image, eopaHTTPPort)
			host := eopa.GetHostPort(fmt.Sprintf("%d/tcp", eopaHTTPPort))
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

func loadEnterpriseOPA(t *testing.T, config, image string, httpPort int) *dockertest.Resource {
	img := strings.Split(image, ":")

	dir := t.TempDir()
	confPath := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(confPath, []byte(config), 0x777); err != nil {
		t.Fatalf("write config: %v", err)
	}

	eopa, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Name:       "eopa-e2e",
		Repository: img[0],
		Tag:        img[1],
		Hostname:   "eopa-e2e",
		Env: []string{
			"EOPA_LICENSE_TOKEN=" + os.Getenv("EOPA_LICENSE_TOKEN"),
			"EOPA_LICENSE_KEY=" + os.Getenv("EOPA_LICENSE_KEY"),
		},
		Mounts: []string{
			confPath + ":/config.yml",
		},
		ExposedPorts: []string{fmt.Sprintf("%d/tcp", httpPort)},
		Cmd:          strings.Split("run --server --addr "+fmt.Sprintf(":%d", httpPort)+" --config-file /config.yml --log-level debug --disable-telemetry", " "),
	})
	if err != nil {
		t.Fatalf("could not start %s: %s", image, err)
	}

	t.Cleanup(func() {
		if err := dockerPool.Purge(eopa); err != nil {
			t.Fatalf("could not purge eopa: %s", err)
		}
	})

	if err := dockerPool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		req, err := http.NewRequest("GET", "http://localhost:"+eopa.GetPort(fmt.Sprintf("%d/tcp", httpPort))+"/v1/data/system", nil)
		if err != nil {
			t.Fatalf("http request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return nil
	}); err != nil {
		t.Fatalf("could not connect to enterprise OPA: %s", err)
	}

	return eopa
}

func checkEnterpriseOPA(t *testing.T, host string, exp any) {
	if err := util.WaitFunc(func() bool {
		// check store response (TODO: check metrics/status when we have them)
		resp, err := http.Get("http://" + host + "/v1/data/git/e2e")
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

func cleanupPrevious(t *testing.T) {
	t.Helper()
	for _, n := range []string{"eopa-e2e"} {
		if err := dockerPool.RemoveContainerByName(n); err != nil {
			t.Fatalf("remove %s: %v", n, err)
		}
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
