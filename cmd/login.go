package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

// secretFile defines where to put the secret and where to read it from.
// It'll be parsed relative to the config file it was used in; or relative
// to the CWD if used via a parameter.
const secretFile = "secret-file"
const defaultSessionFile = ".styra-session"

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

const callbackErrorMsg = `Failed to receive token callback.
	
Please copy the token manually and paste it into ` + "`eopa login --read-token`."

func loginCmd(config *viper.Viper, paths []string) *cobra.Command {
	var logger logging.Logger
	var err error

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
		Hidden: os.Getenv("EOPA_LOGIN") == "",
		Short:  "Sign-in to DAS instance",
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

			f, _ := c.Flags().GetString(secretFile)
			sf := sessionFile(f != "", config)

			if read, _ := c.Flags().GetBool(readToken); read {
				bs, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err

				}
				if err := storeSecret(sf, string(bs)); err != nil {
					return err
				}

				logger.Debug("Using session file %s", sf)
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
			u.Path = trimURL(u.Path)
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

			logger.Debug("Using session file %s", sf)
			if err := storeSecret(sf, secret); err != nil {
				return err
			}

			// TODO(sr): add user name to response payload
			logger.Info("Successfully logged in")
			return nil
		},
		PostRunE: func(*cobra.Command, []string) error {
			// NOTE(sr): if the config file location was passed via
			// --styra-config, then it has to exist -- that's checked
			// before. Hence it cannot be generated, either.

			if config.ConfigFileUsed() != "" { // config exists, don't do anything
				return nil
			}
			return writeConfig(config, logger)
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
}

func bindDASFlags(cfg *viper.Viper, c *cobra.Command) {
	cfg.BindPFlag(dasURL, c.Flags().Lookup(dasURL))
	cfg.BindPFlag(secretFile, c.Flags().Lookup(secretFile))
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

func writeConfig(config *viper.Viper, logger logging.Logger) error {
	path := ".styra.yaml"
	err := os.WriteFile(path, generatedConfig(config), 0o600)
	if err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}
	logger.Debug("Wrote config to %s", path)
	return nil
}

const configTempl = "# generated by `eopa login`" + `
%[1]s: "%[2]s"
`

func generatedConfig(config *viper.Viper) []byte {
	return []byte(fmt.Sprintf(configTempl, dasURL, trimURL(config.GetString(dasURL))))
}

func storeSecret(sessionFile, s string) error {
	if err := os.Chmod(sessionFile, 0o700); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(sessionFile, []byte(s), 0o700)
}

func sessionFile(flag bool, config *viper.Viper) string {
	return toAbs(flag, config, secretFile, defaultSessionFile)
}

func toAbs(flag bool, config *viper.Viper, name, def string) string {
	file := config.GetString(name)
	if file == "" {
		file = def
	}
	if filepath.IsAbs(file) {
		return file
	}

	// relative path -- relative to what?
	if flag || config.ConfigFileUsed() == "" { // set via CLI flag, or by default => relative to cwd
		a, _ := filepath.Abs(file)
		return a
	}

	// not set via CLI flag => relative to config
	dir := filepath.Dir(config.ConfigFileUsed())
	return filepath.Join(dir, file)
}

func trimURL(u string) string {
	return strings.TrimSuffix(u, "/")
}
