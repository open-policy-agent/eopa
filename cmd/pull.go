package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/open-policy-agent/opa/logging"

	"github.com/styrainc/enterprise-opa-private/pkg/pull"
)

// targetDir says where to pull libraries to
const targetDir = "libraries"             // that's the flag, --libraries, and the config key
const targetDirDefault = ".styra/include" // that's the folder, libraries/

// force makes the pull command write to libraries even when it exists
const force = "force"

func pullCmd(config *viper.Viper, paths []string) *cobra.Command {
	var logger logging.Logger
	var err error

	cmd := &cobra.Command{
		Use: "pull",
		Example: `
Download all DAS libraries  using settings from .styra.yaml:

    eopa pull

Note: 'eopa pull' will look for .styra.yaml in the current directory,
the repository root, and your home directory. To use a different config
file location, pass --styra-config:

    eopa pull --styra-config ~/.styra-primary.yaml

Write all libraries to to libs/, with debug logging enabled:

    eopa pull --libraries libs --log-level debug

Ignore existing target directory:

    eopa pull --force
`,
		Hidden: os.Getenv("EOPA_PULL") == "",
		Short:  "Pull libraries from DAS instance",
		PreRunE: func(c *cobra.Command, _ []string) error {
			bindDASFlags(config, c)
			c.SilenceUsage = true

			lvl, _ := c.Flags().GetString(logLevel)
			format, _ := c.Flags().GetString("log-format")
			logger, err = getLogger(lvl, format, "")
			if err != nil {
				return err
			}

			path, _ := c.Flags().GetString(styraConfig)
			return readConfig(path, config, paths, logger)
		},
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()

			u0 := config.GetString(dasURL)
			if u0 == "" {
				return fmt.Errorf("URL not provided: use .styra.yaml or pass `--url`")
			}
			u, err := url.Parse(u0)
			if err != nil {
				return fmt.Errorf("invalid URL %s: %w", u0, err)
			}
			if u.Scheme != "https" && u.Scheme != "http" {
				return fmt.Errorf("invalid URL %s: scheme must be http[s]", u0)
			}

			f, _ := c.Flags().GetString(secretFile)
			sf := sessionFile(f != "", config)

			t, _ := c.Flags().GetString(targetDir)
			st := toAbs(t != "", config, targetDir, targetDirDefault)

			return pull.Start(ctx,
				pull.SessionFile(sf),
				pull.URL(u),
				pull.Logger(logger),
				pull.TargetDir(st),
				pull.Force(config.GetBool(force)),
			)
		},
	}

	addDASFlags(cmd)

	cmd.Flags().String(targetDir, targetDirDefault, "where to copy libraries to")
	config.BindPFlag(targetDir, cmd.Flags().Lookup(targetDir))
	cmd.Flags().BoolP(force, "f", false, "ignore if libraries folder exists, overwrite existing content on conflict")
	config.BindPFlag(force, cmd.Flags().Lookup(force))
	return cmd
}
