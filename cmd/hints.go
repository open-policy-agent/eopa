// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	regal_hints "github.com/open-policy-agent/regal/pkg/hints"
)

const styra = `https://docs.styra.com/opa/errors/`

func extraHints(c *cobra.Command, e error) error {
	f := c.Flag("format")
	enable := f != nil && f.Value.String() == "pretty"
	if !enable || e == nil {
		return e
	}

	hints, err := regal_hints.GetForError(e)
	if err != nil { // give up
		return e
	}

	if len(hints) == 0 {
		return e
	}

	sort.Strings(hints)
	h := strings.Builder{}
	h.WriteString("For more information, see:")
	for i := range hints {
		if len(hints) > 1 {
			h.WriteRune('\n')
			h.WriteString("- ")
		} else {
			h.WriteRune(' ')
		}
		h.WriteString(styra)
		h.WriteString(hints[i])
		h.WriteRune('\n')
	}
	fmt.Fprint(os.Stderr, h.String())
	return e
}
