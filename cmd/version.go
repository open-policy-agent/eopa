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
		Short: `Print the version of Enterprise OPA`,
		Long:  `Show version and build information for Enterprise OPA.`,
		Run: func(*cobra.Command, []string) {
			generateCmdOutput(os.Stdout)
		},
	}
}

func generateCmdOutput(out io.Writer) {
	fmt.Fprintln(out, "Version: "+version.Version)
	fmt.Fprintln(out, "OPA Version: "+version.AltVersion)
	fmt.Fprintln(out, "Regal Version: "+regalVersion())
	fmt.Fprintln(out, "Build Timestamp: "+version.Timestamp)
	fmt.Fprintln(out, "Platform: "+version.Platform)
}
