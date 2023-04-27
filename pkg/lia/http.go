package lia

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/styrainc/load-private/pkg/plugins/data/utils"
)

func (r *rec) client() (*http.Client, error) {
	tlsConf, err := utils.ReadTLSConfig(r.tlsSkip, r.tlsCert, r.tlsKey, r.tlsCACert)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: tlsConf,
	}}, nil
}

func (r *rec) httpRequest(ctx context.Context, u *url.URL, bndl io.Reader) (io.ReadCloser, error) {
	u.Path = "v0/impact"

	req, err := http.NewRequest(http.MethodPost, u.String(), bndl)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Add("duration", r.dur.String())
	q.Add("rate", fmt.Sprint(r.rate)) // strconv.FormatFloat? or treat as string all the way?
	q.Add("equals", fmt.Sprint(r.equals))
	req.URL.RawQuery = q.Encode()
	req = req.WithContext(ctx)

	cl, err := r.client()
	if err != nil {
		return nil, err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
	}
	return resp.Body, nil
}
