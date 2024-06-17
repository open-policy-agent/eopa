package license

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/open-policy-agent/opa/logging"
)

var retryParams = atomic.Pointer[LicenseParams]{}

// These are the steps taken to validate a license. Any of them may fail in unforeseen
// ways -- we have seen CloudFlare DDoS mitigations cause "invalid signature" errors,
// so there's no way to really trust the response payloads.
//
//  1. keygen.Validate
//     a. on Timeout and NetworkErrors
//     - i. if offline key; perform offline validation
//     b. on LicenseNotActivated Errors
//     - i. keygen.Activate
//     - ii. start keygen machine monitor
func (l *Checker) Retrier(ctx context.Context, params *LicenseParams) error {
	maxDuration := 2 * 24 * time.Hour
	delay := time.Minute // TODO(sr): time.Hour
	maxAttempts := -1    // don't limit attempts
	retryParams.Store(params)
	ctx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()
	return retrier(ctx, delay, maxAttempts, l.logger, func() error {
		return l.validateForRetry(retryParams.Load())
	})
}

func (l *Checker) oneOffLicenseCheck(ctx context.Context, params *LicenseParams) error {
	maxAttempts := 1 // only one shot!
	retryParams.Store(params)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return retrier(ctx, time.Minute, maxAttempts, l.logger, func() error {
		return l.validateForRetry(retryParams.Load())
	})
}

// NOTE(sr): We're retrying on ANY error. This includes
// - keygen is down
// - no license set
// - online/offline license is expired
//
// This is because EKM could at any point update our license data, or a file
// on disk containing the license could have new contents.
//
// However, once we're done here, we'll STOP. So when the (online/offline) license
// has been validated ONCE, updates to it (via EKM, new file contents), will be
// ignored.
func retrier(ctx context.Context, delay time.Duration, maxAttempts int, logger logging.Logger, attempt func() error) error {
	deadline, hasDeadline := ctx.Deadline()
	err := backoff.RetryNotify(
		attempt,
		backoff.WithContext(
			backoff.WithMaxRetries(
				backoff.NewConstantBackOff(delay),
				uint64(maxAttempts-1),
			),
			ctx,
		),
		func(err error, _ time.Duration) {
			if hasDeadline && maxAttempts != 1 { // if we're only doing one attempt, don't log this
				timeBeforeShutdown := time.Until(deadline)
				logger.Warn("%v: retrying for %v before shutdown", err, timeBeforeShutdown.Truncate(time.Minute))
			}
		},
	)
	// NOTE(sr): Examining error strings is a no-no, but here's our predicament:
	// Both keygen-go and go-retryablehttp return errors build from fmt.Errorf and
	// errors.New, and it's impossible to type-assert either of them.
	// So, we give in, and only dress up the retryablehttp errors a little by
	// removing their superfluous wrapping. Nobody wants to see this in their logs:
	//
	//    Get "https://api.keygenx.sh/v1/me": GET https://api.keygenx.sh/v1/me giving
	//    up after 5 attempt(s): Get "https://api.keygenx.sh/v1/me": dial tcp: lookup
	//    api.keygenx.sh: no such host
	if err != nil && strings.Contains(err.Error(), "giving up after") { // unwrap noisy retryablehttp errors
		return unwrap(err)
	}
	return err
}

// unwrap calls errors.Unwrap until it returns nil, and returns the penultimate error.
func unwrap(err error) error {
	next := err
	var prev error
	for next != nil {
		prev = next
		next = errors.Unwrap(prev)
	}
	return prev
}
