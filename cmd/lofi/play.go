package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
	"github.com/asolis87/lo-fi-player/internal/tui"
)

// selectAudioBackend is the package-level injection seam for
// audio.Select. Production wires it to audio.Select so the
// adapter probe runs; tests swap it for a stub that returns a
// deterministic AudioBackend (typically a MockBackend) so they
// can capture the Track passed to Load without needing mpv on
// $PATH or a working procedural fallback.
var selectAudioBackend = audio.Select

// mpvBackendFactory is the package-level seam the interactive
// selector wires via audio.WithMpvFactory. Production builds a
// real *audio.MpvBackend; tests swap the variable for a function
// that records the call and returns a stub backend so the
// production code path can be exercised without an mpv binary.
var mpvBackendFactory = func() (audio.AudioBackend, error) {
	return audio.NewMpvBackend()
}

// tuiLauncher runs the Bubble Tea program. Production wires
// tea.WithoutSignalHandler so Bubble Tea does NOT install its own
// SIGINT/SIGTERM trap — watchTUISignals owns the OS signal path.
type tuiLauncher func(m tui.Model) error

var launchTUI tuiLauncher = func(m tui.Model) error {
	p := tea.NewProgram(m, tea.WithoutSignalHandler())
	return watchTUISignals(p, m.State, make(chan os.Signal, 1))
}

// watchTUISignals owns SIGINT/SIGTERM for the TUI path (B5 SIGNAL-1 TUI).
func watchTUISignals(p *tea.Program, state *config.PlaybackState, sigCh chan os.Signal) error {
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			p.Quit()
		case <-done:
		}
	}()
	_, err := p.Run()
	close(done)
	cliFinalizeOnce.Do(func() {
		if ferr := cliSignalFinalize(state); ferr != nil {
			fmt.Fprintf(os.Stderr, "lofi play: save state: %v\n", ferr)
		}
	})
	return err
}

// signalCh is the injected OS signal channel; tests swap it for a plain channel.
var signalCh chan os.Signal

// cliSignalFinalize is the sole CLI-side save entry for SIGINT/SIGTERM.
// TUI's q/Ctrl+C is owned by internal/tui.tuiStatePersister.
var cliSignalFinalize = func(state *config.PlaybackState) error {
	if state == nil {
		return nil
	}
	return boundedSave(state)
}

// cliFinalizeOnce guards against double-save races.
var cliFinalizeOnce sync.Once

// boundedSave runs state.Persist under a 5s timeout (PERSIST-2).
func boundedSave(state *config.PlaybackState) error {
	done := make(chan error, 1)
	go func() { done <- state.Persist() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("save state: timeout after 5s")
	}
}

// waitForSignal blocks until SIGINT/SIGTERM.
var waitForSignal = func() error {
	if signalCh == nil {
		signalCh = make(chan os.Signal, 1)
		signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	}
	<-signalCh
	return nil
}

// recordAndPersistOnPlaySuccess: solo cuando backend.Play retorna nil
// se registra el track en MRU y se persiste. Failed Play NO muta
// history. Errores no-fatales.
var recordAndPersistOnPlaySuccess = func(state *config.PlaybackState, trackID string) {
	if state == nil {
		return
	}
	if err := state.RecordPlayed(trackID); err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: save state: %v\n", err)
		return
	}
	if err := state.Persist(); err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: save state: %v\n", err)
	}
}

// runPlay dispatches `lofi play`. With one positional argument the
// headless path takes precedence (RESUME-1 explicit-id-wins). Zero
// args go to runInteractiveResume (see play_resume.go).
func runPlay(args []string) error {
	if len(args) > 0 {
		return runHeadlessPlay(args[0])
	}
	return runInteractiveResume()
}

// runHeadlessPlay: Load -> Play -> RecordPlayed+Persist -> waitForSignal
// -> cliSignalFinalize -> Close. Catalog tracks consultan lock; procedural: rain NO (REQ-MVP-2).
func runHeadlessPlay(target string) error {
	state := config.LoadPlaybackState()
	if !strings.HasPrefix(target, "procedural:") {
		if err := consumerLockGuard("play"); err != nil {
			return err
		}
	}

	backend, audioPath, err := resolveHeadlessBackend(target)
	if err != nil {
		return err
	}
	return playBackend(backend, target, audioPath, state)
}

// resolveHeadlessBackend picks the AudioBackend that will serve
// the headless invocation and the on-disk path of the audio
// bytes (empty for procedural targets). See selectBackendForCatalogTrack
// for the audioPath semantics.
//
//   - target starts with "procedural:": construct a
//     ProceduralBackend directly with the matching generator;
//     audio.Select is intentionally NOT called so a missing mpv
//     cannot break ambient-noise playback. audioPath is "" because
//     the procedural backends synthesize samples, they don't read
//     files.
//   - otherwise: load the on-disk catalog, look up the track by
//     id, and call audio.Select with the mpv factory wired in.
//     A missing catalog surfaces a `lofi sync` hint; a missing
//     track surfaces the offending id; ErrNoAudioBackend surfaces
//     an actionable install hint.
func resolveHeadlessBackend(target string) (audio.AudioBackend, string, error) {
	if strings.HasPrefix(target, "procedural:") {
		b, err := proceduralBackend(target)
		if err != nil {
			return nil, "", err
		}
		return b, "", nil
	}
	return selectBackendForCatalogTrack(target)
}

