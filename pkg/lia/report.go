package lia

import (
	"context"
	"database/sql"
	"fmt"
	"io"

	"github.com/mithrandie/csvq-driver"
	_ "github.com/mithrandie/csvq-driver" // import driver
	"github.com/mithrandie/csvq/lib/query"
)

type Report interface {
	Output(context.Context, io.Writer, format) error
	ToCSV(context.Context, io.Writer) error
	ToJSON(context.Context, io.Writer) error
	ToPretty(context.Context, io.Writer) error

	fmt.Stringer
}

type report struct {
	db     *sql.DB
	output *query.Output
}

func (r *report) String() string {
	return "<db report>"
}

type format string

const (
	pretty format = "BOX"
	csv    format = "CSV"
	json   format = "JSON"
)

func (r *report) ToCSV(ctx context.Context, w io.Writer) error {
	return r.Output(ctx, w, csv)
}

func (r *report) ToJSON(ctx context.Context, w io.Writer) error {
	return r.Output(ctx, w, json)
}

func (r *report) ToPretty(ctx context.Context, w io.Writer) error {
	return r.Output(ctx, w, pretty)
}

func (r *report) Output(ctx context.Context, w io.Writer, fmt format) error {
	if _, err := r.db.ExecContext(ctx, `SET @@FORMAT TO `+string(fmt)); err != nil {
		return err
	}
	out, err := r.queryOutput(ctx, `SELECT * FROM stdin`)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, out)
	return err
}

func (r *report) queryOutput(ctx context.Context, q string) (io.Reader, error) {
	defer r.reset()
	if _, err := r.db.ExecContext(ctx, q); err != nil {
		return nil, err
	}
	return &r.output.Buffer, nil
}

func (r *report) reset() {
	r.output = query.NewOutput()
	csvq.SetOutFile(r.output)
}

// ReportFromReader reads JSON lines from a reader, and closes it when done if it is
// an io.ReadCloser (such as resp.Body).
func ReportFromReader(ctx context.Context, r io.Reader) (Report, error) {
	db, err := sql.Open("csvq", "")
	if err != nil {
		return nil, err
	}
	rep := report{db: db}
	rep.reset()
	stdin := query.NewInput(r)

	if err := csvq.SetStdinContext(ctx, stdin); err != nil {
		return nil, err
	}
	setup := `SET @@IMPORT_FORMAT TO JSONL; SET @@FORMAT TO JSON; SET @@PRETTY_PRINT TO FALSE;`
	if _, err := rep.db.ExecContext(ctx, setup); err != nil {
		return nil, err
	}
	return &rep, nil
}

func (r *report) Close() error {
	return r.db.Close()
}
