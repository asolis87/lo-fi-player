package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// withInjectedSyncer swaps the package-level syncerFactory for the
// duration of a test so runSync can be driven against an offline
// transport without touching the real DNS or filesystem.
func withInjectedSyncer(t *testing.T, factory func(string) *catalog.Syncer) {
	t.Helper()
	orig := syncerFactory
	syncerFactory = factory
	t.Cleanup(func() { syncerFactory = orig })
}

// withInjectedContextFactory swaps runSyncContextFactory for the
// duration of a test so cancellation can be triggered without
// touching the real OS signal table.
func withInjectedContextFactory(t *testing.T, factory func() (context.Context, context.CancelFunc)) {
	t.Helper()
	orig := runSyncContextFactory
	runSyncContextFactory = factory
	t.Cleanup(func() { runSyncContextFactory = orig })
}

// noOpSleeperCatalog evita los sleeps 1s+2s del retry primitive.
type noOpSleeperCatalog struct{}

func (noOpSleeperCatalog) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, io.EOF }

type blockingTransport struct{}

func (blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// TestSync_OfflineExits1 guards S-CAT-2 via the runSync CLI path.
// FirstRunCommitSHA overrides; failing transport guarantees ErrOffline.
func TestSync_OfflineExits1(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	const validSHA = "abcdef0123456789abcdef0123456789abcdef01"
	origSHA := catalog.FirstRunCommitSHA
	catalog.FirstRunCommitSHA = validSHA
	t.Cleanup(func() { catalog.FirstRunCommitSHA = origSHA })

	withInjectedSyncer(t, func(dir string) *catalog.Syncer {
		s := catalog.NewSyncer(dir)
		s.HTTPClient = &http.Client{Transport: offlineTransport{}}
		s.Sleeper = noOpSleeperCatalog{}
		return s
	})

	code, stderr := captureStderr(t, func() int { return codeFor(runSync(nil)) })
	if code != 1 {
		t.Fatalf("runSync(offline) code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "sync") && !strings.Contains(stderr, "network") {
		t.Fatalf("expected recoverable sync/network message, got %q", stderr)
	}
	if !errors.Is(lastSyncErr, catalog.ErrOffline) {
		t.Fatalf("lastSyncErr = %v, want wrapped ErrOffline", lastSyncErr)
	}
}

// TestSync_SIGINT_Exit130_LockReleased: ctx cancel → exit 130 +
// lock released via defer.
func TestSync_SIGINT_Exit130_LockReleased(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	const validSHA = "abcdef0123456789abcdef0123456789abcdef01"
	origSHA := catalog.FirstRunCommitSHA
	catalog.FirstRunCommitSHA = validSHA
	t.Cleanup(func() { catalog.FirstRunCommitSHA = origSHA })

	withInjectedSyncer(t, func(dir string) *catalog.Syncer {
		s := catalog.NewSyncer(dir)
		s.HTTPClient = &http.Client{Transport: blockingTransport{}}
		s.Sleeper = noOpSleeperCatalog{}
		return s
	})

	ctx, cancel := context.WithCancel(context.Background())
	withInjectedContextFactory(t, func() (context.Context, context.CancelFunc) { return ctx, cancel })
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	code, stderr := captureStderr(t, func() int { return codeFor(runSync(nil)) })
	if code != 130 {
		t.Fatalf("runSync code = %d, want 130, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "cancelled") {
		t.Errorf("missing cancelled message, stderr=%q", stderr)
	}
	root := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "lofi-player", "catalog")
	if locked, _ := catalog.IsLocked(root); locked {
		t.Errorf("lock not released after cancel")
	}
}
