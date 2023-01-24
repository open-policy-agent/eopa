package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"

	loadCmd "github.com/styrainc/load/cmd"
	_ "github.com/styrainc/load/pkg/rego_vm"

	"github.com/google/uuid"
	"github.com/keygen-sh/keygen-go/v2"
	"github.com/open-policy-agent/opa/cmd"
	"github.com/spf13/cobra"
)

const loadLicenseToken = "STYRA_LOAD_LICENSE_TOKEN"
const loadLicenseKey = "STYRA_LOAD_LICENSE_KEY"

type sLicense struct {
	mutex       sync.Mutex
	released    bool
	license     *keygen.License
	fingerprint string
}

// validate and activate the keygen license
func (l *sLicense) validateLicense() error {
	keygen.Account = "dd0105d1-9564-4f58-ae1c-9defdd0bfea7" // account=styra-com
	keygen.Product = "f7da4ae5-7bf5-46f6-9634-026bec5e8599" // product=load

	// validate licensekey or licensetoken
	keygen.LicenseKey = os.Getenv(loadLicenseKey)
	if keygen.LicenseKey == "" {
		keygen.Token = os.Getenv(loadLicenseToken) // activation-token of a license; determines the policy
		if keygen.Token == "" {
			return fmt.Errorf("missing license environment variable: %v or %v", loadLicenseKey, loadLicenseToken)
		}
	}
	os.Unsetenv(loadLicenseToken) // remove token from environment! (opa.runtime.env)

	// use random fingerprint: floating concurrent license
	l.fingerprint = uuid.New().String()

	// Validate the license for the current fingerprint
	var err error
	l.license, err = keygen.Validate(l.fingerprint)
	switch {
	case err == keygen.ErrLicenseNotActivated:
		// Activate the current fingerprint
		machine, err := l.license.Activate(l.fingerprint)
		if err != nil {
			return fmt.Errorf("license activation failed: %w", err)
		}

		// Handle SIGINT and gracefully deactivate the machine
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGSEGV) // disable default os.Interrupt handler

		go func() {
			for range sigs {
				l.releaseLicense()
				os.Exit(1) // exit now (default behavior)!
			}
		}()

		// Start a heartbeat monitor for the current machine
		if err := machine.Monitor(); err != nil {
			return fmt.Errorf("license heartbeat monitor failed to start: %w", err)
		}
		return nil

	case err != nil:
		return fmt.Errorf("invalid license: %v", err)
	}

	return nil
}

func (l *sLicense) releaseLicense() {
	if l == nil {
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.released {
		return
	}
	if l.license != nil {
		l.license.Deactivate(l.fingerprint)
	}
	l.released = true
}

func main() {
	// run all deferred functions before os.Exit
	var exit int
	defer func() {
		if exit != 0 {
			os.Exit(exit)
		}
	}() // orderly shutdown, run all defer routines

	l := new(sLicense)
	err := l.validateLicense()
	if err != nil {
		fmt.Println(err.Error())
		exit = 2
		return
	}
	defer l.releaseLicense()

	root := &cobra.Command{
		Use:   path.Base(os.Args[0]),
		Short: "Styra Load",
	}
	for _, c := range cmd.RootCommand.Commands() {
		switch c.Name() {
		case "run":
			root.AddCommand(loadCmd.Run(c))
		default:
			root.AddCommand(c)
		}
	}
	load := loadCmd.Load()
	load.AddCommand(loadCmd.Convert())
	load.AddCommand(loadCmd.Dump())

	root.AddCommand(load)

	if err := root.Execute(); err != nil {
		var e *cmd.ExitError
		if errors.As(err, &e) {
			exit = e.Exit
		} else {
			exit = 1
		}
		return
	}
}