// resolveProcedural maps a procedural:<station> id to the
// matching SampleGenerator. Returns nil when the station is
// unknown OR not exposed in slice #1 (per Decision #326 the
// slice-1 CLI surface only ships procedural:rain; the brown and
// white generators still exist in internal/audio for future
// slices but are not surfaced yet). Callers translate nil into
// an exit-1 stderr message that distinguishes the two cases.
func resolveProcedural(station string) audio.SampleGenerator {
	switch strings.TrimPrefix(station, "procedural:") {
	case "rain":
		return audio.NewRainGenerator(44100)
	}
	return nil
}

// proceduralBackend constructs the ProceduralBackend for the
// given procedural:<station> id. Construction failures fall into
// two distinct buckets (each exits 1 with a focused message):
//
//  1. The station is explicitly not exposed in slice #1
//     (brown, white). The stderr message names the offending
//     id and points the user at procedural:rain so they can
//     recover without reading source.
//  2. The station is unknown entirely. The stderr message
//     lists the slice-1 supported stations.
//
// A nil NewProceduralBackend construction is its own bucket
// because it would indicate a programming error rather than a
// user-facing rejection.
func proceduralBackend(target string) (audio.AudioBackend, error) {
	station := strings.TrimPrefix(target, "procedural:")
	switch station {
	case "brown", "white":
		fmt.Fprintf(os.Stderr, "lofi play: %s not available in slice #1; use procedural:rain\n", target)
		return nil, &commandError{code: 1}
	}
	gen := resolveProcedural(target)
	if gen == nil {
		fmt.Fprintf(os.Stderr, "lofi play: unknown procedural station %q (want procedural:rain)\n", target)
		return nil, &commandError{code: 1}
	}
	b := audio.NewProceduralBackend(
		audio.WithGenerator(gen),
		audio.WithSampleRate(44100),
	)
	if b == nil {
		fmt.Fprintln(os.Stderr, "lofi play: failed to construct procedural backend")
		return nil, &commandError{code: 1}
	}
	return b, nil
}

// selectBackendForCatalogTrack loads the catalog, looks up the
// track, resolves the on-disk audio path the backend will hand
// to Load, and runs audio.Select with the mpv factory wired in.
// The error mapping mirrors the offline / placeholder / float-ref
// branches of runSync so the user sees the same wording for the
// same root cause.
func selectBackendForCatalogTrack(target string) (audio.AudioBackend, string, error) {
	cacheRoot, err := catalogCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: cannot locate cache dir: %v\n", err)
		return nil, "", &commandError{code: 1}
	}

	cat, err := catalog.LoadFromDir(cacheRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "lofi play: no catalog available; run `lofi sync` first")
		} else {
			fmt.Fprintf(os.Stderr, "lofi play: load catalog: %v\n", err)
		}
		return nil, "", &commandError{code: 1}
	}

	track := findTrackByID(cat, target)
	if track == nil {
		fmt.Fprintf(os.Stderr, "lofi play: unknown track id %q (run `lofi list` to see available ids)\n", target)
		return nil, "", &commandError{code: 1}
	}

	audioPath := filepath.Join(cacheRoot, target, "audio.mp3")
	if _, err := os.Stat(audioPath); err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: audio file missing for track %q (expected %s): %v\n", target, audioPath, err)
		return nil, "", &commandError{code: 1}
	}

	backend, err := selectAudioBackend(context.Background(), audio.WithMpvFactory(func() (audio.AudioBackend, error) {
		return audio.NewMpvBackend()
	}))
	if err != nil {
		if errors.Is(err, audio.ErrNoAudioBackend) {
			fmt.Fprintln(os.Stderr, "lofi play: no audio backend available; install mpv for full audio or use `procedural:rain` for ambient noise")
		} else {
			fmt.Fprintf(os.Stderr, "lofi play: select backend: %v\n", err)
		}
		return nil, "", &commandError{code: 1}
	}
	return backend, audioPath, nil
}

// findTrackByID returns a pointer to the catalog track with the
// given id, or nil if none matches. The catalog is sorted by id
// already (LoadFromDir enforces it) so a linear scan is fine for
// slice #1 sizes.
func findTrackByID(cat *catalog.Catalog, id string) *catalog.Track {
	if cat == nil {
		return nil
	}
	for i := range cat.Tracks {
		if cat.Tracks[i].ID == id {
			return &cat.Tracks[i]
		}
	}
	return nil
}

// playBackend runs the AudioBackend lifecycle. Failed Play MUST NOT
// touch state (B5 failed-Play guard); success records+persists then
// waitForSignal blocks until SIGINT/SIGTERM and cliSignalFinalize
// saves once. state is optional.
func playBackend(backend audio.AudioBackend, trackID, audioPath string, state *config.PlaybackState) error {
	if backend == nil {
		return &commandError{code: 1}
	}
	defer func() {
		if cerr := backend.Close(); cerr != nil && !errors.Is(cerr, audio.ErrBackendUnavailable) {
			fmt.Fprintf(os.Stderr, "lofi play: close backend: %v\n", cerr)
		}
	}()
	if err := backend.Load(audio.Track{ID: trackID, Path: audioPath}); err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: load: %v\n", err)
		return &commandError{code: 1}
	}
	if err := backend.Play(); err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: play: %v\n", err)
		return &commandError{code: 1}
	}
	if state != nil {
		recordAndPersistOnPlaySuccess(state, trackID)
	}
	if err := waitForSignal(); err != nil {
		return err
	}
	cliFinalizeOnce.Do(func() {
		if err := cliSignalFinalize(state); err != nil {
			fmt.Fprintf(os.Stderr, "lofi play: save state: %v\n", err)
		}
	})
	return nil
}
