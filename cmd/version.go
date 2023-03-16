package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-policy-agent/opa/version"
)

func initVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: `Print the version of Load`,
		Long:  `Show version and build information for Load.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			return generateCmdOutput(os.Stdout)
		},
	}
}

func generateCmdOutput(out io.Writer) error {
	fmt.Fprintln(out, "Version: "+version.Version)
	fmt.Fprintln(out, "OPA Version: "+version.AltVersion)
	fmt.Fprintln(out, "Build Timestamp: "+version.Timestamp)
	fmt.Fprintln(out, "Platform: "+version.Platform)
	return nil
}
