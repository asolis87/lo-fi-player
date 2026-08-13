package tui

import (
	"testing"

	"github.com/asolis87/lo-fi-player/internal/audio"
	tea "github.com/charmbracelet/bubbletea"
)

// TestKeymap_SpaceTogglesPlayPause is mandatory: REQ-TUI-2 mandates
// spacebar as play/pause. The TUI delegates to Backend.Play / Pause so
// the port (MockBackend in tests, MpvBackend in production) owns the
// semantics. Slice 4 (NAV-2) exige catalogo no vacio: sin pistas la
// transicion compartida inhibe Load y Play.
func TestKeymap_SpaceTogglesPlayPause(t *testing.T) {
	backend := audio.NewMockBackend()
	m := NewModel(backend, newFilledCatalog(), nil)

	m = applyKey(m, spaceKeyMsg())
	if !backend.Played() {
		t.Fatalf("space did not invoke Backend.Play()")
	}
	m = applyKey(m, spaceKeyMsg())
	if !backend.Paused() {
		t.Fatalf("second space did not invoke Backend.Pause()")
	}
}

// TestKeymap_ADoesOneKeypressAttribution is mandatory (REQ-ATT-1):
// `a` opens Attribution in one keypress from any view, no backend
// round-trip.
func TestKeymap_ADoesOneKeypressAttribution(t *testing.T) {
	backend := audio.NewMockBackend()
	cases := []Mode{ModeNowPlaying, ModeQueue, ModeCatalog, ModeAttribution}
	for _, start := range cases {
		start := start
		t.Run(viewName(start), func(t *testing.T) {
			m := NewModel(backend, nil, nil)
			m.Mode = start
			m = applyKey(m, keyMsg('a'))
			if m.Mode != ModeAttribution {
				t.Fatalf("Mode after `a` from %s = %v, want Attribution", viewName(start), m.Mode)
			}
		})
	}
}

// TestKeymap_QuitExits is mandatory: ctrl+c must cleanly quit the
// TUI from any view (not just NowPlaying).
func TestKeymap_QuitExits(t *testing.T) {
	for _, start := range []Mode{ModeNowPlaying, ModeQueue, ModeCatalog, ModeAttribution} {
		start := start
		t.Run(viewName(start), func(t *testing.T) {
			m := NewModel(audio.NewMockBackend(), nil, nil)
			m.Mode = start
			_, cmd := m.Update(ctrlCKeyMsg())
			if cmd == nil {
				t.Fatalf("ctrl+c from %s did not return a tea.Cmd", viewName(start))
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatalf("ctrl+c from %s did not yield tea.QuitMsg, got %T", viewName(start), cmd())
			}
		})
	}
}

func TestKeymap_PlusMinus_StepsVolume(t *testing.T) {
	backend := audio.NewMockBackend()
	m := NewModel(backend, nil, nil)

	m = applyKey(m, keyMsg('+'))
	if backend.Volume() != defaultVolume+volumeStep {
		t.Fatalf("Volume after + = %d, want %d", backend.Volume(), defaultVolume+volumeStep)
	}
	m = applyKey(m, keyMsg('+'))
	if backend.Volume() != defaultVolume+2*volumeStep {
		t.Fatalf("Volume after ++ = %d, want %d", backend.Volume(), defaultVolume+2*volumeStep)
	}
	m = applyKey(m, keyMsg('-'))
	if backend.Volume() != defaultVolume+volumeStep {
		t.Fatalf("Volume after - = %d, want %d", backend.Volume(), defaultVolume+volumeStep)
	}
}

func TestKeymap_PlusMinus_ClampsAtBounds(t *testing.T) {
	backend := audio.NewMockBackend()
	m := NewModel(backend, nil, nil)
	m.Volume = 100

	m = applyKey(m, keyMsg('+'))
	if backend.Volume() != 100 {
		t.Fatalf("Volume past max = %d, want 100", backend.Volume())
	}

	m2 := NewModel(backend, nil, nil)
	m2.Volume = 3
	m2 = applyKey(m2, keyMsg('-'))
	if backend.Volume() != 0 {
		t.Fatalf("Volume past min (clamped to 0) = %d, want 0", backend.Volume())
	}
	// No underflow even if pressed many times from the lower bound.
	m2 = applyKey(m2, keyMsg('-'))
	if backend.Volume() < 0 {
		t.Fatalf("Volume went negative: %d", backend.Volume())
	}
	if m2.Volume != 0 {
		t.Fatalf("Model.Volume past lower bound = %d, want 0", m2.Volume)
	}
}

// TestKeymap_RejectsUnknownKeys guarantees unrecognised keys do not
// mutate state (silent ignore keeps strict-TDD keyboard semantics).
func TestKeymap_RejectsUnknownKeys(t *testing.T) {
	backend := audio.NewMockBackend()
	m := NewModel(backend, nil, nil)
	before := m
	m = applyKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	if m.Mode != before.Mode || m.Volume != before.Volume {
		t.Fatalf("unknown key mutated state: before=%+v after=%+v", before, m)
	}
}
