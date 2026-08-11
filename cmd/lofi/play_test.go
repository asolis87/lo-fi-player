package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestPlay_HeadlessProceduralStation guards the procedural path:
// `lofi play procedural:rain|brown|white` MUST build a
// ProceduralBackend with the matching SampleGenerator and play
// it through the AudioBackend port without touching audio.Select.
// This is the headless fallback the spec calls for when no
// shipped track is wanted.
func TestPlay_HeadlessProceduralStation(t *testing.T) {
	stubWaitForSignal(t)

	cases := []struct {
		station string
		wantGen interface {
			Next(int) []int16
			Reset()
		}
		check func(t *testing.T)
	}{
		{
			station: "procedural:rain",
			wantGen: &audio.RainGenerator{},
			check: func(t *testing.T) {
				// type checked at the bottom via the wrapper
			},
		},
		{
			station: "procedural:brown",
			wantGen: &audio.BrownNoiseGenerator{},
			check: func(t *testing.T) {},
		},
		{
			station: "procedural:white",
			wantGen: &audio.WhiteNoiseGenerator{},
			check: func(t *testing.T) {},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.station, func(t *testing.T) {
			gen := resolveProcedural(tc.station)
			if gen == nil {
				t.Fatalf("resolveProcedural(%q) = nil", tc.station)
			}
			// Concrete type check via a type switch keeps the
			// test resilient to future SampleGenerator additions.
			switch tc.wantGen.(type) {
			case *audio.RainGenerator:
				if _, ok := gen.(*audio.RainGenerator); !ok {
					t.Fatalf("resolveProcedural(%q) = %T, want *audio.RainGenerator", tc.station, gen)
				}
			case *audio.BrownNoiseGenerator:
				if _, ok := gen.(*audio.BrownNoiseGenerator); !ok {
					t.Fatalf("resolveProcedural(%q) = %T, want *audio.BrownNoiseGenerator", tc.station, gen)
				}
			case *audio.WhiteNoiseGenerator:
				if _, ok := gen.(*audio.WhiteNoiseGenerator); !ok {
					t.Fatalf("resolveProcedural(%q) = %T, want *audio.WhiteNoiseGenerator", tc.station, gen)
				}
			}

			// End-to-end through runPlay so the full wiring
			// (procedural prefix detection, backend construction,
			// Load, Play, signal-wait stub, Close) is exercised.
			if err := runPlay([]string{tc.station}); err != nil {
				t.Fatalf("runPlay(%q): %v", tc.station, err)
			}
		})
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
// the procedural: -> SampleGenerator mapping. Adding a new
// station requires updating this table so dispatchers can rely
// on the contract.
func TestResolveProcedural_KnownStations(t *testing.T) {
	cases := []struct {
		station string
		wantErr bool
		kind    string
	}{
		{"procedural:rain", false, "rain"},
		{"procedural:brown", false, "brown"},
		{"procedural:white", false, "white"},
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