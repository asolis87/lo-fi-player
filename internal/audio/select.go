package audio

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// ErrNoAudioBackend is returned by Select when neither the mpv
// adapter nor the procedural fallback can produce a usable
// AudioBackend. The accompanying message is user-facing and MUST
// contain actionable guidance (install hint, override knob, etc.).
var ErrNoAudioBackend = errors.New("audio: no audio backend available")

// MpvFactory constructs an AudioBackend that talks to mpv. It is
// injected via WithMpvFactory so Select() can stay decoupled from
// the mpv adapter package: on a build where mpv.go is not linked
// the factory remains nil and Select falls straight through to the
// procedural backend. cmd/lofi (post-PR-3) wires the real factory.
type MpvFactory func() (AudioBackend, error)

// ProceduralFactory constructs the fallback AudioBackend. Tests
// override the default to drive the error path.
type ProceduralFactory func() (AudioBackend, error)

// SelectOption configures Select.
type SelectOption func(*selectConfig)

type selectConfig struct {
	mpvForced   bool
	mpvProbe    func() (string, bool)
	mpvFactory  MpvFactory
	procFactory ProceduralFactory
}

// WithMpvForced toggles strict-mpv mode. When true, Select fails
// with ErrNoAudioBackend if the mpv probe reports no binary,
// regardless of whether the procedural fallback could be used.
// When false (default), Select silently falls back to the
// procedural backend when mpv is unavailable. CI agents that
// explicitly want mpv pass WithMpvForced(true); end users leave
// it off and benefit from the ambient-noise fallback.
func WithMpvForced(forced bool) SelectOption {
	return func(c *selectConfig) { c.mpvForced = forced }
}

// WithMpvProbe overrides the $PATH detection for tests and for
// callers that know exactly where mpv lives. The probe returns
// (resolved_path, true) when a binary was found, ("", false)
// otherwise. The path is included in error messages so users
// can see which binary Select was looking for.
func WithMpvProbe(fn func() (string, bool)) SelectOption {
	return func(c *selectConfig) { c.mpvProbe = fn }
}

// WithMpvFactory injects the MpvFactory used to construct the
// mpv adapter. When nil (the default in this slice), Select
// treats mpv as unavailable even if the probe finds a binary;
// this is the right default for builds where mpv.go is not yet
// linked. cmd/lofi (post-PR-3) wires NewMpvBackend here.
func WithMpvFactory(fn MpvFactory) SelectOption {
	return func(c *selectConfig) { c.mpvFactory = fn }
}

// WithProceduralFactory overrides the default procedural factory.
// Tests use this to drive the "no backend available" error path
// without mocking exec.LookPath or the generator constructors.
func WithProceduralFactory(fn ProceduralFactory) SelectOption {
	return func(c *selectConfig) { c.procFactory = fn }
}

// defaultMpvProbe uses exec.LookPath to find "mpv" on $PATH.
func defaultMpvProbe() (string, bool) {
	p, err := exec.LookPath("mpv")
	if err != nil {
		return "", false
	}
	return p, true
}

// defaultProceduralBackendFactory constructs the canonical
// fallback: a ProceduralBackend wrapping a RainGenerator at
// 44.1 kHz. The seed is the package-level default so the
// fallback's first few seconds are reproducible.
func defaultProceduralBackendFactory() (AudioBackend, error) {
	return NewProceduralBackend(
		WithGenerator(NewRainGenerator(44100)),
		WithSampleRate(44100),
	), nil
}

// Select picks the AudioBackend that will serve the current
// process. The selection logic, in order:
//
//  1. If WithMpvForced(true) was passed, mpv MUST be available
//     on $PATH (or the override probe). If it is missing,
//     ErrNoAudioBackend is returned immediately with install
//     guidance — no silent fallback.
//  2. Otherwise, when the probe finds mpv AND an MpvFactory is
//     wired, Select tries to construct the MpvBackend. Success
//     short-circuits the rest of the selection.
//  3. On mpv miss or mpv-construction failure, Select attempts
//     the procedural fallback. Success returns the procedural
//     backend. Failure (or a nil factory) escalates to
//     ErrNoAudioBackend.
//
// The ctx argument is accepted for parity with other context-aware
// adapters in the codebase; Select does not currently cancel on
// ctx.Done() because selection is fast and synchronous, but
// callers SHOULD pass their own ctx in case future work adds
// networked discovery.
func Select(ctx context.Context, opts ...SelectOption) (AudioBackend, error) {
	_ = ctx // reserved for future context-aware discovery
	cfg := selectConfig{
		mpvProbe:    defaultMpvProbe,
		procFactory: defaultProceduralBackendFactory,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	probePath, mpvFound := cfg.mpvProbe()

	// Strict-mpv mode: missing binary is a hard error.
	if cfg.mpvForced && !mpvFound {
		return nil, fmt.Errorf(
			"%w: mpv binary not found in $PATH (searched for %q); install mpv or set LOFI_MPV to override",
			ErrNoAudioBackend, "mpv",
		)
	}

	// Optional-mpv mode: try the factory if both the probe and
	// the factory are positive. A factory error falls through to
	// the procedural backend.
	if !cfg.mpvForced && mpvFound && cfg.mpvFactory != nil {
		b, err := cfg.mpvFactory()
		if err == nil {
			return b, nil
		}
		// Fall through; mpv construction failed but the user
		// still gets ambient noise instead of silence.
	}

	// Procedural fallback.
	if cfg.procFactory != nil {
		b, err := cfg.procFactory()
		if err == nil {
			return b, nil
		}
		return nil, fmt.Errorf(
			"%w: mpv %s, procedural fallback failed (%v); install mpv for full audio",
			ErrNoAudioBackend, mpvStatus(mpvFound, probePath), err,
		)
	}

	return nil, fmt.Errorf(
		"%w: mpv %s and procedural fallback unavailable; install mpv for full audio",
		ErrNoAudioBackend, mpvStatus(mpvFound, probePath),
	)
}

// mpvStatus formats a short, user-facing description of the mpv
// probe outcome for inclusion in error messages.
func mpvStatus(found bool, path string) string {
	if found {
		return "available at " + path
	}
	return "not found in $PATH"
}
