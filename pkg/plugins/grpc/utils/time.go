// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package time

import "time"

// TimerWithCancel exists because we had several memory leaks when using time.After in select statements.
// Instead, we now manually create timers, wait on them, and manually free them. See this for more details:
// https://www.arangodb.com/2020/09/a-story-of-a-memory-leak-in-go-how-to-properly-use-time-after/
//
// Warning: the cancel cannot be done concurrent to reading, everything should work in the same routine
//
// Example:
//
//	for retries := 0; true; retries++ {
//
//		...main logic...
//
//		timer, cancel := ftime.TimerWithCancel(utils.Backoff(retries))
//		select {
//		case <-ctx.Done():
//			cancel()
//			return ctx.Err()
//		case <-timer.C:
//			continue
//		}
//	}
func TimerWithCancel(delay time.Duration) (*time.Timer, func()) {
	timer := time.NewTimer(delay)

	return timer, func() {
		// the Stop function returns true if the timer is active, so no draining is required at this time, but the timer is stopped
		// if the Stop function returned false, then the timer was already stopped or fired/expired.
		// In this case the channel should be drained to prevent memory leaks only if it is not empty.
		// It is safe only if the cancel function is used in same go routine.
		// The concurrent reading or canceling may cause deadlock.
		if !timer.Stop() && len(timer.C) > 0 {
			<-timer.C
		}
	}
}
