package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/tui"
)

// stubWaitForSignal replaces the package-level waitForSignal so
// the headless dispatch path returns immediately. Without this
// the real implementation blocks on SIGINT/SIGTERM and tests
// never finish.
func stubWaitForSignal(t *testing.T) {
	t.Helper()
	orig := waitForSignal
	waitForSignal = func() error { return nil }
	t.Cleanup(func() { waitForSignal = orig })
}

// writeHeadlessCatalog seeds a single track-rain/track.json under
// the test's XDG cache root so the "unknown id" path can run.
func writeHeadlessCatalog(t *testing.T) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "lofi-player", "catalog", "v1", "track-rain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	tr := catalog.Track{
		SchemaVersion:   catalog.CurrentSchemaVersion,
		ID:              "track-rain",
		Title:           "Heavy Rain",
		Artist:          "Anonymous",
		License:         catalog.LicenseCCBY,
		LicenseStatus:   catalog.LicenseStatusVerified,
		SourceURL:       "https://example.test/track-rain",
		ChecksumSHA256:  "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12",
		AttributionText: "by Anonymous",
		DurationSeconds: 60,
		AudioFilename:   "audio.mp3",
	}
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "track.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestPlay_HeadlessUnknownIDExits1 guards REQ-CLI-2: when
// `lofi play <id>` is invoked and the id is not in the catalog,
// the binary MUST exit 1 with an actionable message naming the
// offending id.
func TestPlay_HeadlessUnknownIDExits1(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeHeadlessCatalog(t)
	stubWaitForSignal(t)

	code, stderr := captureStderr(t, func() int {
		return codeFor(runPlay([]string{"track-unknown"}))
	})
	if code != 1 {
		t.Fatalf("runPlay([track-unknown]) code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "track-unknown") {
		t.Fatalf("expected stderr to mention the unknown id, got %q", stderr)
	}
}

// TestPlay_HeadlessProceduralRainWritesAudioToDevice replaces
// the previous TestPlay_HeadlessProceduralStation, which only
// asserted "no error" from runPlay and therefore would have
// passed even when the procedural backend was silent (the
// pre-PR-14 bug). The new test reconstructs the same backend
// runPlay builds for `lofi play procedural:rain` and threads a
// fake Device through WithDevice so the assertion can verify
// the pump actually delivered samples to a sink — without
// touching the immutable play.go.
func TestPlay_HeadlessProceduralRainWritesAudioToDevice(t *testing.T) {
	gen := resolveProcedural("procedural:rain")
	if gen == nil {
		t.Fatalf("resolveProcedural(procedural:rain) = nil")
	}
	if _, ok := gen.(*audio.RainGenerator); !ok {
		t.Fatalf("resolveProcedural(procedural:rain) = %T, want *audio.RainGenerator", gen)
	}

	dev := newRecordingDevice()
	b := audio.NewProceduralBackend(
		audio.WithGenerator(gen),
		audio.WithSampleRate(44100),
		audio.WithDevice(dev),
	)
	if b == nil {
		t.Fatal("NewProceduralBackend returned nil")
	}
	t.Cleanup(func() { _ = b.Close() })

	if err := b.Load(audio.Track{ID: "procedural:rain", Path: "procedural:rain"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for dev.sampleCount() < 2048 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := dev.sampleCount(); got < 2048 {
		t.Fatalf("procedural:rain delivered %d samples to the device, want >= 2048 (the silence bug)", got)
	}
}

// TestPlay_HeadlessProceduralRainStillWorks is the slice-1
// positive case for the Decision #326 procedural gate: rain is
// the one station surfaced to the CLI in slice #1, so
// `lofi play procedural:rain` MUST return a nil error from the
// end-to-end runPlay path.
func TestPlay_HeadlessProceduralRainStillWorks(t *testing.T) {
	stubWaitForSignal(t)

	gen := resolveProcedural("procedural:rain")
	if gen == nil {
		t.Fatalf("resolveProcedural(procedural:rain) = nil")
	}

	code, stderr := captureStderr(t, func() int {
		return codeFor(runPlay([]string{"procedural:rain"}))
	})
	if code != 0 {
		t.Fatalf("runPlay(procedural:rain) code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("runPlay(procedural:rain) stderr = %q, want empty", stderr)
	}
}

// TestPlay_HeadlessProceduralBrownRejects guards Decision #326:
// procedural:brown is NOT exposed in slice #1 even though the
// generator exists in internal/audio. runPlay MUST exit 1 with
// a stderr message that names the station and points the user at
// procedural:rain.
func TestPlay_HeadlessProceduralBrownRejects(t *testing.T) {
	stubWaitForSignal(t)

	code, stderr := captureStderr(t, func() int {
		return codeFor(runPlay([]string{"procedural:brown"}))
	})
	if code != 1 {
		t.Fatalf("runPlay(procedural:brown) code = %d, want 1", code)
	}
	want := []string{
		"procedural:brown",
		"not available in slice #1",
		"procedural:rain",
	}
	for _, s := range want {
		if !strings.Contains(stderr, s) {
			t.Errorf("stderr missing %q in %q", s, stderr)
		}
	}

	// resolveProcedural itself must return nil for brown so the
	// proceduralBackend construction surface stays consistent.
	if gen := resolveProcedural("procedural:brown"); gen != nil {
		t.Errorf("resolveProcedural(procedural:brown) = %T, want nil", gen)
	}
}

// TestPlay_HeadlessProceduralWhiteRejects is the white-noise
// twin of TestPlay_HeadlessProceduralBrownRejects. The contract
// is identical: procedural:white is not exposed in slice #1, so
// runPlay exits 1 with an actionable stderr message and
// resolveProcedural returns nil.
func TestPlay_HeadlessProceduralWhiteRejects(t *testing.T) {
	stubWaitForSignal(t)

	code, stderr := captureStderr(t, func() int {
		return codeFor(runPlay([]string{"procedural:white"}))
	})
	if code != 1 {
		t.Fatalf("runPlay(procedural:white) code = %d, want 1", code)
	}
	want := []string{
		"procedural:white",
		"not available in slice #1",
		"procedural:rain",
	}
	for _, s := range want {
		if !strings.Contains(stderr, s) {
			t.Errorf("stderr missing %q in %q", s, stderr)
		}
	}

	if gen := resolveProcedural("procedural:white"); gen != nil {
		t.Errorf("resolveProcedural(procedural:white) = %T, want nil", gen)
	}
}

// TestPlay_HeadlessProceduralUnknownExits1 covers the negative
// path of the procedural prefix: `lofi play procedural:bogus`
// MUST exit 1 and name the offending station.
func TestPlay_HeadlessProceduralUnknownExits1(t *testing.T) {
	stubWaitForSignal(t)

	code, stderr := captureStderr(t, func() int {
		return codeFor(runPlay([]string{"procedural:bogus"}))
	})
	if code != 1 {
		t.Fatalf("runPlay([procedural:bogus]) code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "procedural:bogus") {
		t.Fatalf("expected stderr to mention the bogus station, got %q", stderr)
	}
}

// TestPlay_NoCatalogExits1WithSyncHint covers the missing-cache
// path: when the catalog directory does not exist, runPlay MUST
// exit 1 and surface a hint that points at `lofi sync` so the
// user can recover without reading the source.
func TestPlay_NoCatalogExits1WithSyncHint(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	stubWaitForSignal(t)

	code, stderr := captureStderr(t, func() int {
		return codeFor(runPlay([]string{"track-anything"}))
	})
	if code != 1 {
		t.Fatalf("runPlay(no catalog) code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "lofi sync") {
		t.Fatalf("expected lofi sync hint, got %q", stderr)
	}
}

// TestPlay_LaunchesTUIWhenNoArgs is the canonical TUI-launching
// path: `lofi play` with no arguments MUST invoke the launcher
// and hand it a Model whose Backend is non-nil and whose Mode
// is ModeNowPlaying. The stub launcher captures the Model so
// the assertion does not need a real TTY.
func TestPlay_LaunchesTUIWhenNoArgs(t *testing.T) {
	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)

	code, _ := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 0 {
		t.Fatalf("runPlay(nil) code = %d, want 0", code)
	}
	if !stub.called {
		t.Fatalf("runPlay(nil) did not invoke the TUI launcher")
	}
	if stub.model.Backend == nil {
		t.Fatalf("TUI Model.Backend is nil; composition root did not wire the audio port")
	}
	if stub.model.Mode != tui.ModeNowPlaying {
		t.Fatalf("TUI Model.Mode = %v, want ModeNowPlaying", stub.model.Mode)
	}
}

// TestPlay_NoArgs_TUIErrorPropagates ensures a non-nil launcher
// error surfaces as exit code 1 so reviewers can distinguish a
// TUI runtime failure from a successful clean exit.
func TestPlay_NoArgs_TUIErrorPropagates(t *testing.T) {
	stub := &stubLauncher{err: errStubTUI}
	withStubLauncher(t, stub)

	code, stderr := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 1 {
		t.Fatalf("runPlay(nil) with failing launcher: code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "tui") {
		t.Fatalf("expected TUI failure surfaced to stderr, got %q", stderr)
	}
}

// TestResolveProcedural_KnownStations exhaustively locks down
// the procedural: -> SampleGenerator mapping. Per Decision #326
// the slice-1 CLI surface only exposes procedural:rain; the
// generators for brown and white still exist in internal/audio
// but resolveProcedural and runPlay MUST refuse them so the
// surfaced contract is exactly the table below.
func TestResolveProcedural_KnownStations(t *testing.T) {
	cases := []struct {
		station string
		wantErr bool
		kind    string
	}{
		{"procedural:rain", false, "rain"},
		{"procedural:brown", true, ""},
		{"procedural:white", true, ""},
		{"procedural:bogus", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.station, func(t *testing.T) {
			gen := resolveProcedural(tc.station)
			if tc.wantErr {
				if gen != nil {
					t.Fatalf("resolveProcedural(%q) = %T, want nil", tc.station, gen)
				}
				return
			}
			if gen == nil {
				t.Fatalf("resolveProcedural(%q) = nil", tc.station)
			}
		})
	}
}

// recordingDevice is the test-only Device the procedural CLI
// tests use to verify the pump actually delivered samples. It
// lives in the cmd/lofi package (not internal/audio) because the
// fakeDevice there is package-private; the interface matches
// audio.Device so the procedural backend can use it through
// WithDevice.
type recordingDevice struct {
	mu      sync.Mutex
	written []int16
}

func newRecordingDevice() *recordingDevice        { return &recordingDevice{} }
func (r *recordingDevice) Write(s []int16) error  { r.mu.Lock(); defer r.mu.Unlock(); r.written = append(r.written, s...); return nil }
func (r *recordingDevice) Close() error           { return nil }
func (r *recordingDevice) sampleCount() int       { r.mu.Lock(); defer r.mu.Unlock(); return len(r.written) }