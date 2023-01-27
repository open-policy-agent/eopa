package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
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
		stop        int32
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

// stopped: see if releaseLicense was called
func (l *sLicense) stopped() bool {
	return atomic.LoadInt32(&l.stop) != 0
}

// validate and activate the keygen license
func (l *sLicense) validateLicense() {
	keygen.Account = "dd0105d1-9564-4f58-ae1c-9defdd0bfea7" // account=styra-com
	keygen.Product = "f7da4ae5-7bf5-46f6-9634-026bec5e8599" // product=load
	keygen.Logger = &keygenLogger{level: keygen.LogLevelNone}

	var err error
	defer func() {
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(2)
		}
	}()

	// validate licensekey or licensetoken
	keygen.LicenseKey = os.Getenv(loadLicenseKey)
	if keygen.LicenseKey == "" {
		keygen.Token = os.Getenv(loadLicenseToken) // activation-token of a license; determines the policy
		if keygen.Token == "" {
			err = fmt.Errorf("missing license environment variable: %v or %v", loadLicenseKey, loadLicenseToken)
			return
		}
	}
	os.Unsetenv(loadLicenseToken) // remove token from environment! (opa.runtime.env)

	// use random fingerprint: floating concurrent license
	l.fingerprint = uuid.New().String()

	if l.stopped() { // if releaseLicense was called, exit now
		return
	}

	// Validate the license for the current fingerprint
	var lerr error
	l.license, lerr = keygen.Validate(l.fingerprint)
	switch {
	case lerr == keygen.ErrLicenseNotActivated:
		// Activate the current fingerprint

		if l.stopped() { // if releaseLicense was called, exit now
			return
		}

		machine, lerr := l.license.Activate(l.fingerprint)
		if lerr != nil {
			err = fmt.Errorf("license activation failed: %w", lerr)
			return
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
		if l.stopped() { // if releaseLicense was called, exit now
			return
		}

		// Start a heartbeat monitor for the current machine
		if lerr := machine.Monitor(); lerr != nil {
			err = fmt.Errorf("license heartbeat monitor failed to start: %w", lerr)
			return
		}
		return

	case lerr != nil:
		err = fmt.Errorf("invalid license: %w", lerr)
		return
	}
}

func (l *sLicense) releaseLicense() {
	if l == nil {
		return
	}
	// tell validateLicense to stop (if its still running)
	atomic.AddInt32(&l.stop, 1)

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
