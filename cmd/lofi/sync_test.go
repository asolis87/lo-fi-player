package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

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

// offlineTransport is an http.RoundTripper that always returns an
// error; used to simulate offline network without DNS lookups.
type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.EOF
}

// TestSync_OfflineExits1 guards S-CAT-2 via the runSync CLI path:
// when the network is unreachable, `lofi sync` MUST exit non-zero
// with a recoverable message and the local cache MUST stay
// untouched. We override FirstRunCommitSHA to a valid 40-hex SHA so
// the URL passes ValidateURL; the injected failing transport then
// guarantees the Fetch step fails with ErrOffline.
func TestSync_OfflineExits1(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	const validSHA = "abcdef0123456789abcdef0123456789abcdef01"
	origSHA := catalog.FirstRunCommitSHA
	catalog.FirstRunCommitSHA = validSHA
	t.Cleanup(func() { catalog.FirstRunCommitSHA = origSHA })

	withInjectedSyncer(t, func(dir string) *catalog.Syncer {
		s := catalog.NewSyncer(dir)
		s.HTTPClient = &http.Client{Transport: offlineTransport{}}
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