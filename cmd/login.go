package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/open-policy-agent/opa/logging"

	"github.com/styrainc/enterprise-opa-private/pkg/login"
)

// styraConfig overrides the default search locations, and has the command
// use the provided file as-is. If it does not exist, it's an error.
const styraConfig = "styra-config"

// dasURL both defines the key used in the styra.yaml and the CLI parameter
// used to override the config (or define it if there is no config)
const dasURL = "url"

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

// secretFile determines where the secret gathered in the login flow is
// written to.
// TODO(sr): currently this is in the CWD, it should probably be in $HOME
// or next to the .styra.yaml config file that was used.
const secretFile = ".styra-session"

const callbackErrorMsg = `Failed to receive token callback.
	
Please copy the token manually and paste it into ` + "`eopa login --read-token`."

func loginCmd(config *viper.Viper, paths []string) *cobra.Command {
	for _, p := range paths {
		config.AddConfigPath(p)
	}
	var logger logging.Logger
	var err error

	cmd := &cobra.Command{
		Use:    "login",
		Hidden: os.Getenv("EOPA_LOGIN") == "",
		Short:  "Sign-in to DAS instance",
		PreRunE: func(c *cobra.Command, _ []string) error {
			c.SilenceUsage = true
			lvl, _ := c.Flags().GetString(logLevel)
			format, _ := c.Flags().GetString("log-format")
			logger, err = getLogger(lvl, format, "")
			if err != nil {
				return err
			}

			if p, _ := c.Flags().GetString(styraConfig); p != "" {
				config.SetConfigFile(p)
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
		},
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()

			if config.GetBool(readToken) {
				bs, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err

				}
				if err := storeSecret(string(bs)); err != nil {
					return err
				}

				logger.Info("Successfully stored token")
				return nil
			}

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

			secret, err := login.Start(ctx,
				login.URL(u),
				login.Browser(!config.GetBool(noOpen)),
				login.Timeout(config.GetDuration(timeout)),
				login.Logger(logger),
			)
			if err != nil {
				logger.Error(callbackErrorMsg)
				return err
			}
			if err := storeSecret(secret); err != nil {
				return err
			}

			// TODO(sr): add user name to response payload
			logger.Info("Successfully logged in")
			return nil
		},
	}

	addDASFlags(config, cmd)
	return cmd
}

func addDASFlags(cfg *viper.Viper, c *cobra.Command) {
	c.Flags().String(dasURL, "", `DAS address to connect to (e.g. "https://my-tenant.styra.com")`)
	cfg.BindPFlag(dasURL, c.Flags().Lookup(dasURL))
	c.Flags().String(styraConfig, "", `Styra DAS config file to use`)
	c.Flags().Bool(noOpen, false, "do not attempt to open a browser window")
	cfg.BindPFlag(noOpen, c.Flags().Lookup(noOpen))
	c.Flags().Duration(timeout, time.Minute, "timeout waiting for a browser callback event")
	cfg.BindPFlag(timeout, c.Flags().Lookup(timeout))
	c.Flags().Bool(readToken, false, "read token from stdin")
	cfg.BindPFlag(readToken, c.Flags().Lookup(readToken))
	c.Flags().String(logLevel, "info", "log level")
	c.Flags().String(logFormat, "text", "log format")
}

func storeSecret(s string) error {
	return os.WriteFile(secretFile, []byte(s), 0o700)
}
