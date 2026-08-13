package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
	"github.com/asolis87/lo-fi-player/internal/tui"
)

// resumePromptText es el unico texto que runInteractiveResume
// escribe a stdout antes de leer stdin.
const resumePromptText = "Resume previous session? [y/N]: "

// loadCatalogForResume provee el catalogo para la sesion
// interactiva. nil degrada a ModeNoCatalog (CATALOG-1).
var loadCatalogForResume = func() (*catalog.Catalog, error) {
	root, err := catalogCacheDir()
	if err != nil {
		return nil, err
	}
	cat, err := catalog.LoadFromDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return cat, nil
}

// loadStateForResume devuelve el PlaybackState persistido o nil
// si no hay estado previo (caller omite el prompt).
var loadStateForResume = config.LoadPlaybackState

// stdinIsTTY detecta terminal interactiva via os.Stdin.Stat()
// para evitar promover github.com/charmbracelet/x/term.
var stdinIsTTY = func() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// resumeReader es el io.Reader del que resumePrompter lee.
var resumeReader io.Reader = os.Stdin

// interactiveMpvProbe is the package-level seam the interactive
// selector passes to audio.Select via WithMpvProbe. Production
// uses exec.LookPath on "mpv"; tests swap it to a deterministic
// function so the real audio.Select code path runs with a known
// mpv-found outcome and the factory seam can be exercised
// causally (probe=true → factory invoked; probe=false →
// procedural fallback attempted).
var interactiveMpvProbe = func() (string, bool) {
	p, err := exec.LookPath("mpv")
	if err != nil {
		return "", false
	}
	return p, true
}

// resumePrompter escribe el prompt y lee un byte. y/Y -> resume;
// n/N/EOF -> decline. Errores distintos de EOF se propagan.
var resumePrompter = func(w io.Writer, r io.Reader) (bool, error) {
	if _, err := fmt.Fprint(w, resumePromptText); err != nil {
		return false, err
	}
	buf := make([]byte, 1)
	n, err := r.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	switch buf[0] {
	case 'y', 'Y':
		return true, nil
	}
	return false, nil
}

// newRainBackend es el seam que SwapToRain usa para construir la
// lluvia procedural. Produccion: ProceduralBackend + RainGenerator.
// Tests inyectan un stub que devuelve un MockBackend determinista.
var newRainBackend = func() (audio.AudioBackend, error) {
	b := audio.NewProceduralBackend(
		audio.WithGenerator(audio.NewRainGenerator(44100)),
		audio.WithSampleRate(44100),
	)
	if b == nil {
		return nil, fmt.Errorf("lofi play: procedural rain backend unavailable")
	}
	return b, nil
}

// interactiveAudioSession owns the AudioBackend that the
// interactive TUI plays through, plus the mode label the TUI
// displays. Close uses sync.Once so the launcher-failure path
// and the normal-return path both call backend.Close() exactly
// once even though they share the same cleanup code. SwapToRain
// reemplaza el backend vigente por procedural:rain en respuesta a
// un EventError irrecuperable (EVT-2 / S-EVT-2-sticky); el backend
// viejo se cierra tras el swap para que Close() final solo afecte
// al backend vigente.
type interactiveAudioSession struct {
	backend audio.AudioBackend
	mode    string
	once    sync.Once
	err     error
}

// newInteractiveAudioSession wraps a freshly-selected backend
// with the mode label derived from its concrete type.
func newInteractiveAudioSession(b audio.AudioBackend) *interactiveAudioSession {
	return &interactiveAudioSession{backend: b, mode: audioModeFor(b)}
}

// Backend returns the AudioBackend the TUI plays through.
func (s *interactiveAudioSession) Backend() audio.AudioBackend { return s.backend }

// Mode returns the user-facing audio mode label for the active
// backend. Empty string means "unknown" (TUI renders that).
func (s *interactiveAudioSession) Mode() string { return s.mode }

// Close releases the backend exactly once. Subsequent calls
// return the original error and do not double-close. Non-nil
// errors that are not ErrBackendUnavailable are surfaced to
// stderr so the user sees them, mirroring the headless
// playBackend defer contract. Un swap previo (SwapToRain) puede
// haber dejado el puntero en nil momentaneamente; en ese caso
// Close no intenta cerrar dos veces.
func (s *interactiveAudioSession) Close() error {
	s.once.Do(func() {
		b := s.backend
		s.backend = nil
		if b == nil {
			return
		}
		err := b.Close()
		if err != nil && !errors.Is(err, audio.ErrBackendUnavailable) {
			fmt.Fprintf(os.Stderr, "lofi play: close backend: %v\n", err)
		}
		s.err = err
	})
	return s.err
}

