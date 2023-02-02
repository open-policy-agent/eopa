package cmd

import (
	"os"
	"path"
	"sync"

	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"
)

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
	for _, c := range cmd.RootCommand.Commands() {
		switch c.Name() {
		case "run":
			root.AddCommand(Run(c))
		default:
			root.AddCommand(c)
		}
	}
	load := Bundle()
	load.AddCommand(Convert())
	load.AddCommand(Dump())

	root.AddCommand(load)

	return root
}
