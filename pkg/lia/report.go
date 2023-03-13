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

	Count(context.Context) int

	fmt.Stringer
}

type report struct {
	db      *sql.DB
	limit   int
	grouped bool
	output  *query.Output
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
	out, err := r.queryOutput(ctx)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, out)
	return err
}

func (r *report) Count(ctx context.Context) int {
	defer r.reset()
	row := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM STDIN`)
	var c int
	_ = row.Scan(&c)
	return c
}

func (r *report) queryOutput(ctx context.Context) (io.Reader, error) {
	defer r.reset()
	var query string
	switch {
	case r.grouped:
		query = `SELECT
			path,
			input,
			count(*) AS n,
			AVG(eval_ns_a) AS mean_primary_ns,
			MEDIAN(eval_ns_a) AS median_primary_ns,
			MIN(eval_ns_a) AS min_primary_ns,
			MAX(eval_ns_a) AS max_primary_ns,
			STDEV(eval_ns_a) AS stddev_primary_ns,
			VAR(eval_ns_a) AS var_primary_ns,
			AVG(eval_ns_b) AS mean_secondary_ns,
			MEDIAN(eval_ns_b) AS median_secondary_ns,
			MIN(eval_ns_b) AS min_secondary_ns,
			MAX(eval_ns_b) AS max_secondary_ns,
			STDEV(eval_ns_b) AS stddev_secondary_ns,
			VAR(eval_ns_b) AS var_secondary_ns
		FROM STDIN GROUP BY path, input ORDER BY n DESC`
		if r.limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, r.limit)
		}
	default:
		query = `SELECT * FROM STDIN`
		if r.limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, r.limit)
		}
	}
	if _, err := r.db.ExecContext(ctx, query); err != nil {
		return nil, err
	}
	return &r.output.Buffer, nil
}

func (r *report) reset() {
	r.output = query.NewOutput()
	csvq.SetOutFile(r.output)
}

type ReportOption func(*report)

func Limit(n int) ReportOption {
	return func(r *report) {
		r.limit = n
	}
}

func Grouped(b bool) ReportOption {
	return func(r *report) {
		r.grouped = b
	}
}

// ReportFromReader reads JSON lines from a reader, and closes it when done if it is
// an io.ReadCloser (such as resp.Body).
func ReportFromReader(ctx context.Context, r io.Reader, opts ...ReportOption) (Report, error) {
	db, err := sql.Open("csvq", "")
	if err != nil {
		return nil, err
	}
	rep := report{db: db}
	for _, o := range opts {
		o(&rep)
	}
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
