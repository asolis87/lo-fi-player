package tui

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
)

func threeTrackCatalog() *catalog.Catalog {
	return &catalog.Catalog{Version: "1", Tracks: []catalog.Track{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
}

func eventModel(t *testing.T, backend audio.AudioBackend) (Model, *config.PlaybackState, *atomic.Int32) {
	t.Helper()
	saves := &atomic.Int32{}
	withPersisterStub(t, func() error { saves.Add(1); return nil })
	state := config.NewPlaybackState(config.Default())
	return Model{
		Backend: backend, Catalog: threeTrackCatalog(), State: state,
		Mode: ModeNowPlaying, Volume: defaultVolume, SelectedIdx: 0,
		ResolvePath: func(id string) string { return "/cache/" + id + "/audio.mp3" },
	}, state, saves
}

// EVT-1: EventEnd con generacion vigente -> auto-avanza circular,
// carga, reproduce, persiste una vez.
func TestEventEnd_AutoAdvancesAndPlaysWhenGenerationMatches(t *testing.T) {
	backend := audio.NewMockBackend()
	m, state, saves := eventModel(t, backend)
	m = applyKey(m, keyMsg('n')) // idx=1, gen=1, playing "b"
	out, _ := m.Update(audio.Event{Type: audio.EventEnd, Generation: 1})
	m = out.(Model)
	if m.SelectedIdx != 2 || m.LoadedID != "c" || m.PlayingID != "c" || !m.Playing || m.LoadedGeneration != 2 {
		t.Fatalf("auto-avance invalido: idx=%d loaded=%q playing=%q flag=%v gen=%d", m.SelectedIdx, m.LoadedID, m.PlayingID, m.Playing, m.LoadedGeneration)
	}
	if saves.Load() != 2 {
		t.Fatalf("persistencias = %d, want 2", saves.Load())
	}
	if got := state.History(); len(got) != 2 || got[0] != "c" || got[1] != "b" {
		t.Fatalf("history = %v, want MRU-front [c b]", got)
	}
}

// Correlacion generacional: stale EventEnd se ignora sin navegar,
// Load, Play ni persistencia.
func TestEventEnd_IgnoredWhenStaleGeneration(t *testing.T) {
	backend := audio.NewMockBackend()
	m, state, saves := eventModel(t, backend)
	m = applyKey(m, keyMsg('n'))
	out, _ := m.Update(audio.Event{Type: audio.EventEnd, Generation: 99})
	m = out.(Model)
	if m.SelectedIdx != 1 || m.LoadedID != "b" || m.PlayingID != "b" || m.LoadedGeneration != 1 {
		t.Fatalf("stale EventEnd muto: idx=%d loaded=%q playing=%q gen=%d", m.SelectedIdx, m.LoadedID, m.PlayingID, m.LoadedGeneration)
	}
	if saves.Load() != 1 || len(state.History()) != 1 || len(backend.Loaded()) != 1 {
		t.Fatalf("stale EventEnd persiste o llama Load: saves=%d history=%v loads=%d", saves.Load(), state.History(), len(backend.Loaded()))
	}
}

// Frontera circular: EventEnd en el ultimo indice envuelve a 0.
func TestEventEnd_WrapsAroundAtLastIndex(t *testing.T) {
	backend := audio.NewMockBackend()
	m, _, _ := eventModel(t, backend)
	m.SelectedIdx, m.LoadedID, m.LoadedGeneration, m.NextGeneration, m.PlayingID = 2, "c", 5, 5, "c"
	m.Playing = true
	out, _ := m.Update(audio.Event{Type: audio.EventEnd, Generation: 5})
	m = out.(Model)
	if m.SelectedIdx != 0 || m.LoadedID != "a" || m.PlayingID != "a" || !m.Playing || m.LoadedGeneration != 6 {
		t.Fatalf("wraparound invalido: idx=%d loaded=%q playing=%q flag=%v gen=%d", m.SelectedIdx, m.LoadedID, m.PlayingID, m.Playing, m.LoadedGeneration)
	}
}

// EVT-3 via tea.Cmd: Init devuelve un Cmd que lee UN evento del canal
// Events() y lo entrega como Msg.
func TestWaitForAudioEventCmd_YieldsNextEventFromChannel(t *testing.T) {
	backend := audio.NewMockBackend()
	m := Model{Backend: backend, Catalog: threeTrackCatalog(), Mode: ModeNowPlaying, Volume: defaultVolume}
	cmd := m.waitForAudioEventCmd()
	if cmd == nil {
		t.Fatalf("Cmd nil para backend EventSource")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	backend.EmitEvent(audio.Event{Type: audio.EventEnd, Generation: 1})
	select {
	case msg := <-done:
		ev, ok := msg.(audio.Event)
		if !ok || ev.Type != audio.EventEnd || ev.Generation != 1 {
			t.Fatalf("evento = %+v, want EventEnd{Gen:1}", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Cmd no produjo Msg en 2s")
	}
}

// EVT-3: tras manejar audio.Event, Update devuelve un Cmd que lee el
// siguiente evento del MISMO backend. Asi una cadena n/audio/audio
// mantiene exactamente UN suscriptor activo.
func TestUpdate_AfterAudioEventReturnsListenerCmd(t *testing.T) {
	backend := audio.NewMockBackend()
	m, _, _ := eventModel(t, backend)
	out, cmd := m.Update(audio.Event{Type: audio.EventEnd, Generation: 1}) // gen mismatch; ignored
	m = out.(Model)
	if cmd == nil {
		t.Fatalf("Cmd nil tras audio.Event")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	backend.EmitEvent(audio.Event{Type: audio.EventError, Message: "boom"})
	select {
	case msg := <-done:
		ev, ok := msg.(audio.Event)
		if !ok || ev.Type != audio.EventError || ev.Message != "boom" {
			t.Fatalf("segundo Msg = %+v, want EventError{boom}", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Cmd no produjo segundo Msg en 2s")
	}
}

// EVT-2: EventError sticky dispara el swap inyectado, backend viejo
// queda Closed, identidades reseteadas, modo actualizado, banner
// persistido hasta el siguiente Load exitoso.
func TestEventError_RainFallbackRebindsBackend(t *testing.T) {
	old, rain := audio.NewMockBackend(), audio.NewMockBackend()
	m, state, _ := eventModel(t, old)
	m.LoadedID, m.LoadedGeneration, m.PlayingID, m.NextGeneration = "a", 1, "a", 1
	m.Playing = true
	var calls int32
	m.RebindBackend = func() (audio.AudioBackend, string, error) {
		atomic.AddInt32(&calls, 1)
		// RebindBackend representa SwapToRain: cierra el backend viejo
		// antes de devolver el nuevo (EVT-2 contrato).
		if cerr := old.Close(); cerr != nil {
			return nil, "", cerr
		}
		return rain, "procedural:rain", nil
	}
	out, _ := m.Update(audio.Event{Type: audio.EventError, Message: "mpv crashed"})
	m = out.(Model)
	if calls != 1 || m.Backend != audio.AudioBackend(rain) || m.AudioMode != "procedural:rain" {
		t.Fatalf("swap fallo: calls=%d backend=%v mode=%q", calls, m.Backend, m.AudioMode)
	}
	if m.LoadedID != "" || m.PlayingID != "" || m.LoadedGeneration != 0 || m.NextGeneration != 0 {
		t.Fatalf("identidades/generaciones no reseteadas: loaded=%q playing=%q gen=%d next=%d", m.LoadedID, m.PlayingID, m.LoadedGeneration, m.NextGeneration)
	}
	if !strings.Contains(m.LastError, "mpv crashed") {
		t.Fatalf("LastError = %q, want contiene mpv crashed", m.LastError)
	}
	if !old.Closed() {
		t.Fatalf("backend viejo no cerrado tras swap")
	}
	if _, ok := m.Backend.(audio.EventSource); !ok {
		t.Fatalf("backend nuevo no implementa EventSource")
	}
	if !m.Playing || len(state.History()) != 0 {
		t.Fatalf("Playing/History mutados en fallback: flag=%v history=%v", m.Playing, state.History())
	}
}

// VIS-1 + EVT-2: banner de EventError sobrevive hasta el siguiente
// Load exitoso post-swap (loadAndPlaySelected limpia LastError).
func TestEventError_BannerPersistsUntilNextLoadSuccessful(t *testing.T) {
	old, rain := audio.NewMockBackend(), audio.NewMockBackend()
	m, _, _ := eventModel(t, old)
	m.RebindBackend = func() (audio.AudioBackend, string, error) { return rain, "procedural:rain", nil }
	out, _ := m.Update(audio.Event{Type: audio.EventError, Message: "mpv crashed"})
	m = out.(Model)
	if !strings.Contains(m.LastError, "mpv crashed") {
		t.Fatalf("LastError tras EventError = %q", m.LastError)
	}
	m = applyKey(m, keyMsg('n'))
	if m.LastError != "" {
		t.Fatalf("LastError tras n exitoso = %q, want empty", m.LastError)
	}
}

// Fallback path: si RebindBackend retorna error, banner y backend
// viejo se conservan para que el usuario pueda seguir navegando.
func TestEventError_RebindFailureKeepsBannerAndOldBackend(t *testing.T) {
	old := audio.NewMockBackend()
	m, _, _ := eventModel(t, old)
	m.RebindBackend = func() (audio.AudioBackend, string, error) {
		return nil, "", errors.New("rain factory down")
	}
	out, _ := m.Update(audio.Event{Type: audio.EventError, Message: "mpv crashed"})
	m = out.(Model)
	if m.Backend != audio.AudioBackend(old) || m.LastError != "mpv crashed" {
		t.Fatalf("Backend=%v LastError=%q, want viejo y mensaje original", m.Backend, m.LastError)
	}
	if old.Closed() {
		t.Fatalf("backend viejo cerrado tras rebind fallido")
	}
}
