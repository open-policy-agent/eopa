package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/keygen-sh/keygen-go/v2"

	"github.com/open-policy-agent/opa/logging"
)

const loadLicenseToken = "STYRA_LOAD_LICENSE_TOKEN"
const loadLicenseKey = "STYRA_LOAD_LICENSE_KEY"

type (
	License struct {
		mutex       sync.Mutex
		license     *keygen.License
		released    bool
		stop        int32
		fingerprint string
		logger      *logging.StandardLogger
	}

	keygenLogger struct {
		level keygen.LogLevel
	}
)

func NewLicense() *License {
	// validate licensekey or licensetoken
	keygen.LicenseKey = os.Getenv(loadLicenseKey)
	if keygen.LicenseKey == "" {
		keygen.Token = os.Getenv(loadLicenseToken) // activation-token of a license; determines the policy
	}

	// remove licenses from environment! (opa.runtime.env)
	os.Unsetenv(loadLicenseKey)
	os.Unsetenv(loadLicenseToken)
	return &License{logger: logging.Get()}
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

// stopped: see if ReleaseLicense was called
func (l *License) stopped() bool {
	return atomic.LoadInt32(&l.stop) != 0
}

func readLicense(file string) (string, error) {
	dat, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("invalid license file %v: %w", file, err)
	}
	s := strings.TrimSpace(string(dat))
	if len(s) == 0 {
		return "", fmt.Errorf("invalid license file %v", file)
	}
	return s, nil
}

// validate and activate the keygen license
func (l *License) ValidateLicense(key string, token string) {
	keygen.Account = "dd0105d1-9564-4f58-ae1c-9defdd0bfea7" // account=styra-com
	keygen.Product = "f7da4ae5-7bf5-46f6-9634-026bec5e8599" // product=load
	keygen.Logger = &keygenLogger{level: keygen.LogLevelNone}

	var err error
	defer func() {
		if err != nil {
			l.logger.Error("licensing error: %v", err)
			os.Exit(2)
		}
	}()

	// validate licensekey or licensetoken
	if keygen.LicenseKey == "" && keygen.Token == "" {
		var dat string
		if key != "" {
			dat, err = readLicense(key)
			if err != nil {
				return
			}
			keygen.LicenseKey = dat
		} else if token != "" {
			dat, err = readLicense(token)
			if err != nil {
				return
			}
			keygen.Token = dat
		} else {
			err = fmt.Errorf("missing license environment variable: %v or %v", loadLicenseKey, loadLicenseToken)
			return
		}
	}

	// use random fingerprint: floating concurrent license
	l.fingerprint = uuid.New().String()

	if l.stopped() { // if ReleaseLicense was called, exit now
		return
	}

	// Validate the license for the current fingerprint
	var lerr error
	l.license, lerr = keygen.Validate(l.fingerprint)

	switch {
	case lerr == keygen.ErrLicenseNotActivated:
		// Activate the current fingerprint
		d := time.Until(*l.license.Expiry).Truncate(time.Second)
		if d > 3*24*time.Hour { // > 3 days
			l.logger.Debug("Licensing activation: expires in %.2fd", float64(d)/float64(24*time.Hour))
		} else {
			l.logger.Debug("Licensing activation: expires in %v", d)
		}

		if l.stopped() { // if ReleaseLicense was called, exit now
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
				l.ReleaseLicense()
				os.Exit(1) // exit now (default behavior)!
			}
		}()

		// Always start a heartbeat monitor for the current machine
		if lerr := l.monitor(machine); lerr != nil {
			err = fmt.Errorf("license heartbeat monitor failed to start: %w", lerr)
		}
		return

	case lerr != nil:
		err = fmt.Errorf("invalid license: %w", lerr)
		return
	}
}

func (l *License) ReleaseLicense() {
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
		l.logger.Debug("Licensing deactivation")
		l.license.Deactivate(l.fingerprint)
	}
	l.released = true
}

// monitorRetry: try connecting to keygen SaaS for upto 16 minutes
func (l *License) monitorRetry(m *keygen.Machine) error {
	t := 30 * time.Second
	c := 32

	for range time.Tick(t) {
		if err := heartbeat(m); err != nil {
			if c = c - 1; c < 0 {
				return err
			}
		}
	}
	return nil
}

// monitor: send keygen SaaS heartbeat
func (l *License) monitor(m *keygen.Machine) error {
	if err := heartbeat(m); err != nil {
		return err
	}

	go func() {
		t := (time.Duration(m.HeartbeatDuration) * time.Second) - (30 * time.Second)

		for range time.Tick(t) {
			if err := heartbeat(m); err != nil {
				if err := l.monitorRetry(m); err != nil {
					// give up - leak license
					l.logger.Error("Licensing heartbeat error: %v", err)
					return
				}
			}
		}
	}()
	return nil
}

func heartbeat(m *keygen.Machine) error {
	client := keygen.NewClient()

	_, err := client.Post("machines/"+m.ID+"/actions/ping", nil, m)
	return err
}
