package cmd

import (
	"os"
	"path"
	"sync"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"
)

var brand = "Load"

func addLicenseFlags(c *cobra.Command, key *string, token *string) {
	c.Flags().StringVar(key, "license-key", "", "Location of file containing STYRA_LOAD_LICENSE_KEY")
	c.Flags().StringVar(token, "license-token", "", "Location of file containing STYRA_LOAD_LICENSE_TOKEN")
}

func LoadCommand(wg *sync.WaitGroup, license *License) *cobra.Command {
	var key string
	var token string

	root := &cobra.Command{
		Use:   path.Base(os.Args[0]),
		Short: "Styra Load",

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			switch cmd.CalledAs() {
			case "eval", "run":
				if wg != nil && license != nil {
					wg.Add(1)
					go func() {
						// do the license validate and activate asynchronously; so user doesn't have to wait
						defer wg.Done()
						license.ValidateLicense(key, token)
					}()
				}
			}
		},
	}

	opa := cmd.Command(brand)
	for _, c := range opa.Commands() {
		switch c.Name() {
		case "run":
			addLicenseFlags(c, &key, &token)
			root.AddCommand(Run(c, brand))
		case "eval":
			addLicenseFlags(c, &key, &token)
			root.AddCommand(c)
		default:
			root.AddCommand(c)
		}
	}
	bundle := Bundle()
	bundle.AddCommand(Convert())
	bundle.AddCommand(Dump())

	root.AddCommand(bundle)
	return root
}
