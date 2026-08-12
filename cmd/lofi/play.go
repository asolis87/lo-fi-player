package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

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

// tuiLauncher runs the Bubble Tea program. Tests swap it for a
// stub so they can capture the model without taking over the
// terminal (Bubble Tea's WithInput / WithOutput paths require a
// TTY in some environments).
type tuiLauncher func(m tui.Model) error

// launchTUI is the production launcher. Override in tests only.
var launchTUI tuiLauncher = func(m tui.Model) error {
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

// waitForSignal blocks until the process receives SIGINT or
// SIGTERM. Exposed as a package-level var so tests can swap it
// for an immediate-return stub. EOF on stdin is intentionally NOT
// considered a stop signal — headless playback in scripts
// frequently runs without a controlling terminal, and exiting on
// stdin close would break every `lofi play <id> &` invocation.
var waitForSignal = func() error {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(ch)
	<-ch
	return nil
}

// runPlay dispatches `lofi play`. With no positional argument the
// TUI takes over the terminal (REQ-CLI-2 / REQ-TUI-1); with one
// argument the headless path:
//
//  1. resolves the audio backend (procedural:station or catalog
//     track) per the spec contract, OR
//  2. prints a precise, actionable error to stderr and returns a
//     non-zero commandError.
func runPlay(args []string) error {
	if len(args) == 0 {
		backend := audio.NewMockBackend()
		m := tui.NewModel(backend, nil, nil)
		if err := launchTUI(m); err != nil {
			fmt.Fprintf(os.Stderr, "lofi play: tui: %v\n", err)
			return &commandError{code: 1}
		}
		return nil
	}
	return runHeadlessPlay(args[0])
}

// runHeadlessPlay resolves target into an audio backend and runs
// it through the full Load -> Play -> waitForSignal -> Close
// lifecycle. The catalog is loaded exactly once and reused for
// track lookup; procedural stations skip catalog loading and
// audio.Select entirely per the slice #1 contract.
func runHeadlessPlay(target string) error {
	// REQ-CFG-* still applies: the config is loaded so future
	// releases can persist last-queue / last-track-index across
	// headless invocations without re-plumbing this seam. For
	// slice #1 we only need it to be loadable; persistence is
	// PR #10+ work.
	_ = config.LoadOrDefault()

	backend, audioPath, err := resolveHeadlessBackend(target)
	if err != nil {
		return err
	}
	return playBackend(backend, target, audioPath)
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

// playBackend runs the AudioBackend lifecycle: Load with a Track
// whose ID matches the user request and whose Path is the
// resolved on-disk audio file (empty for procedural targets,
// whose backends synthesize samples). Play starts the stream;
// waitForSignal blocks until SIGINT/SIGTERM; Close releases
// resources.
func playBackend(backend audio.AudioBackend, trackID, audioPath string) error {
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
	return waitForSignal()
}