package audio

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestSelect_NoMpvFallbackProcedural is the happy-path fallback:
// when the probe finds no mpv binary (or when the caller overrides
// the probe), Select MUST return a working procedural AudioBackend
// rather than failing the whole binary.
func TestSelect_NoMpvFallbackProcedural(t *testing.T) {
	deviceFactoryOverride = noopDevice{}
	t.Cleanup(func() { deviceFactoryOverride = nil })
	ctx := context.Background()
	b, err := Select(ctx,
		WithMpvProbe(func() (string, bool) { return "", false }),
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if b == nil {
		t.Fatal("Select returned nil backend")
	}
	if _, ok := b.(*ProceduralBackend); !ok {
		t.Fatalf("Select returned %T, want *ProceduralBackend", b)
	}
	// The default procedural backend must be immediately usable.
	// Walk the full Load -> Play -> State -> Close surface the way
	// cmd/lofi would.
	if err := b.Load(Track{ID: "procedural:rain", Path: "procedural:rain"}); err != nil {
		t.Fatalf("procedural Load: %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("procedural Play: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("procedural Close: %v", err)
	}
}

// TestSelect_NoneAvailableReturnsError covers the failure path:
// both the mpv probe returns "not found" AND the procedural
// fallback fails to construct. Select MUST return ErrNoAudioBackend
// (matchable via errors.Is) and the message MUST include actionable
// guidance so the user can recover.
func TestSelect_NoneAvailableReturnsError(t *testing.T) {
	ctx := context.Background()
	b, err := Select(ctx,
		WithMpvProbe(func() (string, bool) { return "", false }),
		WithMpvFactory(func() (AudioBackend, error) {
			return nil, errors.New("simulated mpv failure")
		}),
		WithProceduralFactory(func() (AudioBackend, error) {
			return nil, errors.New("simulated procedural failure")
		}),
	)
	if err == nil {
		t.Fatal("Select: expected error, got nil")
	}
	if !errors.Is(err, ErrNoAudioBackend) {
		t.Fatalf("Select error = %v, want wrapped ErrNoAudioBackend", err)
	}
	if !strings.Contains(err.Error(), "install mpv") {
		t.Fatalf("Select error %q lacks actionable install guidance", err.Error())
	}
	if b != nil {
		t.Fatalf("Select returned backend %T on error path, want nil", b)
	}
}

// TestSelect_MpvForcedMissingErrors locks down the strict-mpv mode:
// when the caller requires mpv (WithMpvForced(true)) and the probe
// reports no mpv on $PATH, Select MUST error even though a
// procedural backend would otherwise be available. This is the
// mode headless CI uses to fail loud when mpv is missing on a
// build agent that explicitly asked for mpv.
func TestSelect_MpvForcedMissingErrors(t *testing.T) {
	ctx := context.Background()
	b, err := Select(ctx,
		WithMpvForced(true),
		WithMpvProbe(func() (string, bool) { return "", false }),
	)
	if err == nil {
		t.Fatal("Select: expected error, got nil")
	}
	if !errors.Is(err, ErrNoAudioBackend) {
		t.Fatalf("Select error = %v, want wrapped ErrNoAudioBackend", err)
	}
	if b != nil {
		t.Fatalf("Select returned backend %T on error path, want nil", b)
	}
}
