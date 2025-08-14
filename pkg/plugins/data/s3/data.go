// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/storage"

	"github.com/open-policy-agent/eopa/pkg/plugins/data/transform"
	"github.com/open-policy-agent/eopa/pkg/plugins/data/types"
	"github.com/open-policy-agent/eopa/pkg/plugins/data/utils"
)

const (
	Name = "s3"

	DownloaderMaxConcurrency = 5                // Maximum # of parallel downloads per Read() call.
	DownloaderMaxPartSize    = 32 * 1024 * 1024 // 32 MB per part.
)

// Data plugin
type Data struct {
	manager        *plugins.Manager
	log            logging.Logger
	Config         Config
	hc             *http.Client
	awsConfig      *aws.Config
	s3Options      []func(*s3.Options)
	exit, doneExit chan struct{}

	*transform.Rego
}

// Ensure that this sub-plugin will be triggered by the data umbrella plugin,
// because it implements types.Triggerer.
var _ types.Triggerer = (*Data)(nil)

func (c *Data) Start(ctx context.Context) error {
	if err := c.Prepare(ctx); err != nil {
		return fmt.Errorf("prepare rego_transform: %w", err)
	}
	c.hc = &http.Client{}
	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithHTTPClient(c.hc),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.Config.AccessID, c.Config.Secret, "")),
		config.WithRegion(c.Config.region),
	)
	if err != nil {
		return err
	}

	// endpoint and ForcePath should be populated by the config logic.
	s3Options := []func(*s3.Options){}
	if c.Config.endpoint != "" {
		s3Options = append(s3Options, func(o *s3.Options) {
			o.EndpointResolverV2 = newCustomEndpointResolver(c.Config.endpoint)
		})
	}
	if c.Config.ForcePath {
		s3Options = append(s3Options, func(o *s3.Options) { o.UsePathStyle = true })
	}

	c.awsConfig = &cfg
	c.s3Options = s3Options

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
	c.hc.CloseIdleConnections()
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
			c.log.Error("polling data from s3 failed: %+v", err)
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
	svc := s3.NewFromConfig(*c.awsConfig, c.s3Options...)
	keys, err := c.getKeys(ctx, svc)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	results, err := c.process(ctx, svc, keys)
	if err != nil {
		return err
	}
	if results == nil {
		return nil
	}

	if err := c.Ingest(ctx, c.Path(), results); err != nil {
		return fmt.Errorf("plugin %s at %s: %w", c.Name(), c.Config.path, err)
	}
	return nil
}

func (c *Data) getKeys(ctx context.Context, svc *s3.Client) ([]string, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(c.Config.bucket),
		Prefix: aws.String(c.Config.filepath),
	}

	var keys []string
	paginator := s3.NewListObjectsV2Paginator(svc, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to get the list of objects: %w", err)
		}

		for _, o := range page.Contents {
			keys = append(keys, *o.Key)
		}
	}

	return keys, nil
}

func (c *Data) process(ctx context.Context, svc *s3.Client, keys []string) (any, error) {
	switch len(keys) {
	case 0:
		return nil, nil
	case 1:
		// in case when the path parameter points to a single file
		// the result should contain only the content of that file
		r, err := c.download(ctx, svc, keys[0])
		if err != nil {
			return nil, err
		}
		return utils.ParseFile(keys[0], r)
	}

	files := make(map[string]any)
	for _, key := range keys {
		r, err := c.download(ctx, svc, key)
		if err != nil {
			return nil, err
		}
		document, err := utils.ParseFile(key, r)
		if err != nil {
			return nil, err
		}
		if document == nil {
			continue
		}

		utils.InsertFile(files, strings.Split(key, "/"), document)
	}

	if len(files) == 0 {
		return nil, nil
	}
	return files, nil
}

func (c *Data) download(ctx context.Context, svc *s3.Client, key string) (io.Reader, error) {
	downloader := manager.NewDownloader(svc, func(d *manager.Downloader) {
		d.Concurrency = DownloaderMaxConcurrency
		d.PartSize = DownloaderMaxPartSize
	})

	buf := &manager.WriteAtBuffer{}
	if _, err := downloader.Download(ctx, buf, &s3.GetObjectInput{
		Bucket: aws.String(c.Config.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return nil, fmt.Errorf("unable to download data: %w", err)
	}

	return bytes.NewReader(buf.Bytes()), nil
}
