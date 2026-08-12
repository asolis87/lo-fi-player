package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
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
// the test's XDG cache root so the "unknown id" path can run. The
// audio.mp3 sibling is the deterministic payload the checksum
// expects so PR-D #3.1 (audio.mp3 required) and PR-D #3.2
// (checksum verified at load) both stay green.
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
		ChecksumSHA256:  "ebc2689f897aa333887187a499a15658989ca923cbd49ecc8080b6eef955cdc6",
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
	if err := os.WriteFile(filepath.Join(dir, "audio.mp3"), []byte("dummy bytes"), 0o644); err != nil {
		t.Fatalf("write audio.mp3: %v", err)
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
// and hand it a Model whose Backend is non-nil. Bajo el contrato
// CATALOG-1 (slice 3) cuando no hay catalogo sembrado (caso por
// defecto de este test), el modo inicial pasa a ModeNoCatalog y
// se exhibe la guia de `lofi sync`. El caso Now-Playing con
// catalogo presente es responsabilidad del flujo de B2 (resume +
// filtros) en su propio PR; este test solo cubre el contrato
// minimo de lanzamiento.
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
	if stub.model.Mode != tui.ModeNoCatalog {
		t.Fatalf("TUI Model.Mode = %v, want ModeNoCatalog (no catalog seeded)", stub.model.Mode)
	}
}

// catalogForResumeStub swaps loadCatalogForResume for fn and
// restores the default on cleanup.
func catalogForResumeStub(t *testing.T, fn func() (*catalog.Catalog, error)) {
	t.Helper()
	orig := loadCatalogForResume
	loadCatalogForResume = fn
	t.Cleanup(func() { loadCatalogForResume = orig })
}

// stateForResumeWithVolumeAndHistory construye un PlaybackState
// minimo sin tocar disco ni XDG_CONFIG_HOME.
func stateForResumeWithVolumeAndHistory(t *testing.T, volume int, history []string) *config.PlaybackState {
	t.Helper()
	cfg := config.Default()
	cfg.Volume = volume
	cfg.History = append([]string{}, history...)
	return config.NewPlaybackState(cfg)
}

// TestResumeTTY_Accept_Hydrates: RESUME-1 s1. TTY + 'y' hidrata
// volumen efectivo y Mode Now-Playing (catalog presente).
func TestResumeTTY_Accept_Hydrates(t *testing.T) {
	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)

	withStdinIsTTYStub(t, true)
	withResumeReaderStub(t, bytes.NewReader([]byte{'y'}))
	withLoadStateForResumeStub(t, func() *config.PlaybackState {
		return stateForResumeWithVolumeAndHistory(t, 72, []string{"A", "B"})
	})
	catalogForResumeStub(t, func() (*catalog.Catalog, error) {
		return &catalog.Catalog{Tracks: []catalog.Track{{ID: "A"}, {ID: "B"}}}, nil
	})

	code, stderr := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 0 {
		t.Fatalf("runPlay(nil) code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if !stub.called || stub.model.Volume != 72 || stub.model.Mode != tui.ModeNowPlaying {
		t.Fatalf("hydrated model wrong: called=%v vol=%d mode=%v", stub.called, stub.model.Volume, stub.model.Mode)
	}
}

// TestResumeTTY_Decline_ClearsHistoryKeepsVolume: RESUME-1 s2.
// Declinacion preserva volumen y vacia historia.
func TestResumeTTY_Decline_ClearsHistoryKeepsVolume(t *testing.T) {
	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)

	withStdinIsTTYStub(t, true)
	withResumeReaderStub(t, bytes.NewReader([]byte{'n'}))
	original := stateForResumeWithVolumeAndHistory(t, 65, []string{"A", "B", "C"})
	withLoadStateForResumeStub(t, func() *config.PlaybackState { return original })
	catalogForResumeStub(t, func() (*catalog.Catalog, error) {
		return &catalog.Catalog{Tracks: []catalog.Track{{ID: "A"}}}, nil
	})

	code, _ := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 0 {
		t.Fatalf("runPlay(nil) code = %d, want 0", code)
	}
	if stub.model.Volume != 65 {
		t.Fatalf("Volume = %d, want 65 (declined but preserved)", stub.model.Volume)
	}
	if len(original.History()) != 3 {
		t.Fatalf("original.History() length = %d, want 3 (declination must not mutate)", len(original.History()))
	}
	dec, err := decideResumeState(original, nil)
	if err != nil || dec == nil || len(dec.History()) != 0 || dec.EffectiveVolume() != 65 {
		t.Fatalf("decideResumeState(decline) wrong: err=%v dec=%v history=%v vol=%d", err, dec, dec.History(), dec.EffectiveVolume())
	}
}

