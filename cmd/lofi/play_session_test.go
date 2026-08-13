package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
)

func withInteractiveMpvProbeStub(t *testing.T, fn func() (string, bool)) {
	t.Helper()
	orig := interactiveMpvProbe
	interactiveMpvProbe = fn
	t.Cleanup(func() { interactiveMpvProbe = orig })
}

func withMpvBackendFactoryStub(t *testing.T, fn func() (audio.AudioBackend, error)) {
	t.Helper()
	orig := mpvBackendFactory
	mpvBackendFactory = fn
	t.Cleanup(func() { mpvBackendFactory = orig })
}

// withSelectAudioBackendStub swaps the package-level selector
// for fn. Tests use it ONLY to inject extra SelectOptions (e.g.
// WithProceduralFactory) into the real audio.Select call when
// the no-backend path needs a deterministic procedural failure
// without standing up a real MPV-less audio device.
func withSelectAudioBackendStub(t *testing.T, fn func(ctx context.Context, opts ...audio.SelectOption) (audio.AudioBackend, error)) {
	t.Helper()
	orig := selectAudioBackend
	selectAudioBackend = fn
	t.Cleanup(func() { selectAudioBackend = orig })
}

// stubInteractiveSession wires the common probe + factory +
// launcher + catalog + state seams for tests that want the
// real audio.Select to produce a mock backend.
func stubInteractiveSession(t *testing.T, factoryFn func() (audio.AudioBackend, error)) *stubLauncher {
	t.Helper()
	launcher := &stubLauncher{err: nil}
	withStubLauncher(t, launcher)
	withInteractiveMpvProbeStub(t, func() (string, bool) { return "/usr/bin/mpv", true })
	withMpvBackendFactoryStub(t, factoryFn)
	stubResumeContext(t)
	return launcher
}

// stubResumeContext wires the no-state, one-track catalog +
// nil-state seams every test in this file needs.
func stubResumeContext(t *testing.T) {
	t.Helper()
	catalogForResumeStub(t, func() (*catalog.Catalog, error) {
		return &catalog.Catalog{Tracks: []catalog.Track{{ID: "A"}}}, nil
	})
	withLoadStateForResumeStub(t, func() *config.PlaybackState { return nil })
}

// TestInteractiveComposition_SelectsBackendViaMpvFactory: the
// production root MUST call audio.Select with the
// interactiveMpvProbe + mpvBackendFactory seams wired in. The
// real audio.Select code path runs end-to-end; probe=true
// (stubbed) + factory non-nil (production wiring) -> real Select
// invokes the factory -> mock reaches TUI.
func TestInteractiveComposition_SelectsBackendViaMpvFactory(t *testing.T) {
	mock := audio.NewMockBackend()
	var factoryCalls int32
	stub := stubInteractiveSession(t, func() (audio.AudioBackend, error) {
		atomic.AddInt32(&factoryCalls, 1)
		return mock, nil
	})
	code, stderr := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 0 {
		t.Fatalf("runPlay(nil) code = %d, want 0 (stderr=%q)", code, stderr)
	}
	if atomic.LoadInt32(&factoryCalls) != 1 {
		t.Fatalf("mpvBackendFactory called %d times, want 1 (real audio.Select invoked it via the WithMpvFactory option)", factoryCalls)
	}
	if !stub.called {
		t.Fatalf("TUI launcher not called")
	}
	if stub.model.Backend != audio.AudioBackend(mock) {
		t.Fatalf("TUI Model.Backend = %p, want %p (the backend the factory produced)", stub.model.Backend, mock)
	}
}

// TestInteractiveComposition_NoBackendExitsWithoutLauncher:
// probe=false + procedural factory failure -> runPlay exits 1
// with actionable stderr AND does not invoke the TUI launcher.
func TestInteractiveComposition_NoBackendExitsWithoutLauncher(t *testing.T) {
	launcher := &stubLauncher{err: nil}
	withStubLauncher(t, launcher)
	withInteractiveMpvProbeStub(t, func() (string, bool) { return "", false })
	withSelectAudioBackendStub(t, func(ctx context.Context, opts ...audio.SelectOption) (audio.AudioBackend, error) {
		opts = append(opts, audio.WithProceduralFactory(func() (audio.AudioBackend, error) {
			return nil, errors.New("simulated procedural failure")
		}))
		return audio.Select(ctx, opts...)
	})
	var factoryCalls int32
	withMpvBackendFactoryStub(t, func() (audio.AudioBackend, error) {
		atomic.AddInt32(&factoryCalls, 1)
		return nil, errors.New("factory should not be invoked when probe is false")
	})
	stubResumeContext(t)

	code, stderr := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 1 {
		t.Fatalf("runPlay(nil) with no-backend selector code = %d, want 1 (stderr=%q)", code, stderr)
	}
	if atomic.LoadInt32(&factoryCalls) != 0 {
		t.Fatalf("mpvBackendFactory invoked %d times; probe=false must skip it", factoryCalls)
	}
	if launcher.called {
		t.Fatalf("TUI launcher called despite selector failure; no-leak contract violated")
	}
	if !strings.Contains(stderr, "no audio backend") || !strings.Contains(stderr, "install mpv") {
		t.Fatalf("stderr missing actionable guidance, got %q", stderr)
	}
}

