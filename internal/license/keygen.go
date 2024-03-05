package license

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	rhttp "github.com/hashicorp/go-retryablehttp"
	"github.com/keygen-sh/keygen-go/v2"
	"github.com/sirupsen/logrus"

	"github.com/open-policy-agent/opa/logging"
)

const eopaLicenseToken = "EOPA_LICENSE_TOKEN"
const eopaLicenseKey = "EOPA_LICENSE_KEY"
const ErrorExitCode = 3

var licenseRetries = 6   // up to 30 seconds total
var defaultRateSleep = 5 // seconds

var ErrMissingLicense = fmt.Errorf(`no license provided

Sign up for a free trial now by running %s

If you already have a license:
    Define either %q or %q in your environment
        - or -
    Provide the %s or %s flag when running a command

For more information on licensing Enterprise OPA visit https://docs.styra.com/enterprise-opa/installation/licensing`,
	"`eopa license trial`", eopaLicenseKey, eopaLicenseToken, "`--license-key`", "`--license-token`")

type Source int

const (
	SourceCommandLine Source = iota
	SourceOverride
)

type (
	Checker struct {
		mutex       sync.Mutex
		license     *keygen.License
		logger      logging.Logger
		expiry      time.Time
		released    bool
		shutdown    chan struct{}
		finished    chan struct{}
		fingerprint string
	}

	LicenseParams struct {
		Source Source // EKM override or command line
		Key    string
		Token  string
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
	KeygenLicense struct {
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

func NewChecker() *Checker {
	keygen.Account = "dd0105d1-9564-4f58-ae1c-9defdd0bfea7" // account=styra-com
	keygen.Product = "f7da4ae5-7bf5-46f6-9634-026bec5e8599" // product=enterprise-opa
	keygen.PublicKey = "8b8ff31c1d3031add9b1b734e09e81c794731718c2cac2e601b8dfbc95daa4fc"
	// keygen.APIURL = "https://2.2.2.99" // simulate offline network (timeout)
	// keygen.APIURL = "https://api.keygenx.sh" // simulate offline network (DNS not found)

	// validate licensekey or licensetoken
	keygen.LicenseKey = os.Getenv(eopaLicenseKey)
	if keygen.LicenseKey == "" {
		keygen.Token = os.Getenv(eopaLicenseToken) // activation-token of a license; determines the policy
	}

	// remove licenses from environment! (opa.runtime.env)
	os.Unsetenv(eopaLicenseKey)
	os.Unsetenv(eopaLicenseToken)

	l := &Checker{
		logger:   logging.New(),
		shutdown: make(chan struct{}, 1),
		finished: make(chan struct{}, 1),
	}

	keygen.Logger = l
	retryClient := rhttp.NewClient()
	retryClient.Logger = l
	keygen.HTTPClient = retryClient.StandardClient()
	return l
}

func NewLicenseParams() *LicenseParams {
	lp := &LicenseParams{
		Source: SourceCommandLine,
	}
	return lp
}

// Debug, Info, Warn and Error are for rhttp. They're set up as methods
// on *Checker so they react to someone updating the Checker's logger via
// SetLogger().
func (l *Checker) Debug(f string, fs ...any) {
	l.logger.WithFields(fields(fs)).Debug(f)
}

func (l *Checker) Info(f string, fs ...any) {
	l.logger.WithFields(fields(fs)).Info(f)
}

func (l *Checker) Warn(f string, fs ...any) {
	l.logger.WithFields(fields(fs)).Warn(f)
}

func (l *Checker) Error(f string, fs ...any) {
	l.logger.WithFields(fields(fs)).Error(f)
}

func fields(fs []any) map[string]any {
	x := make(map[string]any, len(fs)/2)
	for i := 0; i < len(fs)/2; i++ {
		v := fs[2*i+1]
		if w, ok := v.(fmt.Stringer); ok {
			v = w.String()
		}
		x[fs[2*i].(string)] = v
	}
	return x
}

// Debugf, Infof, Warnf and Errorf for keygen's logging. They're set up as methods
// on *Checker so they react to someone updating the Checker's logger via SetLogger().
// NOTE(sr): We're mapping ALL keygen errors to "debug" level. We don't want to show
// them under ordinary conditions, but if we're debugging license trouble, they need
// to be surfaced.
func (l *Checker) Errorf(format string, v ...interface{}) {
	l.logger.Debug(format, v...)
}

func (l *Checker) Warnf(format string, v ...interface{}) {
	l.logger.Debug(format, v...)
}

func (l *Checker) Infof(format string, v ...interface{}) {
	l.logger.Debug(format, v...)
}

func (l *Checker) Debugf(string, ...interface{}) {
	// l.logger.Debug(format, v...) // very noisy
}

func (l *Checker) ID() string {
	if l.license != nil {
		return l.license.ID
	}
	return ""
}

func (l *Checker) IsOnline() bool {
	return l.license != nil
}

func (l *Checker) Expiry() time.Time {
	return l.expiry
}

func (l *Checker) Logger() logging.Logger {
	return l.logger
}

func (l *Checker) SetLogger(logger logging.Logger) {
	l.logger = logger
}

func (l *Checker) SetLevel(level logging.Level) {
	if std, ok := l.logger.(*logging.StandardLogger); ok {
		std.SetLevel(level)
	}
}

func (l *Checker) SetFormatter(formatter logrus.Formatter) {
	if std, ok := l.logger.(*logging.StandardLogger); ok {
		std.SetFormatter(formatter)
	}
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

func (l *Checker) showExpiry(prefix string) {
	d := time.Until(l.expiry).Truncate(time.Second)
	if d > 3*24*time.Hour { // > 3 days
		l.logger.Debug("%s: expires in %.2fd", prefix, float64(d)/float64(24*time.Hour))
	} else {
		l.logger.Warn("%s: expires in %v", prefix, d)
	}
}

func stringToTime(data string, param string) (time.Time, error) {
	if data == "" {
		return time.Time{}, fmt.Errorf("off-line license verification: missing %s time", param)
	}

	t, err := time.Parse("2006-01-02T15:04:05.000Z", data)
	if err != nil {
		return time.Time{}, fmt.Errorf("off-line license verification: %w", err)
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

func (l *Checker) validateOffline() error {
	// Verify the license key's signature and decode embedded dataset
	license := &keygen.License{Scheme: keygen.SchemeCodeEd25519, Key: keygen.LicenseKey}
	dataset, err := license.Verify()
	if err != nil {
		return fmt.Errorf("off-line license verification: %w", err)
	}

	var data keygenDataset
	if err := json.Unmarshal(dataset, &data); err != nil {
		return fmt.Errorf("off-line license verification: %w", err)
	}

	if data.Product.ID != keygen.Product {
		return fmt.Errorf("off-line license verification: invalid product")
	}

	created, err := stringToTime(data.License.Created, "created")
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if created.After(now.Add(time.Hour)) {
		return fmt.Errorf("off-line license verification: invalid created %s", created.String())
	}

	expiry, err := stringToTime(data.License.Expiry, "expiry")
	if err != nil {
		return err
	}

	if expiry.Before(now) {
		return fmt.Errorf("off-line license verification: license expired %s", expiry.String())
	}
	l.expiry = expiry
	l.showExpiry("Offline license:")
	return nil
}

// ValidateLicense validate and activate the keygen license
// This is the up-front license check: we won't retry! If it fails,
// the startup goes into OPA-fallback mode.
func (l *Checker) ValidateLicense(ctx context.Context, params *LicenseParams) error {
	return l.oneOffLicenseCheck(ctx, params)
}

// ValidateLicenseOrDie can only be started once in the lifetime of an EOPA process:
// it'll either end in success and disappear; or it'll end in failure, and stop the
// entire process (harshly).
func (l *Checker) ValidateLicenseWithRetry(ctx context.Context, params *LicenseParams) error {
	return l.Retrier(ctx, params)
}

// This function is run through Retrier, and
// - is only run by that once at a time
// - is retried on any errors
// However, we still check the Retry-After rate limit header -- since this method
// is also called for the synchronous license verification, it's better to deal with
// the rate limit properly.
func (l *Checker) validateForRetry(params *LicenseParams) error {
	// update keygen.Token and keygen.Key from passed-in-parameters (EKM)
	if err := params.UpdateGlobals(); err != nil {
		return err
	}

	if isOfflineKey(keygen.LicenseKey) {
		return l.validateOffline()
	}
	// use random fingerprint: floating concurrent license
	l.fingerprint = uuid.New().String()

	// Validate the license for the current fingerprint
	license, err := keygen.Validate(l.fingerprint)
	if err != nil {
		// if the err is "not activated", we're OK: proceed to activate it
		if err != keygen.ErrLicenseNotActivated {
			if isTimeout(err) { // fix output message
				return fmt.Errorf("invalid license: timed out")
			}
			return err
		}
		// NOTE(sr): It's odd, but license really is part of the return
		// values from keygen.Validate, even if err != nil
		if license.Expiry == nil {
			return fmt.Errorf("license activation: missing expiry")
		}
	}
	l.expiry = *license.Expiry
	l.showExpiry("License")
	l.SetLicense(license)

	machine, err := license.Activate(l.fingerprint)
	if err != nil {
		return fmt.Errorf("license activation: %w", err)
	}
	// NOTE(sr): we're almost done, and there's no way this could go wrong
	// now -- so we'll spawn off the heartbeat goroutine. The `return nil`
	// below will stop the retrier loop.
	//
	// old comment: also set policy "Heartbeat Basis": FROM_CREATION
	go l.monitor(machine)
	return nil
}

func (l *Checker) UpdateLicenseParams(lp *LicenseParams) {
	retryParams.Store(lp)
}

func (params *LicenseParams) UpdateGlobals() error {
	var err error
	if keygen.LicenseKey == "" && keygen.Token == "" {
		switch {
		case params.Key != "":
			keygen.LicenseKey, err = readSource(params.Key, params.Source)
		case params.Token != "":
			keygen.Token, err = readSource(params.Token, params.Source)
		default:
			err = ErrMissingLicense
		}
	}
	return err
}

func readSource(fileOrContent string, s Source) (string, error) {
	switch s {
	case SourceOverride:
		return fileOrContent, nil
	}
	return readLicense(fileOrContent)
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

func (l *Checker) SetLicense(license *keygen.License) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	stopped := l.stopped()
	if !stopped {
		l.license = license
	}
	return stopped
}

// stopped: see if ReleaseLicense was called
func (l *Checker) stopped() bool {
	select {
	case <-l.shutdown:
		return true
	default:
		return false
	}
}

// wait: wait for monitor to stop
func (l *Checker) Wait(dur time.Duration) bool {
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

func (l *Checker) sleep(dur time.Duration) bool {
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

func (l *Checker) ReleaseLicense() {
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
func (l *Checker) monitorRetry(m *keygen.Machine) error {
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
func (l *Checker) monitor(m *keygen.Machine) {
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

func (l *Checker) heartbeat(m *keygen.Machine) error {
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

func (l *Checker) Machines() (int, error) {
	m, err := l.license.Machines()
	return len(m), err
}

func (l *Checker) Policy() (*KeygenLicense, error) {
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
	var data KeygenLicense
	if err := json.Unmarshal(license.Body, &data); err != nil {
		return nil, fmt.Errorf("license unmarshal failed: %w", err)
	}
	return &data, nil
}
