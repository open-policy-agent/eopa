// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	opa_version "github.com/open-policy-agent/opa/v1/version"
	"github.com/spf13/cobra"

	"github.com/open-policy-agent/eopa/internal/version"
)

func initVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: `Print the version of EOPA`,
		Long:  `Show version and build information for EOPA.`,
		Run: func(*cobra.Command, []string) {
			generateCmdOutput(os.Stdout)
		},
	}
}

func generateCmdOutput(out io.Writer) {
	fmt.Fprintln(out, "Version: "+version.Version)
	fmt.Fprintln(out, "OPA Version: "+opa_version.Version)
	fmt.Fprintln(out, "Regal Version: "+regalVersion())
	fmt.Fprintln(out, "Build Timestamp: "+opa_version.Timestamp)
	fmt.Fprintln(out, "Platform: "+opa_version.Platform)
}

func regalVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, x := range bi.Deps {
		if x.Path == "github.com/styrainc/regal" {
			return strings.TrimLeft(x.Version, "v")
		}
	}
	return ""
}
