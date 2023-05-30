package keygen

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/keygen-sh/keygen-go/v2"
	"github.com/sirupsen/logrus"

	"github.com/open-policy-agent/opa/logging"
)

const loadLicenseToken = "STYRA_LOAD_LICENSE_TOKEN"
const loadLicenseKey = "STYRA_LOAD_LICENSE_KEY"
const licenseErrorExitCode = 3

var licenseRetries = 6   // up to 30 seconds total
var defaultRateSleep = 5 // seconds

var ErrMissingLicense = fmt.Errorf(`no license provided

Sign up for a free trial now by running %s

If you already have a license:
    Define either %q or %q in your environment
        - or -
    Provide the %s or %s flag when running a command

For more information on licensing Styra Load visit https://docs.styra.com/load/installation/licensing`,
	"`load license trial`", loadLicenseKey, loadLicenseToken, "`--license-key`", "`--license-token`")

type Source int

const (
	SourceCommandLine Source = iota
	SourceOverride
)

type (
	License struct {
		mutex       sync.Mutex
		license     *keygen.License
		logger      logging.Logger
		expiry      time.Time
		released    bool
		shutdown    chan struct{}
		finished    chan struct{}
		started     bool
		mustStartBy *time.Timer
		fingerprint string
	}

	LicenseParams struct {
		Source Source // EKM override or command line
		Key    string
		Token  string
	}

	keygenLogger struct {
		logger logging.Logger
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

	// retrieve license: https://keygen.sh/docs/api/licenses/#licenses-retrieve
	keygenLicense struct {
		Data struct {
			Attributes struct {
				MaxMachines int
			}
			RelationShips struct {
				Machines struct {
					Meta struct {
						Count int
					}
				}
			}
		}
	}
)

func NewLicense() *License {
	keygen.Account = "dd0105d1-9564-4f58-ae1c-9defdd0bfea7" // account=styra-com
	keygen.Product = "f7da4ae5-7bf5-46f6-9634-026bec5e8599" // product=load
	keygen.PublicKey = "8b8ff31c1d3031add9b1b734e09e81c794731718c2cac2e601b8dfbc95daa4fc"
	//keygen.APIURL = "https://2.2.2.99" // simulate offline network (timeout)
	//keygen.APIURL = "https://api.keygenx.sh" // simulate offline network (DNS not found)

	logger := logging.New()
	keygen.Logger = &keygenLogger{logger}

	// validate licensekey or licensetoken
	keygen.LicenseKey = os.Getenv(loadLicenseKey)
	if keygen.LicenseKey == "" {
		keygen.Token = os.Getenv(loadLicenseToken) // activation-token of a license; determines the policy
	}

	// remove licenses from environment! (opa.runtime.env)
	os.Unsetenv(loadLicenseKey)
	os.Unsetenv(loadLicenseToken)

	l := &License{logger: logger, shutdown: make(chan struct{}, 1), finished: make(chan struct{}, 1)}
	l.mustStartBy = time.AfterFunc(10*time.Minute, l.timerCallback) // 10 minutes to start license check limit
	return l
}

func NewLicenseParams() *LicenseParams {
	lp := &LicenseParams{
		Source: SourceCommandLine,
	}
	return lp
}

// NOTE(sr): We're mapping ALL keygen errors to "debug" level. We don't want to show
// them under ordinary conditions, but if we're debugging license trouble, they need
// to be surfaced.
func (l *keygenLogger) Errorf(format string, v ...interface{}) {
	l.logger.Debug(format, v...)
}

func (l *keygenLogger) Warnf(format string, v ...interface{}) {
	l.logger.Debug(format, v...)
}

func (l *keygenLogger) Infof(format string, v ...interface{}) {
	l.logger.Debug(format, v...)
}

func (l *keygenLogger) Debugf(string, ...interface{}) {
	// l.logger.Debug(format, v...) // very noisy
}

func (l *License) IsOnline() bool {
	return l.license != nil
}

func (l *License) Expiry() time.Time {
	return l.expiry
}

func (l *License) Logger() logging.Logger {
	return l.logger
}

func (l *License) SetLogger(logger logging.Logger) {
	l.logger = logger
}

func (l *License) SetLevel(level logging.Level) {
	if std, ok := l.logger.(*logging.StandardLogger); ok {
		std.SetLevel(level)
	}
}

func (l *License) SetFormatter(formatter logrus.Formatter) {
	if std, ok := l.logger.(*logging.StandardLogger); ok {
		std.SetFormatter(formatter)
	}
}

func (l *License) timerCallback() {
	l.logger.Error("licensing error: timeout")
	os.Exit(licenseErrorExitCode)
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
	l.expiry = expiry

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

func isOfflineKey(key string) bool {
	return strings.HasPrefix(key, "key/")
}

func rateLimitRetrySeconds(lerr error) time.Duration {
	var e *keygen.RateLimitError
	if errors.As(lerr, &e) {
		r := e.RetryAfter
		if r == 0 {
			r = defaultRateSleep
		}
		return time.Duration(r) * time.Second
	}
	return 0
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

// ValidateLicense validate and activate the keygen license
//  1. keygen.Validate
//     a. on Timeout and NetworkErrors
//     - i. if offline key; perform offline validation
//     b. on LicenseNotActivated Errors
//     - i. keygen.Activate
//     - ii. setup signal handler SIGINT, SIGTERM
//     - iii. start keygen machine monitor
func (l *License) ValidateLicense(params *LicenseParams, terminate func(code int, err error)) {
	var err error
	defer func() {
		if err != nil {
			terminate(licenseErrorExitCode, err)
		}
	}()

	// stop background timer
	l.mustStartBy.Stop()

	l.mutex.Lock()
	if l.started { // only run once
		l.mutex.Unlock()
		return
	}
	l.started = true
	l.mutex.Unlock()

	if l.logger == nil {
		l.logger = logging.Get()
	}

	// validate licensekey or licensetoken
	if keygen.LicenseKey == "" && keygen.Token == "" {
		var dat string
		if params.Key != "" {
			if params.Source == SourceOverride {
				dat = params.Key
			} else {
				dat, err = readLicense(params.Key)
				if err != nil {
					return
				}
			}
			keygen.LicenseKey = dat
		} else if params.Token != "" {
			if params.Source == SourceOverride {
				dat = params.Token
			} else {
				dat, err = readLicense(params.Token)
				if err != nil {
					return
				}
			}
			keygen.Token = dat
		} else {
			err = ErrMissingLicense
			return
		}
	}

	if l.stopped() { // if ReleaseLicense was called, exit now
		return
	}

	// try offline license
	if isOfflineKey(keygen.LicenseKey) {
		err = l.validateOffline()
		return
	}

	// use random fingerprint: floating concurrent license
	l.fingerprint = uuid.New().String()

	var lerr error
	var license *keygen.License
	for i := 0; i < licenseRetries; i++ {
		// Validate the license for the current fingerprint
		license, lerr = keygen.Validate(l.fingerprint)
		if lerr == nil {
			err = fmt.Errorf("invalid license: expected LicenseNotActivated")
			return
		}
		if r := rateLimitRetrySeconds(lerr); r != 0 {
			l.logger.Info("ValidateLicense rate limit error: Retry-After=%v", r)
			if !l.sleep(r) {
				return
			}
			continue
		}
		break
	}

	if lerr == keygen.ErrLicenseNotActivated {
		go func() {
			// Handle SIGINT and gracefully deactivate the machine
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGSEGV) // disable default os.Interrupt handler

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

		var lerr error
		var machine *keygen.Machine
		for i := 0; i < licenseRetries; i++ {
			machine, lerr = license.Activate(l.fingerprint)
			if lerr != nil {
				if r := rateLimitRetrySeconds(lerr); r != 0 {
					l.logger.Info("ActivateLicense rate limit error: Retry-After=%v", r)
					if !l.sleep(r) {
						return
					}
					continue
				}
			}
			break
		}
		if lerr != nil {
			err = fmt.Errorf("license activation failed: %w", lerr)
		}

		if l.stopped() { // if ReleaseLicense was called, exit now
			return
		}

		// Start heartbeat monitor for machine (also set policy "Heartbeat Basis": FROM_CREATION)
		go l.monitor(machine)
		return
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

// stopped: see if ReleaseLicense was called
func (l *License) stopped() bool {
	select {
	case <-l.shutdown:
		return true
	default:
		return false
	}
}

// wait: wait for monitor to stop
func (l *License) wait(dur time.Duration) bool {
	delay := time.NewTimer(dur)
	select {
	case <-l.finished:
		if !delay.Stop() {
			<-delay.C // if the timer has been stopped then read from the channel.
		}
		return false
	case <-delay.C:
		return true
	}
}

func (l *License) sleep(dur time.Duration) bool {
	delay := time.NewTimer(dur)
	select {
	case <-l.shutdown:
		if !delay.Stop() {
			<-delay.C // if the timer has been stopped then read from the channel.
		}
		return false
	case <-delay.C:
		return true
	}
}

func (l *License) ReleaseLicense() {
	if l == nil {
		return
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.released {
		return
	}

	close(l.shutdown) // wakeup any sleeps

	if l.license != nil {
		l.logger.Debug("Licensing deactivation")
		for i := 0; i < licenseRetries; i++ {
			err := l.license.Deactivate(l.fingerprint)
			if err != nil {
				if r := rateLimitRetrySeconds(err); r != 0 {
					l.logger.Info("ReleaseLicense rate limit error: Retry-After=%v", r)
					time.Sleep(r) // must use time.Sleep; already shutdown
					continue
				}
				l.logger.Error("License deactivation: %v", err)
			}
			break
		}
	}
	l.released = true
}

// monitorRetry: try connecting to keygen SaaS for upto 16 minutes
func (l *License) monitorRetry(m *keygen.Machine) error {
	t := 30 * time.Second
	c := 32

	for l.sleep(t) {
		err := l.heartbeat(m)
		if err == nil {
			break
		}
		if c = c - 1; c < 0 {
			return err
		}
	}
	return nil
}

// monitor: send keygen SaaS heartbeat
func (l *License) monitor(m *keygen.Machine) {
	defer close(l.finished) // signal monitor has completed

	if l.stopped() {
		return
	}
	if err := l.heartbeat(m); err != nil {
		l.logger.Warn("Licensing heartbeat error: %v", err)
	}

	if m.HeartbeatDuration < 60 { // set up some minimum
		m.HeartbeatDuration = 60
	}
	t := (time.Duration(m.HeartbeatDuration) * time.Second) - (30 * time.Second)

	for l.sleep(t) {
		if err := l.heartbeat(m); err != nil {
			if err := l.monitorRetry(m); err != nil {
				// give up - leak license
				l.logger.Error("Licensing heartbeat error: %v", err)
				return
			}
		}
	}
}

func (l *License) heartbeat(m *keygen.Machine) error {
	var err error
	for i := 0; i < licenseRetries; i++ {
		client := keygen.NewClient()
		_, err = client.Post("machines/"+m.ID+"/actions/ping", nil, m)
		if err == nil {
			return nil
		}
		if r := rateLimitRetrySeconds(err); r != 0 {
			l.logger.Info("monitorRetry rate limit error: Retry-After=%v", r)
			if !l.sleep(r) {
				return nil
			}
			continue
		}
		break
	}
	return fmt.Errorf("heartbeat failure: %w", err)
}

func (l *License) Machines() (int, error) {
	m, err := l.license.Machines()
	return len(m), err
}

func (l *License) Policy() (*keygenLicense, error) {
	var license *keygen.Response
	var err error
	for i := 0; i < licenseRetries; i++ {
		client := keygen.NewClient()
		license, err = client.Get("licenses/"+l.license.ID, nil, nil)
		if err == nil {
			break
		}
		if r := rateLimitRetrySeconds(err); r != 0 {
			l.logger.Info("Policy rate limit error: Retry-After=%v", r)
			if !l.sleep(r) {
				return nil, err
			}
			continue
		}
		break
	}
	if err != nil {
		return nil, fmt.Errorf("policy failure: %w", err)
	}
	var data keygenLicense
	if err := json.Unmarshal(license.Body, &data); err != nil {
		return nil, fmt.Errorf("license unmarshal failed: %w", err)
	}
	return &data, nil
}
