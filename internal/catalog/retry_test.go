package catalog

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSleeper records Sleep calls without blocking.
type fakeSleeper struct{ calls []time.Duration }

func (f *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	f.calls = append(f.calls, d)
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// blockingSleeper respeta ctx.Done() durante el sleep.
type blockingSleeper struct{}

func (blockingSleeper) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// 503 transient HTTP error para los tests.
func mk503() error {
	return &HTTPStatusError{StatusCode: http.StatusServiceUnavailable, URL: "http://x", Transient: true}
}

// TestRetry_503_503_Success: 503, 503, success. 2 sleeps (1s, 2s).
func TestRetry_503_503_Success(t *testing.T) {
	sleeper := &fakeSleeper{}
	var calls atomic.Int32
	op := func(ctx context.Context) error {
		n := calls.Add(1)
		if n < 3 {
			return mk503()
		}
		return nil
	}
	if err := Retry(context.Background(), sleeper, IsTransientHTTP, op); err != nil {
		t.Fatalf("Retry = %v, want nil", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("op calls = %d, want 3", got)
	}
	if len(sleeper.calls) != 2 || sleeper.calls[0] != 1*time.Second || sleeper.calls[1] != 2*time.Second {
		t.Fatalf("sleeps = %v, want [1s 2s]", sleeper.calls)
	}
}

// TestRetry_ThreeFailuresExhausted: 3 fallos agotan reintentos.
func TestRetry_ThreeFailuresExhausted(t *testing.T) {
	sleeper := &fakeSleeper{}
	var calls atomic.Int32
	transient := errors.New("503 upstream")
	op := func(ctx context.Context) error { calls.Add(1); return transient }
	err := Retry(context.Background(), sleeper, func(error) bool { return true }, op)
	if err == nil || !errors.Is(err, ErrRetriesExhausted) || !errors.Is(err, transient) {
		t.Fatalf("err = %v, want wrapped ErrRetriesExhausted AND root cause %v", err, transient)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("op calls = %d, want exactly 3 (no 4th attempt)", got)
	}
	if len(sleeper.calls) != 2 {
		t.Fatalf("sleeps = %v, want 2", sleeper.calls)
	}
}

// TestRetry_404NoSleep: 404 es permanente. Retry NO duerme ni retry.
func TestRetry_404NoSleep(t *testing.T) {
	sleeper := &fakeSleeper{}
	var calls atomic.Int32
	op := func(ctx context.Context) error {
		calls.Add(1)
		return &HTTPStatusError{StatusCode: http.StatusNotFound, URL: "http://x", Transient: false}
	}
	err := Retry(context.Background(), sleeper, IsTransientHTTP, op)
	var httpErr *HTTPStatusError
	if err == nil || !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
		t.Fatalf("err = %v, want HTTPStatusError 404", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("op calls = %d, want 1 (404 permanent)", got)
	}
	if len(sleeper.calls) != 0 {
		t.Fatalf("sleeps = %v, want 0 (404 no sleep)", sleeper.calls)
	}
}

// TestRetry_DeadlinePreventsNextAttempt: deadline bloquea el primer intento.
func TestRetry_DeadlinePreventsNextAttempt(t *testing.T) {
	sleeper := &fakeSleeper{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var calls atomic.Int32
	op := func(ctx context.Context) error { calls.Add(1); <-ctx.Done(); return ctx.Err() }
	err := Retry(ctx, sleeper, func(error) bool { return true }, op)
	if err == nil || (!errors.Is(err, ErrRetriesExhausted) && !errors.Is(err, context.DeadlineExceeded)) {
		t.Fatalf("err = %v, want ErrRetriesExhausted or DeadlineExceeded", err)
	}
	if got := calls.Load(); got > 1 {
		t.Fatalf("op calls = %d, deadline should prevent next attempt", got)
	}
}

// TestRetry_CancellationDuringSleep: ctx cancel aborta el sleep.
func TestRetry_CancellationDuringSleep(t *testing.T) {
	sleeper := &blockingSleeper{}
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	op := func(ctx context.Context) error { calls.Add(1); return mk503() }
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	err := Retry(ctx, sleeper, IsTransientHTTP, op)
	if err == nil || (!errors.Is(err, context.Canceled) && !errors.Is(err, ErrRetriesExhausted)) {
		t.Fatalf("err = %v, want context.Canceled or ErrRetriesExhausted", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("op calls = %d, cancel should stop after first attempt", got)
	}
}