// TestInteractiveSession_ClosesExactlyOnce: the session owner
// MUST Close the backend exactly once across the normal and
// launcher-failure exit paths.
func TestInteractiveSession_ClosesExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		launchErr error
		wantCode  int
	}{{"NormalReturn", nil, 0}, {"LauncherFailure", errStubTUI, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			mock := audio.NewMockBackend()
			launcher := &stubLauncher{err: tc.launchErr}
			withStubLauncher(t, launcher)
			withInteractiveMpvProbeStub(t, func() (string, bool) { return "/usr/bin/mpv", true })
			withMpvBackendFactoryStub(t, func() (audio.AudioBackend, error) { return mock, nil })
			stubResumeContext(t)

			code, _ := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d", code, tc.wantCode)
			}
			if !launcher.called {
				t.Fatalf("launcher not called")
			}
			if launcher.model.Backend != audio.AudioBackend(mock) {
				t.Fatalf("launcher.model.Backend = %p, want %p (the backend the selector returned)", launcher.model.Backend, mock)
			}
			if !mock.Closed() {
				t.Fatalf("MockBackend.Closed = false; session must Close exactly once")
			}
		})
	}
}

// closeErrBackend wraps a MockBackend and returns a fixed error
// from Close so the session's stderr-visibility contract can be
// tested without a real adapter.
type closeErrBackend struct {
	inner *audio.MockBackend
	err   error
	calls *atomic.Int32
}

func (c *closeErrBackend) Load(t audio.Track) error  { return c.inner.Load(t) }
func (c *closeErrBackend) Play() error               { return c.inner.Play() }
func (c *closeErrBackend) Pause() error              { return c.inner.Pause() }
func (c *closeErrBackend) Stop() error               { return c.inner.Stop() }
func (c *closeErrBackend) SetVolume(v int) error     { return c.inner.SetVolume(v) }
func (c *closeErrBackend) Seek(ms int) error         { return c.inner.Seek(ms) }
func (c *closeErrBackend) State() (bool, int, error) { return c.inner.State() }
func (c *closeErrBackend) Close() error              { c.calls.Add(1); return c.err }

// TestInteractiveSession_LogsCloseError: non-ErrBackendUnavailable
// close errors MUST be surfaced to stderr (mirrors the headless
// playBackend defer contract). sync.Once must still guarantee
// exactly one Close call.
func TestInteractiveSession_LogsCloseError(t *testing.T) {
	wrapper := &closeErrBackend{inner: audio.NewMockBackend(), err: errors.New("disk write failed"), calls: &atomic.Int32{}}
	launcher := &stubLauncher{err: nil}
	withStubLauncher(t, launcher)
	withInteractiveMpvProbeStub(t, func() (string, bool) { return "/usr/bin/mpv", true })
	withMpvBackendFactoryStub(t, func() (audio.AudioBackend, error) { return wrapper, nil })
	stubResumeContext(t)

	code, stderr := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if wrapper.calls.Load() != 1 {
		t.Fatalf("Close called %d times, want 1 (sync.Once)", wrapper.calls.Load())
	}
	if !strings.Contains(stderr, "close backend") || !strings.Contains(stderr, "disk write failed") {
		t.Fatalf("stderr missing close error visibility, got %q", stderr)
	}
}

// TestRunInteractiveResume_ResolvesPath: ResolvePath injected
// into the TUI model MUST return the on-disk audio path of a
// catalog id (cache root / <id> / audio.mp3). Empty string for
// unknown ids.
func TestRunInteractiveResume_ResolvesPath(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	mock := audio.NewMockBackend()
	stub := stubInteractiveSession(t, func() (audio.AudioBackend, error) { return mock, nil })
	if _, _ = captureStderr(t, func() int { return codeFor(runPlay(nil)) }); !stub.called {
		t.Fatalf("launcher not called")
	}
	if stub.model.ResolvePath == nil {
		t.Fatalf("TUI Model.ResolvePath = nil; composition root must inject the resolver")
	}
	want := filepath.Join(cacheRoot, "lofi-player", "catalog", "v1", "A", "audio.mp3")
	if got := stub.model.ResolvePath("A"); got != want {
		t.Fatalf("ResolvePath(A) = %q, want %q", got, want)
	}
	if got := stub.model.ResolvePath("missing"); got != "" {
		t.Fatalf("ResolvePath(missing) = %q, want \"\"", got)
	}
}

