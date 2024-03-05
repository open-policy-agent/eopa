package license

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/logging"
	logging_test "github.com/open-policy-agent/opa/logging/test"
)

// TestRetrier asserts certain behaviors on the retrier logic that underpins our license
// validation retry mechanism. Because mocking the APIs of keygen for this in a way that
// lets us simulate failures is annoying, we'll go with this "analogous test".
func TestRetrier(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewNoOpLogger()
	delay := 10 * time.Millisecond
	maxAttempts := -1

	t.Run("happy path", func(t *testing.T) {
		attempts := 0
		if err := retrier(ctx, delay, maxAttempts, logger, func() error {
			attempts++
			return nil
		}); err != nil {
			t.Fatalf("got error: %v %[1]T", err)
		}
		if exp, act := 1, attempts; exp != act {
			t.Errorf("expected %d attempts, got %d", exp, act)
		}
	})

	t.Run("max out max duration", func(t *testing.T) {
		// NOTE(sr): This test is relevant because we haven't limited the number of retries (-1).
		attempts := 0
		maxDuration := 100 * time.Millisecond
		ctx, cancel := context.WithTimeout(ctx, maxDuration)
		defer cancel()
		err := retrier(ctx, delay, maxAttempts, logger, func() error {
			attempts++
			return fmt.Errorf("error %d", attempts-1)
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if exp, act := 5, attempts; exp > act {
			t.Errorf("expected more than %d attempts, got %d", exp, act)
		}
	})

	t.Run("success on second attempt", func(t *testing.T) {
		attempts := 1
		err := retrier(ctx, delay, maxAttempts, logger, func() error {
			if attempts == 1 {
				attempts++
				return fmt.Errorf("error %d", attempts-1)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v %[1]T", err)
		}
		if exp, act := 2, attempts; exp != act {
			t.Errorf("expected %d attempts, got %d", exp, act)
		}
	})

	t.Run("logger warns us on failures", func(t *testing.T) {
		attempts := 1
		logger := logging_test.New()
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		err := retrier(ctx, delay, maxAttempts, logger, func() error {
			if attempts == 1 {
				attempts++
				return fmt.Errorf("error %d", attempts-1)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v %[1]T", err)
		}
		if exp, act := 2, attempts; exp != act {
			t.Errorf("expected %d attempts, got %d", exp, act)
		}
		if exp, act := 1, len(logger.Entries()); exp != act {
			t.Fatalf("expected %d log entries, got %d", exp, act)
		}
		log := logger.Entries()[0]
		if exp, act := logging.Warn, log.Level; exp != act {
			t.Errorf("expected log level %v, got %v", exp, act)
		}
		if exp, act := "error 1: retrying for 0s before shutdown", log.Message; exp != act {
			t.Errorf("expected log message %q, got %q", exp, act)
		}
	})

	t.Run("logger unwraps retryhttp errors", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		e := errors.New("foo")
		attempts := 0
		err := retrier(ctx, delay, 1, logger, func() error {
			attempts++
			return fmt.Errorf("giving up after %d attempt(s): %w", attempts, e)
		})
		if err != e { // NB: don't use errors.Is here, as that would unwrap, too
			t.Errorf("unexpected error: %v %[1]T", err)
		}
	})
}
