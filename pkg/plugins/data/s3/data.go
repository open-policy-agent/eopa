package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"

	"github.com/styrainc/load-private/pkg/plugins/data/utils"
	inmem "github.com/styrainc/load-private/pkg/storage"
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
	session        *session.Session
	exit, doneExit chan struct{}
}

type credentialsProvider struct {
	accessID, secret string
}

func (p *credentialsProvider) Retrieve() (credentials.Value, error) {
	return credentials.Value{
		AccessKeyID:     p.accessID,
		SecretAccessKey: p.secret,
	}, nil
}

func (p *credentialsProvider) IsExpired() bool {
	return false
}

func (c *Data) Start(ctx context.Context) error {
	c.hc = &http.Client{}
	cfg := aws.NewConfig().
		WithHTTPClient(c.hc).
		WithCredentials(credentials.NewCredentials(&credentialsProvider{c.Config.AccessID, c.Config.Secret})).
		WithRegion(c.Config.region)
	if c.Config.endpoint != "" {
		cfg = cfg.WithEndpoint(c.Config.endpoint)
	}
	s, err := session.NewSession(cfg)
	if err != nil {
		return err
	}
	c.session = s
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
	svc := s3.New(c.session)
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

	if err := inmem.WriteUnchecked(ctx, c.manager.Store, storage.ReplaceOp, c.Config.path, results); err != nil {
		return fmt.Errorf("writing data to %+v failed: %w", c.Config.path, err)
	}

	return nil
}

func (c *Data) getKeys(ctx context.Context, svc *s3.S3) ([]string, error) {
	input := &s3.ListObjectsInput{
		Bucket: aws.String(c.Config.bucket),
		Prefix: aws.String(c.Config.filepath),
	}

	var keys []string
	err := svc.ListObjectsPagesWithContext(ctx, input, func(page *s3.ListObjectsOutput, lastPage bool) bool {
		for _, o := range page.Contents {
			keys = append(keys, *o.Key)
		}
		return !lastPage
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of objects: %w", err)
	}
	return keys, nil
}

func (c *Data) process(ctx context.Context, svc *s3.S3, keys []string) (any, error) {
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

	files := make(map[string]interface{})
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

func (c *Data) download(ctx context.Context, svc *s3.S3, key string) (io.Reader, error) {
	downloader := s3manager.NewDownloaderWithClient(svc, func(d *s3manager.Downloader) {
		d.Concurrency = DownloaderMaxConcurrency
		d.PartSize = DownloaderMaxPartSize
	})
	buf := &aws.WriteAtBuffer{}
	if _, err := downloader.DownloadWithContext(ctx, buf, &s3.GetObjectInput{
		Bucket: aws.String(c.Config.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return nil, fmt.Errorf("unable to download data: %w", err)
	}

	return bytes.NewReader(buf.Bytes()), nil
}
