package lia

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"
)

type Recorder interface {
	Record(context.Context) (Report, error)
	Output(context.Context, Report) error
}

type rec struct {
	addr       string
	rate       float64
	dur        time.Duration
	equals     bool
	bundlePath string
	sink       string
	format     format
	reportOpts []ReportOption

	tlsCACert, tlsCert, tlsKey string
	tlsSkip                    bool
}

type Option func(*rec)

func WithReport(rs ...ReportOption) Option {
	return func(r *rec) {
		r.reportOpts = rs
	}
}

func Addr(s string) Option {
	return func(r *rec) {
		r.addr = s
	}
}

func TLS(caCert, cert, key string, skip bool) Option {
	return func(r *rec) {
		r.tlsSkip = skip
		r.tlsCACert = caCert
		r.tlsCert = cert
		r.tlsKey = key
	}
}

func Rate(s float64) Option {
	return func(r *rec) {
		r.rate = s
	}
}

func Duration(d time.Duration) Option {
	return func(r *rec) {
		r.dur = d
	}
}

func Equals(b bool) Option {
	return func(r *rec) {
		r.equals = b
	}
}

func BundlePath(p string) Option {
	return func(r *rec) {
		r.bundlePath = p
	}
}

func Output(sink, fmt string) Option {
	return func(r *rec) {
		r.sink = sink
		switch fmt {
		case "json":
			r.format = json
		case "csv":
			r.format = csv
		case "pretty":
			r.format = pretty
		default:
			r.format = format(fmt) // this will fail; TODO(sr): improve options flow?
		}
	}
}

func New(opts ...Option) Recorder {
	r := rec{}
	for _, o := range opts {
		o(&r)
	}
	return &r
}

func (r *rec) Record(ctx context.Context) (Report, error) {
	switch r.format {
	case json, csv, pretty: // OK
	case "":
		r.format = pretty
	default:
		return nil, fmt.Errorf(`invalid format: %q (must be "json", "csv" or "pretty")`, r.format)
	}

	u, err := url.Parse(r.addr)
	if err != nil {
		return nil, fmt.Errorf("parse addr: %w", err)
	}

	bndl, err := os.Open(r.bundlePath)
	if err != nil {
		return nil, fmt.Errorf("open bundle: %w", err)
	}
	defer bndl.Close()

	return r.record(ctx, u, bndl)
}

func (r *rec) Output(ctx context.Context, rep Report) error {
	var output io.WriteCloser
	var err error
	if r.sink == "-" {
		output = os.Stdout
	} else {
		output, err = os.Create(r.sink)
	}
	if err != nil {
		return err
	}
	defer output.Close()
	return rep.Output(ctx, output, r.format)
}

func (r *rec) record(ctx context.Context, u *url.URL, bundle io.Reader) (Report, error) {
	raw, err := r.httpRequest(ctx, u, bundle)
	if err != nil {
		return nil, err
	}
	// NOTE(sr): We never close raw. It's a short-lived program.
	return ReportFromReader(ctx, raw, r.reportOpts...)
}
