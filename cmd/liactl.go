package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/styrainc/load-private/pkg/lia"
)

func liaCtl() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "liactl",
		Short: "Live Impact Analysis control",
	}
	cmd.AddCommand(record())
	return cmd
}

// TODO(sr): add TLS related arguments
// flag names
const (
	addr     = "addr"
	duration = "duration"
	equals   = "equals"
	rate     = "sample-rate"
	bndl     = "bundle"
	output   = "output"
	format   = "format"
	limit    = "limit"
	group    = "group"
	failAny  = "fail-any"
)

func record() *cobra.Command {
	c := &cobra.Command{
		Use:   "record",
		Short: "Start recording",
		RunE: func(c *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()

			// Errors occurring here are not CLI arg related
			c.SilenceUsage = true

			host, _ := c.Flags().GetString(addr)
			dur, _ := c.Flags().GetDuration(duration)
			eq, _ := c.Flags().GetBool(equals)
			rate, _ := c.Flags().GetFloat64(rate)
			bndl, _ := c.Flags().GetString(bndl)
			if bndl == "" {
				return fmt.Errorf("bundle path required but unset")
			}
			out, _ := c.Flags().GetString(output)
			format, _ := c.Flags().GetString(format)

			grouped, _ := c.Flags().GetBool(group)
			limit, _ := c.Flags().GetInt(limit)
			fail, _ := c.Flags().GetBool(failAny)

			rec := lia.New(
				lia.Addr(host),
				lia.Duration(dur),
				lia.Equals(eq),
				lia.Rate(rate),
				lia.Output(out, format),
				lia.BundlePath(bndl),
				lia.WithReport(
					lia.Grouped(grouped),
					lia.Limit(limit),
				),
			)
			rep, err := rec.Record(ctx)
			if err != nil {
				return err
			}
			if fail {
				if c := rep.Count(ctx); c > 0 {
					return fmt.Errorf("expected 0 results, got %d", c)
				}
			}
			return rec.Output(ctx, rep)
		},
	}
	// Load connectivity and LIA request options
	c.Flags().StringP(addr, "a", "http://127.0.0.1:8181", `Load address to connect to (e.g. "https://staging.load.corp.com:8443")`)
	c.Flags().DurationP(duration, "d", 30*time.Second, `Live Impact Analysis duration (e.g. "5m")`)
	c.Flags().Bool(equals, false, `Include equal results (e.g. for assessing performance differences)`)
	c.Flags().Float64(rate, 0.1, "Sample rate of evaluations to include (e.g. 0.1 for 10%, or 1 for all requests)")
	c.Flags().StringP(bndl, "b", "", "Path to bundle to use for secondary evaluation")

	// report options
	c.Flags().Int(limit, 0, "Limit report to N rows (if grouped, ordered by count descending)")
	c.Flags().Bool(group, false, "Group report by path and input")
	c.Flags().Bool(failAny, false, "Fail if there's any finding (exit 1)")

	// output options
	c.Flags().StringP(output, "o", "-", `write report to file, "-" means stdout`)
	c.Flags().StringP(format, "f", "pretty", `output format: "json", "csv", or "pretty")`)
	return c
}
