package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/styrainc/load-private/cmd/keygen"
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

func LicenseCmd(license *keygen.License, lparams *keygen.LicenseParams) *cobra.Command {
	return &cobra.Command{
		Use:   "license",
		Short: "License status",
		RunE: func(c *cobra.Command, args []string) error {
			c.SilenceErrors = true
			c.SilenceUsage = true

			lvl, _ := getLevel("info")
			format := getFormatter("json")
			license.SetFormatter(format)
			license.SetLevel(lvl)

			fmt.Printf("Validating license...\n")

			var err error
			license.ValidateLicense(lparams, func(code int, lerr error) { license.Logger().Error("license error: %v", lerr); err = lerr })
			if err != nil {
				fmt.Printf("license validate error: %v", err)
				return err
			}

			online := license.IsOnline()
			showExp(online, license.Expiry())

			if online { // online - lookup license policy and count
				p, err := license.Policy()
				if err != nil {
					fmt.Printf("license policy error: %v", err)
					return err
				}
				fmt.Printf("Max machines: %d, current machine count: %d\n", p.Data.Attributes.MaxMachines, p.Data.RelationShips.Machines.Meta.Count)
			}
			return nil
		},
	}
}
