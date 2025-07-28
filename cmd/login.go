package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/open-policy-agent/opa/v1/logging"
)

// styraConfig overrides the default search locations, and has the command
// use the provided file as-is. If it does not exist, it's an error.
const styraConfig = "styra-config"

// dasURL both defines the key used in the styra.yaml and the CLI parameter
// used to override the config (or define it if there is no config)
const dasURL = "url"

// secretFile defines where to put the secret and where to read it from.
// It'll be parsed relative to the config file it was used in; or relative
// to the CWD if used via a parameter.
const (
	secretFile         = "secret-file"
	defaultSessionFile = ".styra-session"
)

// noOpen is the flag to inhibit attempts to open a browser tab. The command
// will only show the URL instead.
const noOpen = "no-open"

// timeout allows overriding the time waiting for a request sent from the
// browser ui code.
const timeout = "timeout"

// readToken will flip the login cmd into receive-only mode: it'll read its
// input and write that to the secret file location. It's meant to be a simpler
// fallback option compared to TTY handling.
// Example usage is `pbpaste | eopa login --read-token`.
const readToken = "read-token"

// logLevel is used to enable debug logging for the CLI login process.
const logLevel = "log-level"

// logFormat is used to adjust the log format for the CLI login process.
const logFormat = "log-format"

func loginCmd(config *viper.Viper, _ []string) *cobra.Command {
	cmd := &cobra.Command{
		Use: "login",
		Example: `
Create a new browser session that is shared with EOPA.

Using settings from .styra.yaml:

    eopa login

Note: 'eopa login' will look for .styra.yaml in the current directory,
the repository root, and your home directory. To use a different config
file location, pass --styra-config:

    eopa login --styra-config ~/.strya-primary.yaml

You can also provide your DAS endpoint via a flag:

    eopa login --url https://my-tenant.styra.com

On successful login, a .styra.yaml file will be generated in your current
working directory.

If the automatic token transfer fails, the browser tab will show you the
token to use. Paste the token into the following command to have it stored
manually:

	eopa login --read-token
`,
		Short:      "Sign-in to DAS instance",
		Deprecated: "Command no longer used in EOPA",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}

	addDASFlags(cmd)

	// login-specific flags
	cmd.Flags().Bool(noOpen, false, "do not attempt to open a browser window")
	config.BindPFlag(noOpen, cmd.Flags().Lookup(noOpen))
	cmd.Flags().Duration(timeout, time.Minute, "timeout waiting for a browser callback event")
	config.BindPFlag(timeout, cmd.Flags().Lookup(timeout))
	cmd.Flags().Bool(readToken, false, "read token from stdin")
	return cmd
}

func addDASFlags(c *cobra.Command) {
	c.Flags().String(dasURL, "", `DAS address to connect to (e.g. "https://my-tenant.styra.com")`)
	c.Flags().String(secretFile, "", "file to store the secret in")
	c.Flags().String(styraConfig, "", `Styra DAS config file to use`)
	c.Flags().String(logLevel, "info", "log level")
	c.Flags().String(logFormat, "text", "log format")
	c.Flags().String(targetDir, targetDirDefault, "where to copy libraries to")
}

func bindDASFlags(cfg *viper.Viper, c *cobra.Command) {
	cfg.BindPFlag(dasURL, c.Flags().Lookup(dasURL))
	cfg.BindPFlag(secretFile, c.Flags().Lookup(secretFile))
	cfg.BindPFlag(targetDir, c.Flags().Lookup(targetDir))
	if f := c.Flags().Lookup(force); f != nil {
		cfg.BindPFlag(force, f)
	}
}

func readConfig(flagPath string, config *viper.Viper, paths []string, logger logging.Logger) error {
	if flagPath != "" {
		config.SetConfigFile(flagPath)
	} else {
		for _, p := range paths {
			config.AddConfigPath(p)
		}
	}

	logger.Debug("looking for config in %v", paths)
	if err := config.ReadInConfig(); err != nil {
		_, ok := err.(viper.ConfigFileNotFoundError)
		if !ok {
			return fmt.Errorf("config file %s: %w", config.ConfigFileUsed(), err)
		}
	}
	if used := config.ConfigFileUsed(); used != "" {
		logger.Debug("used config file %s", used)
	}
	return nil
}
