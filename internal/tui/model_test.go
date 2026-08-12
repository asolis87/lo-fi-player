package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asolis87/lo-fi-player/internal/audio"
)

// TestModel_StartsOnNowPlaying is mandatory: cuando se construye
// el Model con un catalogo presente, el programa debe aterrizar en
// Now-Playing (REQ-TUI-1) para mantener el flujo slice-1 intacto.
// El caso de catalogo ausente vive en TestModelNoCatalog_StartsWhenCatalogNil
// en nocatalog_test.go (CATALOG-1).
func TestModel_StartsOnNowPlaying(t *testing.T) {
	backend := audio.NewMockBackend()
	cat := newFilledCatalog()
	m := NewModel(backend, cat, nil)

	if m.Mode != ModeNowPlaying {
		t.Fatalf("Mode = %v, want %v", m.Mode, ModeNowPlaying)
	}
	if m.Volume != defaultVolume {
		t.Fatalf("Volume = %d, want %d", m.Volume, defaultVolume)
	}
	if m.LastError != "" {
		t.Fatalf("LastError = %q, want empty at startup", m.LastError)
	}
	if m.ErrCh != nil {
		t.Fatalf("ErrCh should be nil when not provided")
	}
	if m.Backend != backend {
		t.Fatalf("Backend pointer mismatch (not stored)")
	}
}

func TestModel_NavigationKeys_SwitchViews(t *testing.T) {
	cases := []struct {
		key      rune
		wantView Mode
		startOn  Mode // view to start on; zero defaults to NowPlaying
	}{
		{key: 'p', wantView: ModeNowPlaying},
		{key: 'q', wantView: ModeQueue, startOn: ModeCatalog}, // 'q' on NowPlaying quits instead
		{key: 'c', wantView: ModeCatalog},
		{key: 'a', wantView: ModeAttribution},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.key), func(t *testing.T) {
			cat := newFilledCatalog()
			m := NewModel(audio.NewMockBackend(), cat, nil)
			if tc.startOn != 0 {
				m.Mode = tc.startOn
			}
			m = applyKey(m, keyMsg(tc.key))
			if m.Mode != tc.wantView {
				t.Fatalf("Mode after %q = %v, want %v", tc.key, m.Mode, tc.wantView)
			}
		})
	}
}

// TestModel_QOnNowPlaying_Quits pins the contextual behaviour the
// prompt asks for: "q (en now-playing) = quit". Without this rule the
// TUI would be unreachable from the home view.
func TestModel_QOnNowPlaying_Quits(t *testing.T) {
	m := NewModel(audio.NewMockBackend(), nil, nil)
	_, cmd := m.Update(keyMsg('q'))
	if cmd == nil {
		t.Fatalf("q on NowPlaying did not return a tea.Cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("q on NowPlaying did not return tea.QuitMsg, got %T", cmd())
	}
}

// TestModel_EscReturnsToNowPlaying is the REQ-TUI-1 "back" key: any
// non-home view returns to Now-Playing on escape. From Now-Playing
// `esc` is a no-op so the user cannot accidentally leave the TUI.
func TestModel_EscReturnsToNowPlaying(t *testing.T) {
	for _, view := range []Mode{ModeQueue, ModeCatalog, ModeAttribution} {
		view := view
		t.Run(viewName(view), func(t *testing.T) {
			m := NewModel(audio.NewMockBackend(), nil, nil)
			m.Mode = view
			m = applyKey(m, escKeyMsg())
			if m.Mode != ModeNowPlaying {
				t.Fatalf("Mode after esc = %v, want NowPlaying", m.Mode)
			}
		})
	}
}

// TestModel_ReceivesBackendEvent ensures audio error events arriving
// from the composition root surface as a non-fatal banner via
// LastError, matching S-TUI-2.
func TestModel_ReceivesBackendEvent(t *testing.T) {
	m := NewModel(audio.NewMockBackend(), nil, nil)
	out, _ := m.Update(audio.Event{Type: audio.EventError, Message: "mpv crashed"})
	m = out.(Model)
	if m.LastError == "" {
		t.Fatalf("LastError stayed empty after EventError")
	}
	if m.LastError != "mpv crashed" {
		t.Fatalf("LastError = %q, want %q", m.LastError, "mpv crashed")
	}
}

func viewName(v Mode) string {
	switch v {
	case ModeNowPlaying:
		return "NowPlaying"
	case ModeQueue:
		return "Queue"
	case ModeCatalog:
		return "Catalog"
	case ModeAttribution:
		return "Attribution"
	}
	return "unknown"
}
