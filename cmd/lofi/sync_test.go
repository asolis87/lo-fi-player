package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// captureBoth redirects os.Stdout and os.Stderr to in-memory
// buffers while fn runs, then returns both captured streams. The
// pipes are drained on a background goroutine so a slow consumer
// cannot deadlock the caller; the buffers are the single source
// of truth tests assert against.
func captureBoth(t *testing.T, fn func() int) (int, string, string) {
	t.Helper()
	origStdout, origStderr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = wOut
	os.Stderr = wErr
	var outBuf, errBuf bytes.Buffer
	doneOut, doneErr := make(chan struct{}), make(chan struct{})
	go func() { _, _ = io.Copy(&outBuf, rOut); close(doneOut) }()
	go func() { _, _ = io.Copy(&errBuf, rErr); close(doneErr) }()
	code := fn()
	_ = wOut.Close()
	_ = wErr.Close()
	<-doneOut
	<-doneErr
	_ = rOut.Close()
	_ = rErr.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr
	return code, outBuf.String(), errBuf.String()
}

// TestSync_StderrProgress_Success (PR-6B 6B.3): a happy-path
// runSync against an httptest.Server serving a valid manifest
// MUST emit exactly the four progress lines the spec requires
// — "lofi sync: fetch...", "lofi sync: fetch done",
// "lofi sync: apply...", "lofi sync: apply done" — to stderr
// in that order, and MUST produce no stdout output. No per-track
// progress events are emitted because the Syncer does not
// implement asset orchestration.
func TestSync_StderrProgress_Success(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	const validSHA = "abcdef0123456789abcdef0123456789abcdef01"
	origSHA := catalog.FirstRunCommitSHA
	catalog.FirstRunCommitSHA = validSHA
	t.Cleanup(func() { catalog.FirstRunCommitSHA = origSHA })

	// 1-track catalog: Apply only needs a parseable manifest,
	// not the MVP 4-track contract (LoadFromDir enforces 4, but
	// runSync's happy path stops at Apply).
	body, err := json.Marshal(catalog.Catalog{
		SchemaVersion: catalog.CurrentSchemaVersion,
		Version:       "v1.0",
		Tracks: []catalog.Track{{
			SchemaVersion:   catalog.CurrentSchemaVersion,
			ID:              "track-drizzle",
			Title:           "Slow Rain on a Tin Roof",
			Artist:          "Anonymous",
			License:         catalog.LicenseCCBY,
			LicenseStatus:   catalog.LicenseStatusVerified,
			SourceURL:       "https://archive.org/details/ia-drizzle",
			ChecksumSHA256:  "ebc2689f897aa333887187a499a15658989ca923cbd49ecc8080b6eef955cdc6",
			AttributionText: `"Slow Rain on a Tin Roof" by Anonymous, licensed CC-BY-4.0. Source: archive.org/ia-drizzle`,
			DurationSeconds: 217,
			AudioFilename:   "audio.mp3",
		}},
	})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	withInjectedSyncer(t, func(dir string) *catalog.Syncer {
		s := catalog.NewSyncer(dir)
		s.HTTPClient = &http.Client{Transport: stdoutURLRewriteTransport{target: srv.URL}}
		s.Sleeper = noOpSleeperCatalog{}
		return s
	})

	code, stdout, stderr := captureBoth(t, func() int { return codeFor(runSync(nil)) })
	if code != 0 {
		t.Fatalf("runSync code = %d, want 0; stderr=%q", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout must be empty for happy path, got %q", stdout)
	}
	if hits.Load() != 1 {
		t.Errorf("server hit %d times, want exactly 1 (Fetch+Apply, no retries)", hits.Load())
	}

	// The four spec-mandated progress lines MUST appear on stderr
	// in this exact order. Strings.Contains (not exact-line match)
	// is intentional: runSync may also emit a "cached <url>" line
	// AFTER progress, and any future helper line must not break
	// the test.
	progressFragments := []string{
		"lofi sync: fetch...",
		"lofi sync: fetch done",
		"lofi sync: apply...",
		"lofi sync: apply done",
	}
	cursor := 0
	for _, frag := range progressFragments {
		idx := strings.Index(stderr[cursor:], frag)
		if idx < 0 {
			t.Errorf("missing progress fragment %q in stderr (cursor=%d):\n%s", frag, cursor, stderr)
			continue
		}
		cursor += idx + len(frag)
	}
}

// stdoutURLRewriteTransport mirrors syncer_test.go's
// urlRewriteTransport so the cmd/lofi package can re-point the
// pinned-SHA URL at the test server without exporting the
// internal helper. Kept private to avoid cross-package sharing.
type stdoutURLRewriteTransport struct {
	target string
}

func (t stdoutURLRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	raw := req.URL.Query().Get("srv")
	if raw == "" {
		raw = t.target
	}
	cloned := req.Clone(req.Context())
	cloned.URL = &url.URL{
		Scheme: "http",
		Host:   strings.TrimPrefix(raw, "http://"),
		Path:   "/",
	}
	cloned.RequestURI = ""
	return http.DefaultTransport.RoundTrip(cloned)
}
