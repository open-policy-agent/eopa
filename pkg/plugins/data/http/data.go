package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/util"

	"github.com/styrainc/load-private/pkg"
	"github.com/styrainc/load-private/pkg/plugins/data/utils"
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
}

func (c *Data) Start(ctx context.Context) error {
	c.exit = make(chan struct{})
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		return storage.MakeDir(ctx, c.manager.Store, txn, c.Config.path)
	}); err != nil {
		return err
	}

	c.doneExit = make(chan struct{})
	go c.loop(ctx) // Q: Does this context ever stop?
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
		if v := c.poll(ctx, r, eTag, client); v != "" {
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

func (c *Data) poll(ctx context.Context, body io.ReadSeeker, eTag string, client http.Client) string {
	if body != nil {
		body.Seek(0, io.SeekStart) // ignore error since we always seek to the start of the buffer
	}

	req, err := http.NewRequestWithContext(ctx, c.Config.method, c.Config.url.String(), body)
	if err != nil {
		// should never be reached because the url is checked during the validation
		panic(fmt.Errorf("cannot create request: %w", err))
	}
	if c.Config.headers != nil {
		req.Header = c.Config.headers.Clone()
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", acceptedTypes)
	}
	req.Header.Set("User-Agent", fmt.Sprintf("%s (http plugin)", pkg.GetUserAgent()))
	if eTag != "" {
		req.Header.Set("If-None-Match", eTag)
	}
	resp, err := client.Do(req)
	if err != nil {
		c.log.Warn("request failed: %v", err)
		return ""
	}
	if resp.StatusCode == http.StatusNotModified {
		c.log.Debug("not modified, etag: s", eTag)
		return ""
	}

	if resp.StatusCode >= 400 {
		if resp.Body != nil {
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				c.log.Warn("cannot read response body: %v", err)
			}
			resp.Body.Close()
			if len(data) > 0 {
				c.log.Warn("request failed with status %q and response: %s", resp.Status, string(data))
				return ""
			}
		}
		c.log.Warn("request failed with status %q", resp.Status)
		return ""
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.log.Warn("cannot read response body: %v", err)
		return ""
	}
	resp.Body.Close()

	data, err := unmarshalUnknown(raw)
	if err != nil {
		c.log.Debug("response: %s", string(raw))
		c.log.Warn("cannot decode response: %v", err)
		return ""
	}
	if err := storage.WriteOne(ctx, c.manager.Store, storage.ReplaceOp, c.Config.path, data); err != nil {
		c.log.Error("writing data to %+v failed: %v", c.Config.path, err)
		return ""
	}

	// override eTag only if everything ok with the current response
	return resp.Header.Get("ETag")
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