// SwapToRain cierra el backend vigente y lo reemplaza por
// procedural:rain. El backend viejo se cierra tras instalar el
// nuevo, asi Close() final solo afecta al backend vigente. La
// composicion entrega el resultado a la TUI via RebindBackend;
// errores no fatales de Close del backend viejo se registran a
// stderr y el swap continua.
func (s *interactiveAudioSession) SwapToRain() (audio.AudioBackend, string, error) {
	rain, err := newRainBackend()
	if err != nil {
		return nil, "", err
	}
	old := s.backend
	s.backend = rain
	s.mode = "procedural:rain"
	if old != nil {
		if cerr := old.Close(); cerr != nil && !errors.Is(cerr, audio.ErrBackendUnavailable) {
			fmt.Fprintf(os.Stderr, "lofi play: close backend before rain fallback: %v\n", cerr)
		}
	}
	return rain, s.mode, nil
}

// openInteractiveSession asks the package-level selector for an
// AudioBackend (production wiring: audio.Select with the
// interactiveMpvProbe + mpvBackendFactory seams). The session
// owns the backend for its lifetime. Errors propagate to the
// caller so it can render actionable stderr without invoking
// the TUI launcher.
func openInteractiveSession(ctx context.Context) (*interactiveAudioSession, error) {
	backend, err := selectAudioBackend(ctx,
		audio.WithMpvProbe(interactiveMpvProbe),
		audio.WithMpvFactory(mpvBackendFactory),
	)
	if err != nil {
		return nil, err
	}
	return newInteractiveAudioSession(backend), nil
}

// reportNoInteractiveBackend formats the actionable stderr
// message the user sees when no audio backend can be acquired.
// Always returns &commandError{code: 1}.
func reportNoInteractiveBackend(_ error) error {
	fmt.Fprintln(os.Stderr, "lofi play: no audio backend available; install mpv for full audio or use `procedural:rain` for ambient noise")
	return &commandError{code: 1}
}

// audioModeFor returns the user-facing label for the concrete
// backend the selector returned. Concrete adapter types stay in
// the audio package; the TUI receives only the string.
func audioModeFor(b audio.AudioBackend) string {
	switch b.(type) {
	case *audio.MpvBackend:
		return "mpv"
	case *audio.ProceduralBackend:
		return "procedural:rain"
	}
	return ""
}

// interactiveTrackPath returns the on-disk audio path for a
// catalog track id. Empty string for unknown ids so the TUI can
// surface a "no audio file" hint via LastError without leaking
// cache layout into internal/tui.
func interactiveTrackPath(cat *catalog.Catalog, id string) string {
	if cat == nil || id == "" {
		return ""
	}
	for i := range cat.Tracks {
		if cat.Tracks[i].ID == id {
			root, err := catalogCacheDir()
			if err != nil {
				return ""
			}
			return filepath.Join(root, id, "audio.mp3")
		}
	}
	return ""
}

// runInteractiveResume es la rama sin argumentos de `lofi play`:
// RESUME-1 TTY prompt, RESUME-2 non-TTY auto-hydrate, o silencio
// si no hay estado previo. La rama headless (`<id>`) sigue en
// play.go via runHeadlessPlay (explicit-id-wins).
func runInteractiveResume() error {
	cat, _ := loadCatalogForResume()
	state := loadStateForResume()
	hydrated, err := decideResumeState(state, cat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: resume: %v\n", err)
		return &commandError{code: 1}
	}
	session, err := openInteractiveSession(context.Background())
	if err != nil {
		return reportNoInteractiveBackend(err)
	}
	defer func() { _ = session.Close() }()
	m := tui.NewModelWithState(session.Backend(), cat, hydrated, nil)
	m.AudioMode = session.Mode()
	m.ResolvePath = func(id string) string { return interactiveTrackPath(cat, id) }
	m.RebindBackend = func() (audio.AudioBackend, string, error) { return session.SwapToRain() }
	if err := launchTUI(m); err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: tui: %v\n", err)
		return &commandError{code: 1}
	}
	return nil
}

// decideResumeState: RESUME-1/2 + FILTER-1/2. Filtra historia (FILTER-1);
// declinacion conserva volumen y vacia historia (RESUME-1 s2); volume-only
// hidrata sin prompt (PERSIST-1).
func decideResumeState(state *config.PlaybackState, cat *catalog.Catalog) (*config.PlaybackState, error) {
	if state == nil {
		return nil, nil
	}
	resume := true
	if stdinIsTTY() {
		var err error
		resume, err = resumePrompter(os.Stdout, resumeReader)
		if err != nil {
			return nil, err
		}
	}
	hydrated := config.Default()
	hydrated.Volume = state.EffectiveVolume()
	if resume {
		hydrated.History = filterHistoryAgainstCatalog(state.History(), cat)
	}
	return config.NewPlaybackState(hydrated), nil
}
