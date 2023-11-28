package cmd

import (
	"os"

	"github.com/spf13/cobra"

	regal_cmd "github.com/styrainc/regal/cmd"
)

func regal() *cobra.Command {
	wrapper := &cobra.Command{
		Use:    "regal",
		Hidden: os.Getenv("STYRA_REGAL") == "",
	}
	for _, rc := range regal_cmd.RootCommand.Commands() {
		wrapper.AddCommand(rc)
	}

	// 	code := 1
	// 	if e := (cmd.ExitError{}); errors.As(err, &e) {
	// 		code = e.Code()
	// 	}

	// 	os.Exit(code)
	// }
	return wrapper
}
