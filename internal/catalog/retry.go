// Retry primitive for the catalog package (PR-6). Exactly three
// attempts total: initial, 1s sleep, attempt 2, 2s sleep, attempt 3.
// No 4s sleep, no fourth attempt. Sleeper and isTransient are
// injectable so tests drive deterministic no-wall-clock behaviour.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const RetryAttempts = 3

// RetrySleeps[i] is the wait BEFORE attempt i+2. Index 0 = 1s
// (before 2nd attempt), index 1 = 2s (before 3rd attempt). Index 2
// is undefined; the loop never schedules it.
var RetrySleeps = []time.Duration{1 * time.Second, 2 * time.Second}

// ErrRetriesExhausted wraps the last transient error after the
// retry budget is spent. errors.Is(err, ErrRetriesExhausted) is
// the canonical signal; Unwrap exposes the root cause.
var ErrRetriesExhausted = errors.New("catalog: retries exhausted")

// Sleeper blocks for d or until ctx is done. RealSleeper uses
// time.NewTimer; tests inject a fake that records calls.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// RealSleeper implements Sleeper with time.NewTimer. ctx.Err()
// wins so a cancelled context stops the wait immediately.
type RealSleeper struct{}

// Sleep blocks until d elapses or ctx is cancelled.
func (RealSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Retry runs op up to RetryAttempts times. The loop terminates
// immediately when ctx is cancelled or its deadline expires (checked
// before each attempt and each sleep), or when isTransient(err)
// returns false. On exhaustion the returned error wraps
// ErrRetriesExhausted and preserves the last transient error via
// Unwrap. A nil sleeper falls back to RealSleeper; a nil isTransient
// treats every error as transient.
func Retry(ctx context.Context, sleeper Sleeper, isTransient func(error) bool, op func(context.Context) error) error {
	if sleeper == nil {
		sleeper = RealSleeper{}
	}
	if isTransient == nil {
		isTransient = func(error) bool { return true }
	}
	var lastErr error
	attempts := 0
	for i := 0; i < RetryAttempts; i++ {
		if err := ctx.Err(); err != nil {
			return wrapExhausted(attempts, lastErr, err)
		}
		if i > 0 {
			if err := sleeper.Sleep(ctx, RetrySleeps[i-1]); err != nil {
				return wrapExhausted(attempts, lastErr, err)
			}
			if err := ctx.Err(); err != nil {
				return wrapExhausted(attempts, lastErr, err)
			}
		}
		attempts++
		err := op(ctx)
		if err == nil {
			return nil
		}
		if !isTransient(err) {
			return err
		}
		lastErr = err
	}
	return wrapExhausted(attempts, lastErr, nil)
}

// wrapExhausted composes the final error preserving the root
// cause from either the last transient error or the context.
func wrapExhausted(attempts int, lastErr, ctxErr error) error {
	if ctxErr != nil {
		if lastErr == nil {
			return ctxErr
		}
		return fmt.Errorf("%w: %d attempts failed, then context: %v (last error: %w)", ErrRetriesExhausted, attempts, ctxErr, lastErr)
	}
	if lastErr == nil {
		return fmt.Errorf("%w: no attempts made", ErrRetriesExhausted)
	}
	return fmt.Errorf("%w: %d attempts failed: %w", ErrRetriesExhausted, attempts, lastErr)
}
