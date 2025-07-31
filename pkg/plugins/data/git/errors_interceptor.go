// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type errorsInterceptor struct {
	rt http.RoundTripper
}

var _ http.RoundTripper = &errorsInterceptor{}

func newErrorsInterceptor(parent http.RoundTripper) *errorsInterceptor {
	return &errorsInterceptor{parent}
}

func (t *errorsInterceptor) RoundTrip(req *http.Request) (ret *http.Response, rerr error) {
	resp, err := t.rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest && resp.Body != nil {
		defer func() {
			rerr = errors.Join(rerr, resp.Body.Close())
		}()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading the error response with status %q failed: %w", resp.Status, err)
		}
		data = bytes.TrimSpace(data)
		if len(data) == 0 {
			return nil, errors.New(resp.Status)
		}
		return nil, fmt.Errorf("error response with status %q and body: %s", resp.Status, string(data))
	}
	return resp, nil
}
