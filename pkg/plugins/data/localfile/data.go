// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package localfile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/util"

	"github.com/open-policy-agent/eopa/pkg/plugins/data/transform"
	"github.com/open-policy-agent/eopa/pkg/plugins/data/types"
	"github.com/open-policy-agent/eopa/pkg/plugins/data/utils"
)

const (
	Name = "localfile"
)

// Data plugin
type Data struct {
	manager        *plugins.Manager
	log            logging.Logger
	Config         Config
	exit, doneExit chan struct{}

	*transform.Rego
}

var fileBufferPool = sync.Pool{
	New: func() interface{} {
		buffer := new(bytes.Buffer)
		return buffer
	},
}

// Ensure that this sub-plugin will be triggered by the data umbrella plugin,
// because it implements types.Triggerer.
var _ types.Triggerer = (*Data)(nil)

func (c *Data) Start(ctx context.Context) error {
	c.exit = make(chan struct{})
	if err := c.Prepare(ctx); err != nil {
		return fmt.Errorf("prepare rego_transform: %w", err)
	}
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		return storage.MakeDir(ctx, c.manager.Store, txn, c.Config.path)
	}); err != nil {
		return err
	}

	c.doneExit = make(chan struct{})
	go c.loop(ctx)
	return nil
}

func (c *Data) Stop(ctx context.Context) {
	if c.doneExit == nil {
		return
	}
	close(c.exit) // stops our polling loop
	select {
	case <-c.doneExit: // waits for polling loop to be stopped
	case <-ctx.Done(): // or exit if context canceled or timed out
	}
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

// Reads metadata about the file on disk, then will read it into memory.
// If the hash of the contents is different, we will re-parse the contents.
// We can easily add layers of sophistication to this approach, such as
// using fsnotify to watch file system change events, or by polling file metadata.
func (c *Data) loop(ctx context.Context) {
	var contentsHash string
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
		currentFileHash, err := c.poll(ctx, contentsHash, c.Config.FilePath)
		if err != nil {
			c.log.Error("polling for file %q failed: %+v", c.Config.FilePath, err)
		}
		contentsHash = currentFileHash
		timer.Reset(c.Config.interval)
	}
	// stop and drain the timer
	if !timer.Stop() && len(timer.C) > 0 {
		<-timer.C
	}
	close(c.doneExit)
}

func (c *Data) poll(ctx context.Context, contentsHash string, filePath string) (string, error) {
	var fileSize int64
	buffer := fileBufferPool.Get().(*bytes.Buffer)
	defer fileBufferPool.Put(buffer)
	buffer.Reset()

	// Attempt to stat file and preallocate buffer based on file size.
	if info, err := getFileInfo(filePath); err == nil {
		fileSize = info.Size()
		if buffer.Cap() < int(fileSize) {
			buffer.Grow(int(fileSize) - buffer.Cap())
		}
	} else {
		return "", fmt.Errorf("could not stat file %q: %w", filePath, err)
	}

	// Read file contents info buffer.
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file %q: %w", filePath, err)
	}
	defer f.Close()

	if bytesRead, err := io.Copy(buffer, f); err != nil {
		return "", fmt.Errorf("could not read file %q: %w", filePath, err)
	} else if bytesRead < fileSize {
		return "", fmt.Errorf("could not read entire file %q: %w", filePath, err)
	}

	// Generate SHA256 hash of the file contents.
	checksum := sha256.Sum256(buffer.Bytes())

	// If no change, exit early.
	if contentsHash == string(checksum[:]) {
		return contentsHash, nil
	}

	// Otherwise, parse the file contents, transform, then ingest.
	data, err := unmarshalContents(buffer.Bytes(), c.Config.fileType)
	if err != nil {
		return "", fmt.Errorf("could not parse file contents: %w", err)
	}
	if err := c.Ingest(ctx, c.Path(), data); err != nil {
		return "", fmt.Errorf("plugin %s at %s: %w", c.Name(), c.Config.path, err)
	}

	// Return new hash.
	return string(checksum[:]), nil
}

// Uses filetype to drive which parser to use.
func unmarshalContents(raw []byte, filetype string) (any, error) {
	var (
		data any
		err  error
	)
	switch filetype {
	case "xml":
		data, err = utils.ParseXML(bytes.NewReader(raw))
	case "yaml", "json":
		err = util.Unmarshal(raw, &data)
	default:
		err = fmt.Errorf("unsupported file type %q", filetype)
	}
	return data, err
}

func getFileInfo(path string) (fs.FileInfo, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("error getting absolute path: %v", err)
	}

	// Create a filesystem rooted at the parent directory.
	parentDir := filepath.Dir(absPath)
	fsys := os.DirFS(parentDir)

	// Get the base name of the file.
	baseName := filepath.Base(absPath)

	// Use fs.Stat to get file information.
	fileInfo, err := fs.Stat(fsys, baseName)
	if err != nil {
		return nil, fmt.Errorf("error getting file info: %v", err)
	}
	return fileInfo, nil
}
