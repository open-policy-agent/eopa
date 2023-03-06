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
	Record(context.Context) error
}

type rec struct {
	addr       string
	rate       float64
	dur        time.Duration
	equals     bool
	bundlePath string
	sink       string
	format     format
}

type Option func(*rec)

func Addr(s string) Option {
	return func(r *rec) {
		r.addr = s
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

func (r *rec) Record(ctx context.Context) error {
	switch r.format {
	case json, csv, pretty: // OK
	case "":
		r.format = pretty
	default:
		return fmt.Errorf(`invalid format: %q (must be "json", "csv" or "pretty")`, r.format)
	}

	u, err := url.Parse(r.addr)
	if err != nil {
		return fmt.Errorf("parse addr: %w", err)
	}

	output, err := r.output()
	if err != nil {
		return fmt.Errorf("prepare output: %w", err)
	}
	defer output.Close()

	bndl, err := os.Open(r.bundlePath)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer bndl.Close()

	return r.record(ctx, u, output, bndl)
}

func (r *rec) output() (output io.WriteCloser, err error) {
	if r.sink == "-" {
		output = os.Stdout
	} else {
		output, err = os.Open(r.sink)
	}
	return
}

func (r *rec) record(ctx context.Context, u *url.URL, output io.Writer, bundle io.Reader) error {
	report, err := r.httpRequest(ctx, u, bundle)
	if err != nil {
		return err
	}
	return report.Output(ctx, output, r.format)
}
