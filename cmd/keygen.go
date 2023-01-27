package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/keygen-sh/keygen-go/v2"
)

const loadLicenseToken = "STYRA_LOAD_LICENSE_TOKEN"
const loadLicenseKey = "STYRA_LOAD_LICENSE_KEY"

type (
	sLicense struct {
		mutex       sync.Mutex
		license     *keygen.License
		released    bool
		fingerprint string
	}

	keygenLogger struct {
		level keygen.LogLevel
	}
)

func newLicense() *sLicense {
	return &sLicense{}
}

func (l *keygenLogger) Errorf(format string, v ...interface{}) {
	if l.level < keygen.LogLevelError {
		return
	}
	fmt.Fprintf(os.Stderr, "[ERROR] "+format+"\n", v...)
}

func (l *keygenLogger) Warnf(format string, v ...interface{}) {
	if l.level < keygen.LogLevelWarn {
		return
	}

	fmt.Fprintf(os.Stderr, "[WARN] "+format+"\n", v...)
}

func (l *keygenLogger) Infof(format string, v ...interface{}) {
	if l.level < keygen.LogLevelInfo {
		return
	}

	fmt.Fprintf(os.Stdout, "[INFO] "+format+"\n", v...)
}

func (l *keygenLogger) Debugf(format string, v ...interface{}) {
	if l.level < keygen.LogLevelDebug {
		return
	}

	fmt.Fprintf(os.Stdout, "[DEBUG] "+format+"\n", v...)
}

// validate and activate the keygen license
func (l *sLicense) validateLicense() error {
	keygen.Account = "dd0105d1-9564-4f58-ae1c-9defdd0bfea7" // account=styra-com
	keygen.Product = "f7da4ae5-7bf5-46f6-9634-026bec5e8599" // product=load
	keygen.Logger = &keygenLogger{level: keygen.LogLevelNone}

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

		// validate after activation: detect expired licenses
		l.license, err = keygen.Validate(l.fingerprint)
		if err != nil {
			return fmt.Errorf("license validate failed: %w", err)
		}

		return nil

	case err != nil:
		return fmt.Errorf("invalid license: %w", err)
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
