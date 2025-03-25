package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/util"

	"github.com/styrainc/enterprise-opa-private/internal/version"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/transform"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/types"
	"github.com/styrainc/enterprise-opa-private/pkg/plugins/data/utils"
)

const (
	Name          = "http"
	acceptedTypes = "application/json, text/vnd.yaml, application/yaml, application/x-yaml, text/x-yaml, text/yaml, text/plain, text/xml, application/xml"
)

// Data plugin
type Data struct {
	manager        *plugins.Manager
	log            logging.Logger
	Config         Config
	exit, doneExit chan struct{}

	*transform.Rego
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
	go c.loop(ctx) // Q: Does this context ever stop?
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

func (c *Data) loop(ctx context.Context) {
	client := http.Client{
		Timeout: c.Config.timeout,
	}
	if c.Config.tls != nil {
		client.Transport = &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: c.Config.tls,
		}
	}
	// the go follows redirects by default, so return the error only if the FollowRedirects equals to false
	if c.Config.FollowRedirects != nil && !*c.Config.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	var eTag string
	timer := time.NewTimer(0) // zero timer is needed to execute immediately for first time
	var r io.ReadSeeker
	if len(c.Config.body) > 0 {
		r = bytes.NewReader(c.Config.body)
	}

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case <-c.exit:
			break LOOP
		case <-timer.C:
		}
		v, err := c.poll(ctx, r, eTag, client)
		if err != nil {
			c.log.Error("polling for url %q failed: %+v", c.Config.URL, err)
		}
		if v != "" {
			eTag = v
		}
		timer.Reset(c.Config.interval)
	}
	// stop and drain the timer
	if !timer.Stop() && len(timer.C) > 0 {
		<-timer.C
	}
	client.CloseIdleConnections()
	close(c.doneExit)
}

func (c *Data) poll(ctx context.Context, body io.ReadSeeker, eTag string, client http.Client) (string, error) {
	if body != nil {
		body.Seek(0, io.SeekStart) // ignore error since we always seek to the start of the buffer
	}

	req, err := http.NewRequestWithContext(ctx, c.Config.method, c.Config.url.String(), body)
	if err != nil {
		// should never be reached because the url is checked during the validation
		return "", fmt.Errorf("cannot create request: %w", err)
	}
	if c.Config.headers != nil {
		req.Header = c.Config.headers.Clone()
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", acceptedTypes)
	}
	req.Header.Set("User-Agent", fmt.Sprintf("%s (http plugin)", version.UserAgent()))
	if eTag != "" {
		req.Header.Set("If-None-Match", eTag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	// TODO(sr): When this data module is triggered -- i.e. when the rego_transform code has changed --
	// we might still want to re-fetch, i.e. invalidate the etag.
	if resp.StatusCode == http.StatusNotModified {
		c.log.Debug("not modified, etag: s", eTag)
		return "", nil
	}

	if resp.StatusCode >= 400 {
		if resp.Body != nil {
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return "", fmt.Errorf("cannot read response body: %w", err)
			}
			resp.Body.Close()
			if len(data) > 0 {
				return "", fmt.Errorf("request failed with status %q and response: %s", resp.Status, string(data))
			}
		}
		return "", fmt.Errorf("request failed with status %q", resp.Status)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("cannot read response body: %w", err)
	}
	resp.Body.Close()

	data, err := unmarshalUnknown(raw)
	if err != nil {
		c.log.Debug("response: %s", string(raw))
		return "", fmt.Errorf("cannot decode response: %w", err)

	}
	if err := c.Ingest(ctx, c.Path(), data); err != nil {
		return "", fmt.Errorf("plugin %s at %s: %w", c.Name(), c.Config.path, err)
	}

	// override eTag only if everything ok with the current response
	return resp.Header.Get("ETag"), nil
}

func unmarshalUnknown(raw []byte) (any, error) {
	var (
		data any
		err  error
	)
	if data, err = utils.ParseXML(bytes.NewReader(raw)); err == nil {
		return data, nil
	}
	if err := util.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}
