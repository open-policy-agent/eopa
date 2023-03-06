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
)

func record() *cobra.Command {
	c := &cobra.Command{
		Use:   "record",
		Short: "Start recording",
		RunE: func(c *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()

			host, _ := c.Flags().GetString(addr)
			dur, _ := c.Flags().GetDuration(duration)
			eq, _ := c.Flags().GetBool(equals)
			rate, _ := c.Flags().GetFloat64(rate)
			bndl, _ := c.Flags().GetString(bndl)
			if bndl == "" {
				return fmt.Errorf("bundle path required but unset")
			}
			out, _ := c.Flags().GetString(output)
			fmt, _ := c.Flags().GetString(format)
			rec := lia.New(
				lia.Addr(host),
				lia.Duration(dur),
				lia.Equals(eq),
				lia.Rate(rate),
				lia.Output(out, fmt),
				lia.BundlePath(bndl),
			)
			return rec.Record(ctx)
		},
	}
	c.Flags().StringP(addr, "a", "http://127.0.0.1:8181", `Load address to connect to (e.g. "https://staging.load.corp.com:8443")`)
	c.Flags().DurationP(duration, "d", 30*time.Second, `Live Impact Analysis duration (e.g. "5m")`)
	c.Flags().Bool(equals, false, `Include equal results (e.g. for assessing performance differences)`)
	c.Flags().Float64(rate, 0.1, "Sample rate of evaluations to include (e.g. 0.1 for 10%, or 1 for all requests)")
	c.Flags().StringP(bndl, "b", "", "Path to bundle to use for secondary evaluation")

	c.Flags().StringP(output, "o", "-", `write report to file, "-" means stdout`)
	c.Flags().StringP(format, "f", "pretty", `output format: "json", "csv", or "pretty")`)
	return c
}
