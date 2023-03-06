package lia

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

func (r *rec) httpRequest(ctx context.Context, u *url.URL, bndl io.Reader) (Report, error) {
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
	}
	return ReportFromReader(ctx, resp.Body)
}
