package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/types"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/utils"
)

const (
	Name = "git"

	origin  = "origin"
	gitUser = "git"
)

// Data plugin
type Data struct {
	manager        *plugins.Manager
	log            logging.Logger
	Config         Config
	exit, doneExit chan struct{}
	repository     *git.Repository
	ref            plumbing.ReferenceName

	*transform.Rego
}

// Ensure that this sub-plugin will be triggered by the data umbrella plugin,
// because it implements types.Triggerer.
var _ types.Triggerer = (*Data)(nil)

func (c *Data) Start(ctx context.Context) error {
	if err := c.Rego.Prepare(ctx); err != nil {
		return fmt.Errorf("prepare rego_transform: %w", err)
	}
	r, err := git.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		return err
	}
	if _, err := r.CreateRemote(&config.RemoteConfig{
		Name: origin,
		URLs: []string{c.Config.url},
	}); err != nil {
		return err
	}
	c.repository = r

	c.exit = make(chan struct{})
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		return storage.MakeDir(ctx, c.manager.Store, txn, c.Config.path)
	}); err != nil {
		return err
	}

	c.doneExit = make(chan struct{})
	go c.loop(ctx)
	return nil
}

func (c *Data) Stop(context.Context) {
	if c.doneExit == nil {
		return
	}
	close(c.exit) // stops our polling loop
	<-c.doneExit  // waits for polling loop to be stopped
}

func (c *Data) Reconfigure(ctx context.Context, next any) {
	if c.Config.Equal(next.(Config)) {
		return // nothing to do
	}
	if c.doneExit != nil { // started before
		c.Stop(ctx)
	}
	c.Config = next.(Config)
	c.Start(ctx)
}

// dataPlugin accessors
func (c *Data) Name() string {
	return Name
}

func (c *Data) Path() storage.Path {
	return c.Config.path
}

func (c *Data) loop(ctx context.Context) {
	timer := time.NewTimer(0) // zero timer is needed to execute immediately for first time

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case <-c.exit:
			break LOOP
		case <-timer.C:
		}
		if err := c.poll(ctx); err != nil {
			c.log.Error("polling data from git failed: %+v", err)
		}
		timer.Reset(c.Config.interval)
	}
	// stop and drain the timer
	if !timer.Stop() && len(timer.C) > 0 {
		<-timer.C
	}
	close(c.doneExit)
}

func (c *Data) poll(ctx context.Context) error {
	if err := c.fetch(ctx); errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil // no updates, go to next round
	} else if err != nil {
		return err
	}

	cm, err := c.getCommit()
	if err != nil {
		return err
	}

	tree, filterFilename, err := c.getTree(cm)
	if err != nil {
		return err
	}
	results, err := c.processTree(tree, filterFilename)
	if err != nil {
		return err
	}

	if err := c.Rego.Ingest(ctx, c.Path(), results); err != nil {
		return fmt.Errorf("plugin %s at %s: %w", c.Name(), c.Config.path, err)
	}
	return nil
}

func (c *Data) fetch(ctx context.Context) error {
	fetch := func(refspec config.RefSpec) error {
		return c.repository.FetchContext(ctx, &git.FetchOptions{
			RemoteName: origin,
			Auth:       c.Config.auth,
			Depth:      1,
			RefSpecs:   []config.RefSpec{refspec},
		})
	}
	var refspec config.RefSpec
	switch {
	case c.ref != "": // scenario 0: we already know the ref from prev run
		refspec = config.RefSpec(fmt.Sprintf("+%s:%s", c.ref, c.ref))
	case c.Config.commit != plumbing.ZeroHash: // scenario 1: pinned to commit
		refspec = config.RefSpec(fmt.Sprintf("%s:refs/heads/branch", c.Config.commit))
	case c.Config.Branch != "": // scenario 2: pinned to a branch
		c.ref = plumbing.NewBranchReferenceName(c.Config.Branch)
		refspec = config.RefSpec(fmt.Sprintf("+%s:%s", c.ref, c.ref))
	case c.Config.Ref != "": // scenario 3: pinned to a reference
		c.ref = plumbing.ReferenceName(c.Config.Ref)
		refspec = config.RefSpec(fmt.Sprintf("+%s:%s", c.Config.Ref, c.Config.Ref))
	}
	if refspec == "" { // scenario 3: try default references (main or master)
		var err error
		for _, name := range []string{"main", "master"} {
			ref := plumbing.NewBranchReferenceName(name)
			refspec := config.RefSpec(fmt.Sprintf("+%s:%s", ref, ref))
			err = fetch(refspec)
			if err == nil {
				c.ref = ref
				return nil
			} else if !errors.Is(err, git.NoMatchingRefSpecError{}) {
				return fmt.Errorf("fetching by default branch name %q (%s) failed: %w", ref, refspec, err)
			}
		}
		return err
	}

	if err := fetch(refspec); err != nil {
		return fmt.Errorf("fetching by refspec %q failed: %w", refspec, err)
	}
	return nil
}

func (c *Data) getCommit() (*object.Commit, error) {
	sha := c.Config.commit
	if sha == plumbing.ZeroHash {
		r, err := c.repository.Reference(c.ref, true)
		if err != nil {
			return nil, fmt.Errorf("getting a reference by name %q failed: %w", c.ref, err)
		}
		sha = r.Hash()
	}

	co, err := c.repository.CommitObject(sha)
	if err != nil {
		return nil, fmt.Errorf("getting commit object by sha %q failed: %w", sha, err)
	}
	return co, err
}

func (c *Data) getTree(commit *object.Commit) (*object.Tree, string, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, "", fmt.Errorf("could not get a Tree: %w", err)
	}
	var filterFilename string
	if c.Config.FilePath != "" {
		subTree, err := tree.Tree(c.Config.FilePath)
		if errors.Is(err, object.ErrDirectoryNotFound) {
			var dir string
			dir, filterFilename = path.Split(c.Config.FilePath)
			dir = path.Clean(dir)
			if dir == "" || dir == "." {
				return tree, filterFilename, nil
			}
			subTree, err = tree.Tree(dir)
		}
		if err != nil {
			return nil, "", fmt.Errorf("could not get a Tree for path %q: %w", c.Config.FilePath, err)
		}
		tree = subTree
	}
	return tree, filterFilename, nil
}

func (c *Data) processTree(tree *object.Tree, filterFilename string) (any, error) {
	iter := tree.Files()
	defer iter.Close()

	var ff []*object.File
	err := iter.ForEach(func(f *object.File) error {
		if filterFilename == "" || f.Name == filterFilename {
			ff = append(ff, f)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not read the files: %w", err)
	}

	switch len(ff) {
	case 0:
		return nil, nil
	case 1:
		f := ff[0]
		r, err := f.Reader()
		if err != nil {
			return nil, fmt.Errorf("could not obtain a reader: %w", err)
		}

		return utils.ParseFile(f.Name, r.(io.Reader))
	}

	files := make(map[string]any)
	for _, f := range ff {
		name := f.Name
		r, err := f.Reader()
		if err != nil {
			return nil, fmt.Errorf("could not obtain a reader: %w", err)
		}
		document, err := utils.ParseFile(name, r)
		if err != nil {
			return nil, err
		}
		if document == nil {
			continue
		}

		utils.InsertFile(files, strings.Split(name, "/"), document)
	}

	if len(files) == 0 {
		return nil, nil
	}

	return files, nil
}
