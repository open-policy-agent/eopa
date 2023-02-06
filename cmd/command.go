package cmd

import (
	"os"
	"path"
	"sync"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"
)

var brand = "Load"

func LoadCommand(wg *sync.WaitGroup, license *License) *cobra.Command {
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
						license.ValidateLicense()
					}()
				}
			}
		},
	}
	opa := cmd.Command(brand)
	for _, c := range opa.Commands() {
		switch c.Name() {
		case "run":
			root.AddCommand(Run(c, brand))
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
