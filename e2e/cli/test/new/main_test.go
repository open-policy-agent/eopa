// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package new

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/open-policy-agent/eopa/e2e/cli/test/new/testdata"
	"github.com/open-policy-agent/eopa/e2e/utils"
)

func TestNew(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:   utils.ExplodeEmbed(t, testdata.FS),
		Setup: utils.IncludeLicenseEnvVars,
	})
}