// TestAudioModeFor: the mode label exposed to the TUI is derived
// from the concrete backend type without leaking the adapter
// into the TUI. mpv -> "mpv"; procedural -> "procedural:rain";
// everything else -> "".
func TestAudioModeFor(t *testing.T) {
	if got := audioModeFor(audio.NewMockBackend()); got != "" {
		t.Fatalf("audioModeFor(MockBackend) = %q, want \"\"", got)
	}
	proc := audio.NewProceduralBackend(audio.WithGenerator(audio.NewRainGenerator(44100)), audio.WithSampleRate(44100))
	t.Cleanup(func() { _ = proc.Close() })
	if got := audioModeFor(proc); got != "procedural:rain" {
		t.Fatalf("audioModeFor(ProceduralBackend) = %q, want \"procedural:rain\"", got)
	}
}

// TestRunInteractiveResume_ReportsModeToTUI: the composition
// wires the audio mode derived from the selected backend into
// the TUI model (FBK-1). Procedural -> "procedural:rain".
func TestRunInteractiveResume_ReportsModeToTUI(t *testing.T) {
	proc := audio.NewProceduralBackend(audio.WithGenerator(audio.NewRainGenerator(44100)), audio.WithSampleRate(44100))
	t.Cleanup(func() { _ = proc.Close() })
	launcher := &stubLauncher{err: nil}
	withStubLauncher(t, launcher)
	withInteractiveMpvProbeStub(t, func() (string, bool) { return "", false })
	withMpvBackendFactoryStub(t, func() (audio.AudioBackend, error) { return nil, errors.New("not used") })
	withSelectAudioBackendStub(t, func(_ context.Context, _ ...audio.SelectOption) (audio.AudioBackend, error) {
		// probe=false forces Select to skip the factory; we
		// return proc directly so no real audio device runs.
		return proc, nil
	})
	stubResumeContext(t)

	if _, _ = captureStderr(t, func() int { return codeFor(runPlay(nil)) }); launcher.model.AudioMode != "procedural:rain" {
		t.Fatalf("TUI Model.AudioMode = %q, want %q", launcher.model.AudioMode, "procedural:rain")
	}
}

// withNewRainBackendStub swaps el seam newRainBackend que SwapToRain
// usa para construir la lluvia procedural.
func withNewRainBackendStub(t *testing.T, fn func() (audio.AudioBackend, error)) {
	t.Helper()
	orig := newRainBackend
	newRainBackend = fn
	t.Cleanup(func() { newRainBackend = orig })
}

// TestInteractiveSession_SwapToRainClosesOldAndReturnsNew cubre la
// pieza de composicion de EVT-2: SwapToRain cierra el backend
// vigente, instala procedural:rain via el seam newRainBackend y
// devuelve el nuevo backend. Tras el swap, Close() cierra SOLO el
// backend vigente (rain), no el viejo. El segundo Close sigue
// siendo idempotente via sync.Once.
func TestInteractiveSession_SwapToRainClosesOldAndReturnsNew(t *testing.T) {
	old, rain := audio.NewMockBackend(), audio.NewMockBackend()
	session := newInteractiveAudioSession(old)
	withNewRainBackendStub(t, func() (audio.AudioBackend, error) { return rain, nil })

	gotBackend, gotMode, err := session.SwapToRain()
	if err != nil || gotBackend != audio.AudioBackend(rain) || gotMode != "procedural:rain" {
		t.Fatalf("SwapToRain = (%v, %q, %v), want (%p, procedural:rain, nil)", gotBackend, gotMode, err, rain)
	}
	if !old.Closed() || session.Mode() != "procedural:rain" || session.Backend() != audio.AudioBackend(rain) {
		t.Fatalf("estado tras swap: oldClosed=%v mode=%q backend=%v", old.Closed(), session.Mode(), session.Backend())
	}
	if err := session.Close(); err != nil || !rain.Closed() {
		t.Fatalf("Close() tras swap: err=%v rainClosed=%v", err, rain.Closed())
	}
	if err := session.Close(); err != nil {
		t.Fatalf("segundo Close no es idempotente: %v", err)
	}
}

// TestRunInteractiveResume_WiresRebindBackend cubre el cableado:
// runPlay(nil) inyecta RebindBackend en el Model de la TUI para que
// el EventError path pueda invocar SwapToRain.
func TestRunInteractiveResume_WiresRebindBackend(t *testing.T) {
	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)
	withInteractiveMpvProbeStub(t, func() (string, bool) { return "/usr/bin/mpv", true })
	withMpvBackendFactoryStub(t, func() (audio.AudioBackend, error) { return audio.NewMockBackend(), nil })
	stubResumeContext(t)
	if _, _ = captureStderr(t, func() int { return codeFor(runPlay(nil)) }); !stub.called || stub.model.RebindBackend == nil {
		t.Fatalf("called=%v RebindBackend nil=%v; composition debe inyectar SwapToRain", stub.called, stub.model.RebindBackend == nil)
	}
}
