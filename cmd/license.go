package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/styrainc/load-private/cmd/keygen"
	"github.com/styrainc/load-private/cmd/trial"
	"github.com/styrainc/load-private/pkg/tui"
)

const defaultTrialServiceURL string = "https://load-license.corp.styra.com"

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
	c := &cobra.Command{
		Use:   "license",
		Short: "License status",
		RunE: func(c *cobra.Command, args []string) error {
			c.SilenceErrors = true
			c.SilenceUsage = true

			lvl, _ := getLevel("info")
			format := getFormatter("json")
			license.SetFormatter(format)
			license.SetLevel(lvl)

			var err error
			license.ValidateLicense(lparams, func(code int, lerr error) { err = lerr })
			if err != nil {
				fmt.Fprintf(os.Stderr, "Validation error: %v\n", err)
				return err
			}

			online := license.IsOnline()
			showExp(online, license.Expiry())

			if online { // online - lookup license policy and count
				p, err := license.Policy()
				if err != nil {
					fmt.Printf("Policy error: %v", err)
					return err
				}
				fmt.Printf("Max machines: %d, current machine count: %d\n", p.Data.Attributes.MaxMachines, p.Data.RelationShips.Machines.Meta.Count)
			}
			return nil
		},
	}

	trialServiceURL := os.Getenv("STYRA_LOAD_TRIAL_SERVICE_URL")
	if trialServiceURL == "" {
		trialServiceURL = defaultTrialServiceURL
	}
	c.AddCommand(TrialCmd(trial.NewClient(trialServiceURL)))

	return c
}

func TrialCmd(client trial.Client) *cobra.Command {
	var keyOnly bool
	input := trial.Input{}
	c := &cobra.Command{
		Use:          "trial",
		Short:        "Create a new Styra Load trial license.",
		Long:         "Gather all of the data needed to create a new Styra Load trial license and create one. Any information not provided via flags is collected interactively. Upon success, the new trial license key is printed to stdout.",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return trial.Run(trial.RunTrialArgs{
				Input:   input,
				KeyOnly: keyOnly,
				StdOut:  os.Stdout,
				Client:  client,
				RunForm: tui.TeaRunFormWithOptions(),
			})
		},
	}

	flags := c.Flags()
	flags.StringVar(&input.FirstName, "first-name", "", "first name to attach to the trial license")
	flags.StringVar(&input.LastName, "last-name", "", "last name to attach to the trial license")
	flags.StringVar(&input.Email, "email", "", "a work email address to attach to the trial license")
	flags.StringVar(&input.Company, "company", "", "the company name to attach to the trial license")
	flags.StringVar(&input.Country, "country", "", "the country to attach to the trial license")
	flags.BoolVar(&keyOnly, "key-only", false, "on success, print only the license key to stdout")

	return c
}
