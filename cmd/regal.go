// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	opa_cmd "github.com/open-policy-agent/opa/cmd"
	regal_cmd "github.com/styrainc/regal/cmd"
)

func regal() *cobra.Command {
	for _, rc := range regal_cmd.RootCommand.Commands() {
		if rc.Name() == "lint" {
			prev := rc.RunE
			rc.RunE = func(c *cobra.Command, args []string) error {
				if err := prev(c, args); err != nil {
					code := 1
					if e := (regal_cmd.ExitError{}); errors.As(err, &e) {
						code = e.Code()
					}
					return &opa_cmd.ExitError{Exit: code}
				}
				return nil
			}
			return setDefaults(rc)
		}
	}
	panic("unreachable")
}