// TestResumeNonTTY_AutoHydratesNoStdinRead: RESUME-2. Non-TTY
// hidrata silenciosamente y NUNCA lee stdin.
func TestResumeNonTTY_AutoHydratesNoStdinRead(t *testing.T) {
	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)

	reader := newCountingReader()
	withStdinIsTTYStub(t, false)
	withResumeReaderStub(t, reader)
	withLoadStateForResumeStub(t, func() *config.PlaybackState {
		return stateForResumeWithVolumeAndHistory(t, 80, []string{"A", "B"})
	})
	catalogForResumeStub(t, func() (*catalog.Catalog, error) {
		return &catalog.Catalog{Tracks: []catalog.Track{{ID: "A"}}}, nil
	})

	code, _ := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 0 || !stub.called || stub.model.Volume != 80 || reader.Calls() != 0 {
		t.Fatalf("non-TTY wrong: code=%d called=%v vol=%d reads=%d", code, stub.called, stub.model.Volume, reader.Calls())
	}
}

// TestExplicitID_BypassesResumePrompt: explicit-id-wins. `lofi
// play <id>` no consulta stdin ni dispara TUI launcher.
func TestExplicitID_BypassesResumePrompt(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeHeadlessCatalog(t)
	stubWaitForSignal(t)

	reader := newCountingReader()
	withStdinIsTTYStub(t, true)
	withResumeReaderStub(t, reader)
	withLoadStateForResumeStub(t, func() *config.PlaybackState {
		return stateForResumeWithVolumeAndHistory(t, 72, []string{"A"})
	})

	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)

	code, stderr := captureStderr(t, func() int { return codeFor(runPlay([]string{"track-rain"})) })
	if code != 0 {
		t.Fatalf("runPlay(track-rain) code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if stub.called || reader.Calls() != 0 {
		t.Fatalf("explicit-id wrong: tui_called=%v stdin_reads=%d", stub.called, reader.Calls())
	}
}

// TestResumeTTY_NoPriorState_NoPrompt: sin estado previo no se
// consulta stdin ni se construye state.
func TestResumeTTY_NoPriorState_NoPrompt(t *testing.T) {
	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)

	reader := newCountingReader()
	withStdinIsTTYStub(t, true)
	withResumeReaderStub(t, reader)
	withLoadStateForResumeStub(t, func() *config.PlaybackState { return nil })
	catalogForResumeStub(t, func() (*catalog.Catalog, error) {
		return &catalog.Catalog{Tracks: []catalog.Track{{ID: "A"}}}, nil
	})

	code, _ := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 0 || !stub.called || reader.Calls() != 0 {
		t.Fatalf("no-prior-state wrong: code=%d called=%v reads=%d", code, stub.called, reader.Calls())
	}
	if stub.model.Volume != config.DefaultVolume {
		t.Fatalf("Volume = %d, want %d (no prior state -> default)", stub.model.Volume, config.DefaultVolume)
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

func newRecordingDevice() *recordingDevice { return &recordingDevice{} }
func (r *recordingDevice) Write(s []int16) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.written = append(r.written, s...)
	return nil
}
func (r *recordingDevice) Close() error     { return nil }
func (r *recordingDevice) sampleCount() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.written) }

// TestRunHeadlessPlay_ResolvesAudioPath is the PR-D #3.3 gate:
// when `lofi play <id>` resolves a catalog track, runHeadlessPlay
// MUST hand backend.Load a Track whose Path points at the actual
// audio file under the cache (<cacheRoot>/<id>/audio.mp3), not
// the empty string that the slice #1 placeholder path produced.
// We override selectAudioBackend to a stub that returns a
// MockBackend so we can inspect what Load received without mpv
// on $PATH or a working procedural fallback.
func TestRunHeadlessPlay_ResolvesAudioPath(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeHeadlessCatalog(t)
	stubWaitForSignal(t)

	mock := audio.NewMockBackend()
	origSelect := selectAudioBackend
	selectAudioBackend = func(ctx context.Context, opts ...audio.SelectOption) (audio.AudioBackend, error) {
		return mock, nil
	}
	t.Cleanup(func() { selectAudioBackend = origSelect })

	code, stderr := captureStderr(t, func() int { return codeFor(runPlay([]string{"track-rain"})) })
	if code != 0 {
		t.Fatalf("runPlay(track-rain) code = %d, want 0, stderr=%q", code, stderr)
	}

	loaded := mock.Loaded()
	if len(loaded) != 1 {
		t.Fatalf("MockBackend.Load called %d times, want 1 (full log: %+v)", len(loaded), loaded)
	}
	wantPath := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "lofi-player", "catalog", "v1", "track-rain", "audio.mp3")
	if loaded[0].Path != wantPath {
		t.Fatalf("Load(Track).Path = %q, want %q (track-rain should resolve to its audio.mp3 under the cache root)", loaded[0].Path, wantPath)
	}
	if loaded[0].ID != "track-rain" {
		t.Fatalf("Load(Track).ID = %q, want %q", loaded[0].ID, "track-rain")
	}
}
