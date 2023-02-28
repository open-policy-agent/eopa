package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

func showExp(online bool, expiry time.Time) {
	var prefix string
	if online {
		prefix = "Online"
	} else {
		prefix = "Offline"
	}
	d := time.Until(expiry).Truncate(time.Second)
	if d > 3*24*time.Hour { // > 3 days
		fmt.Printf("%s license expires in %.2fd\n", prefix, float64(d)/float64(24*time.Hour))
	} else {
		fmt.Printf("%s license expires in %v\n", prefix, d)
	}
}

func LicenseCmd(license *License, key *string, token *string) *cobra.Command {
	return &cobra.Command{
		Use:   "license",
		Short: "License status",
		RunE: func(c *cobra.Command, args []string) error {
			c.SilenceErrors = true
			c.SilenceUsage = true

			license.logger.SetOutput(io.Discard) // suppress ValidateLicense logging messages

			fmt.Printf("Validating license...\n")

			var err error
			license.ValidateLicense(*key, *token, func(code int, lerr error) { err = lerr })
			if err != nil {
				fmt.Println(err)
				return err
			}
			showExp(license.license != nil, license.expiry)

			if license.license != nil { // online - lookup license policy and count
				p, err := license.Policy()
				if err != nil {
					fmt.Println(err)
					return err
				}
				fmt.Printf("Max machines: %d, current machine count: %d\n", p.Data.Attributes.MaxMachines, p.Data.RelationShips.Machines.Meta.Count)
			}
			return nil
		},
	}
}
