package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

	// offline license embedded-data: https://keygen.sh/docs/api/cryptography/#cryptographic-keys-template-vars
	// use default template or at least: "{ "product":{"id": "{{product}}"}, "license": {"created": "{{created}}", "expiry": "{{expiry}}"} }"
	keygenDataset struct {
		Product struct {
			ID string
		}
		License struct {
			Created string
			Expiry  string
		}
	}
)

func NewLicense() *License {
	keygen.Account = "dd0105d1-9564-4f58-ae1c-9defdd0bfea7" // account=styra-com
	keygen.Product = "f7da4ae5-7bf5-46f6-9634-026bec5e8599" // product=load
	keygen.PublicKey = "8b8ff31c1d3031add9b1b734e09e81c794731718c2cac2e601b8dfbc95daa4fc"
	//keygen.APIURL = "https://2.2.2.99" // simulate offline network (timeout)
	//keygen.APIURL = "https://api.keygenx.sh" // simulate offline network (DNS not found)
	keygen.Logger = &keygenLogger{level: keygen.LogLevelNone}

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

func (l *License) showExpiry(expiry time.Time, prefix string) {
	d := time.Until(expiry).Truncate(time.Second)
	if d > 3*24*time.Hour { // > 3 days
		l.logger.Debug("%s: expires in %.2fd", prefix, float64(d)/float64(24*time.Hour))
	} else {
		l.logger.Debug("%s: expires in %v", prefix, d)
	}
}

func stringToTime(data string, param string) (time.Time, error) {
	if data == "" {
		return time.Time{}, fmt.Errorf("off-line license verification failed: missing %s time", param)
	}

	t, lerr := time.Parse("2006-01-02T15:04:05.000Z", data)
	if lerr != nil {
		return time.Time{}, fmt.Errorf("off-line license verification failed: %w", lerr)
	}
	return t, nil
}

func (l *License) validateOffline() error {
	// Verify the license key's signature and decode embedded dataset
	license := &keygen.License{Scheme: keygen.SchemeCodeEd25519, Key: keygen.LicenseKey}
	dataset, lerr := license.Verify()
	if lerr != nil {
		return fmt.Errorf("off-line license verification failed: %w", lerr)
	}

	var data keygenDataset
	if lerr := json.Unmarshal(dataset, &data); lerr != nil {
		return fmt.Errorf("off-line license verification failed: %w", lerr)
	}

	if data.Product.ID != keygen.Product {
		return fmt.Errorf("off-line license verification failed: invalid product")
	}

	created, lerr := stringToTime(data.License.Created, "created")
	if lerr != nil {
		return lerr
	}

	now := time.Now().UTC()
	if created.After(now.Add(time.Hour)) {
		return fmt.Errorf("off-line license verification failed: invalid created %s", created.String())
	}

	expiry, lerr := stringToTime(data.License.Expiry, "expiry")
	if lerr != nil {
		return lerr
	}

	if expiry.Before(now) {
		return fmt.Errorf("off-line license verification failed: license expired %s", expiry.String())
	}

	l.showExpiry(expiry, "Licensing offline verification")
	return nil
}

// ValidateLicenses: validate and activate the keygen license
//  1. keygen.Validate
//     a. on Timeout and NetworkErrors
//     - i. if offline key; perform offline validation
//     b. on LicenseNotActivated Errors
//     - i. keygen.Activate
//     - ii. setup signal handler SIGINT, SIGTERM
//     - iii. start keygen machine monitor
func (l *License) ValidateLicense(key string, token string, terminate func(code int, err error)) {
	var err error
	defer func() {
		if err != nil {
			l.logger.Error("licensing error: %v", err)
			terminate(2, err)
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

	if l.stopped() { // if ReleaseLicense was called, exit now
		return
	}

	// use random fingerprint: floating concurrent license
	l.fingerprint = uuid.New().String()

	// Validate the license for the current fingerprint
	license, lerr := keygen.Validate(l.fingerprint)
	if lerr == nil {
		err = fmt.Errorf("invalid license: expected LicenseNotActivated")
		return
	}

	if lerr == keygen.ErrLicenseNotActivated {
		// Handle SIGINT and gracefully deactivate the machine
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGSEGV) // disable default os.Interrupt handler

		go func() {
			for s := range sigs {
				l.ReleaseLicense()
				time.Sleep(100 * time.Millisecond)              // give load server sometime to finish
				terminate(1, fmt.Errorf("caught signal %v", s)) // exit now (default behavior)!
			}
		}()

		// Activate the current fingerprint
		if license.Expiry == nil {
			err = fmt.Errorf("license activation failed: missing expiry")
			return
		}

		l.showExpiry(*license.Expiry, "Licensing activation")

		if l.setLicense(license) { // if ReleaseLicense was called, exit now
			return
		}

		machine, lerr := license.Activate(l.fingerprint)
		if lerr != nil {
			err = fmt.Errorf("license activation failed: %w", lerr)
			return
		}

		if l.stopped() { // if ReleaseLicense was called, exit now
			return
		}

		// Start heartbeat monitor for machine (also set policy "Heartbeat Basis": FROM_CREATION)
		l.monitor(machine)
		return
	}

	// try offline license if validation network error or timeout
	if keygen.LicenseKey != "" && strings.HasPrefix(keygen.LicenseKey, "key/") {
		var netError net.Error
		var keygenError *keygen.Error
		if errors.As(lerr, &netError) || errors.As(lerr, &keygenError) && keygenError.Response != nil && keygenError.Response.Status == 429 {
			err = l.validateOffline()
			return
		}
	}

	if isTimeout(lerr) { // fix output message
		err = fmt.Errorf("invalid license: timed out")
		return
	}

	// something's wrong
	err = fmt.Errorf("invalid license: %w", lerr)
}

func isTimeout(netError error) bool {
	// have to Unwrap errors for os.IsTimeout to figure it IsTimeout
	for netError != nil {
		if os.IsTimeout(netError) {
			return true
		}
		netError = errors.Unwrap(netError)
	}
	return false
}

func (l *License) setLicense(license *keygen.License) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	stopped := l.stopped()
	if !stopped {
		l.license = license
	}
	return stopped
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
func (l *License) monitor(m *keygen.Machine) {
	go func() {
		if err := heartbeat(m); err != nil {
			l.logger.Debug("Licensing heartbeat error: %v", err)
		}

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
}

func heartbeat(m *keygen.Machine) error {
	client := keygen.NewClient()

	_, err := client.Post("machines/"+m.ID+"/actions/ping", nil, m)
	return err
}
