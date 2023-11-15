package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/styrainc/enterprise-opa-private/pkg/login"
)

// dasURL both defines the key used in the styra.yaml and the CLI parameter
// used to override the config (or define it if there is no config)
const dasURL = "url"

func loginCmd(config *viper.Viper) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "login",
		Hidden: os.Getenv("EOPA_LOGIN") == "",
		Short:  "Sign-in to DAS instance",
		PreRunE: func(c *cobra.Command, _ []string) error {
			c.SilenceUsage = true
			if err := config.ReadInConfig(); err != nil {
				_, ok := err.(viper.ConfigFileNotFoundError)
				if !ok {
					return fmt.Errorf("config file %s: %w", config.ConfigFileUsed(), err)
				}
				// TOD(sr): debug log about not having found a config
			}
			// TOD(sr): debug log about the config file used
			return nil
		},
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(c.Context())
			defer cancel()

			url := config.GetString(dasURL)
			if url == "" {
				return fmt.Errorf("URL not provided: use .styra.yaml or pass `--url`")
			}
			return login.Start(ctx, url)
		},
	}

	addDASFlags(config, cmd)
	return cmd
}

func addDASFlags(cfg *viper.Viper, c *cobra.Command) {
	c.Flags().StringP(dasURL, "", "", `DAS address to connect to (e.g. "https://my-tenant.styra.com")`)
	cfg.BindPFlag(dasURL, c.Flags().Lookup(dasURL))
}
