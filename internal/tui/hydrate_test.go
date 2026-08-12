package tui

import (
	"testing"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/config"
)

// TestNewModelWithState_HydratesVolumeFromState vale VOL-1 escenario 2:
// un TUI construido con un PlaybackState no-nil que guarda
// volume=72 debe inicializar Model.Volume en 72, no en 50.
func TestNewModelWithState_HydratesVolumeFromState(t *testing.T) {
	cfg := config.Default()
	cfg.Volume = 72
	state := config.NewPlaybackState(cfg)
	m := NewModelWithState(audio.NewMockBackend(), nil, state, nil)

	if m.Volume != 72 {
		t.Fatalf("Volume = %d, want 72 (hydrated from state.EffectiveVolume)", m.Volume)
	}
}

// TestNewModelWithState_NilState_Defaults50 garantiza que el path
// sin estado siga arrancando en defaultVolume (=50) para mantener
// backwards-compatible el constructor original NewModel.
func TestNewModelWithState_NilState_Defaults50(t *testing.T) {
	m := NewModelWithState(audio.NewMockBackend(), nil, nil, nil)

	if m.Volume != 50 {
		t.Fatalf("Volume = %d, want 50 when state is nil", m.Volume)
	}
}

// TestNewModelWithState_OutOfRangeVolume_Defaults50 vale VOL-2:
// un estado con volumen crudo fuera de [0,100] debe hidratarse como
// 50 sin reescribir el raw.
func TestNewModelWithState_OutOfRangeVolume_Defaults50(t *testing.T) {
	cfg := config.Default()
	cfg.Volume = 150
	state := config.NewPlaybackState(cfg)
	m := NewModelWithState(audio.NewMockBackend(), nil, state, nil)

	if m.Volume != 50 {
		t.Fatalf("Volume = %d, want 50 (out-of-range raw maps to default)", m.Volume)
	}
	if state.EffectiveVolume() != 50 {
		t.Fatalf("state.EffectiveVolume() = %d, want 50", state.EffectiveVolume())
	}
}
