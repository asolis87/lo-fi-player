package tui

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
)

// flakyBackend inyecta fallos de Load/Play sobre el doble base para
// probar que la transicion compartida no muta identidad, historial ni
// persistencia cuando el puerto falla (NAV-1 + PERSIST-1).
type flakyBackend struct {
	*audio.MockBackend
	loadErr error
	playErr error
}

func (f *flakyBackend) Load(t audio.Track) error {
	if f.loadErr != nil {
		return f.loadErr
	}
	return f.MockBackend.Load(t)
}

func (f *flakyBackend) Play() error {
	if f.playErr != nil {
		return f.playErr
	}
	return f.MockBackend.Play()
}

// twoTrackCatalog es el catalogo determinista de dos pistas con el que
// se ejercita el wraparound de n/b sin tocar disco.
func twoTrackCatalog() *catalog.Catalog {
	return &catalog.Catalog{Version: "1", Tracks: []catalog.Track{{ID: "a"}, {ID: "b"}}}
}

// newTransitionModel arma el modelo con estado vivo, persister contado
// y resolvedor de rutas inyectado.
func newTransitionModel(t *testing.T, backend audio.AudioBackend) (Model, *config.PlaybackState, *atomic.Int32) {
	t.Helper()
	saves := &atomic.Int32{}
	withPersisterStub(t, func() error { saves.Add(1); return nil })
	state := config.NewPlaybackState(config.Default())
	m := Model{
		Backend:     backend,
		Catalog:     twoTrackCatalog(),
		State:       state,
		Mode:        ModeNowPlaying,
		Volume:      defaultVolume,
		ResolvePath: func(id string) string { return "/cache/" + id + "/audio.mp3" },
	}
	return m, state, saves
}

// TestLoadAndPlaySelected_NextPrevAdvancesGenerationAndIdentity cubre
// NAV-1: n/b eligen el objetivo con wraparound, incrementan generacion,
// invocan Load(Track{Generation}) y luego Play, y persisten una sola vez
// por transicion exitosa.
func TestLoadAndPlaySelected_NextPrevAdvancesGenerationAndIdentity(t *testing.T) {
	backend := audio.NewMockBackend()
	m, state, saves := newTransitionModel(t, backend)

	m = applyKey(m, keyMsg('n'))
	if m.SelectedIdx != 1 || m.LoadedID != "b" || m.PlayingID != "b" || !m.Playing {
		t.Fatalf("n: idx=%d loaded=%q playing=%q flag=%v", m.SelectedIdx, m.LoadedID, m.PlayingID, m.Playing)
	}
	if m.LoadedGeneration != 1 || m.NextGeneration != 1 {
		t.Fatalf("generaciones tras n = loaded %d next %d, want 1/1", m.LoadedGeneration, m.NextGeneration)
	}
	m = applyKey(m, keyMsg('n')) // wraparound 1 -> 0
	if m.SelectedIdx != 0 || m.LoadedID != "a" || m.LoadedGeneration != 2 {
		t.Fatalf("wraparound n: idx=%d loaded=%q gen=%d", m.SelectedIdx, m.LoadedID, m.LoadedGeneration)
	}
	m = applyKey(m, keyMsg('b')) // wraparound 0 -> ultimo
	if m.SelectedIdx != 1 || m.LoadedID != "b" || m.LoadedGeneration != 3 {
		t.Fatalf("wraparound b: idx=%d loaded=%q gen=%d", m.SelectedIdx, m.LoadedID, m.LoadedGeneration)
	}

	loaded := backend.Loaded()
	if len(loaded) != 3 {
		t.Fatalf("Load invocado %d veces, want 3", len(loaded))
	}
	for i, tr := range loaded {
		if tr.Generation != uint64(i+1) || tr.Path != "/cache/"+tr.ID+"/audio.mp3" {
			t.Fatalf("Load[%d] = %+v, want Generation %d y ruta resuelta", i, tr, i+1)
		}
	}
	if saves.Load() != 3 {
		t.Fatalf("persist invocado %d veces, want 3 (una por transicion exitosa)", saves.Load())
	}
	if got := state.History(); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("History = %v, want MRU-front [b a]", got)
	}
}

// TestLoadAndPlaySelected_FailurePreservesIdentityAndHistory cubre
// PERSIST-1: Load fallido no toca Loaded*, Play fallido no toca
// Playing*, y ninguno muta historial ni persiste.
func TestLoadAndPlaySelected_FailurePreservesIdentityAndHistory(t *testing.T) {
	cases := []struct {
		name       string
		backend    *flakyBackend
		wantLoaded string
		wantGen    uint64
		wantBanner string
	}{
		{"LoadFails", &flakyBackend{MockBackend: audio.NewMockBackend(), loadErr: fmt.Errorf("no file")}, "", 0, "load: no file"},
		{"PlayFails", &flakyBackend{MockBackend: audio.NewMockBackend(), playErr: fmt.Errorf("ipc down")}, "b", 1, "play: ipc down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, state, saves := newTransitionModel(t, tc.backend)
			m = applyKey(m, keyMsg('n'))
			if m.LoadedID != tc.wantLoaded || m.LoadedGeneration != tc.wantGen {
				t.Fatalf("identidad loaded = %q/%d, want %q/%d", m.LoadedID, m.LoadedGeneration, tc.wantLoaded, tc.wantGen)
			}
			if m.PlayingID != "" || m.Playing {
				t.Fatalf("identidad playing mutada: %q flag=%v", m.PlayingID, m.Playing)
			}
			if m.LastError != tc.wantBanner {
				t.Fatalf("LastError = %q, want %q", m.LastError, tc.wantBanner)
			}
			if saves.Load() != 0 || len(state.History()) != 0 {
				t.Fatalf("persistencia/historial mutados: saves=%d history=%v", saves.Load(), state.History())
			}
			if m.NextGeneration != 1 {
				t.Fatalf("NextGeneration = %d, want 1 (token consumido)", m.NextGeneration)
			}
		})
	}
}

// TestTogglePlay_LoadsWhenIdleAndPausesWhenPlaying cubre la transicion
// compartida en space: carga cuando no hay pista, pausa cuando suena y
// reanuda sin nueva generacion ni persistencia extra.
func TestTogglePlay_LoadsWhenIdleAndPausesWhenPlaying(t *testing.T) {
	backend := audio.NewMockBackend()
	m, _, saves := newTransitionModel(t, backend)

	m = applyKey(m, spaceKeyMsg())
	if m.LoadedID != "a" || m.PlayingID != "a" || m.SelectedIdx != 0 || m.LoadedGeneration != 1 {
		t.Fatalf("space idle: loaded=%q playing=%q idx=%d gen=%d", m.LoadedID, m.PlayingID, m.SelectedIdx, m.LoadedGeneration)
	}
	m = applyKey(m, spaceKeyMsg())
	if m.Playing || !backend.Paused() || m.LoadedID != "a" || m.PlayingID != "a" {
		t.Fatalf("space pause: flag=%v paused=%v loaded=%q playing=%q", m.Playing, backend.Paused(), m.LoadedID, m.PlayingID)
	}
	m = applyKey(m, spaceKeyMsg())
	if !m.Playing || len(backend.Loaded()) != 1 || m.LoadedGeneration != 1 || saves.Load() != 1 {
		t.Fatalf("space resume: flag=%v loads=%d gen=%d saves=%d", m.Playing, len(backend.Loaded()), m.LoadedGeneration, saves.Load())
	}
}

// TestNavigationWithoutCatalog_NoLoadNoPlay cubre NAV-2: catalogo nil o
// vacio inhibe Load y Play y conserva la guia de `lofi sync`.
func TestNavigationWithoutCatalog_NoLoadNoPlay(t *testing.T) {
	cases := []struct {
		name string
		cat  *catalog.Catalog
	}{
		{"NilCatalog", nil},
		{"EmptyCatalog", &catalog.Catalog{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := audio.NewMockBackend()
			m := Model{Backend: backend, Catalog: tc.cat, Mode: ModeNoCatalog, Volume: defaultVolume}
			for _, k := range []tea.KeyMsg{keyMsg('n'), keyMsg('b'), spaceKeyMsg()} {
				m = applyKey(m, k)
			}
			if len(backend.Loaded()) != 0 || backend.Played() {
				t.Fatalf("catalogo ausente disparo audio: loads=%d played=%v", len(backend.Loaded()), backend.Played())
			}
			if m.LoadedID != "" || m.PlayingID != "" || m.NextGeneration != 0 {
				t.Fatalf("estado mutado sin catalogo: loaded=%q playing=%q gen=%d", m.LoadedID, m.PlayingID, m.NextGeneration)
			}
			if !strings.Contains(m.View(), noCatalogTUIText) {
				t.Fatalf("View sin guia de sync: %q", m.View())
			}
		})
	}
}

// TestViewNowPlaying_ShowsLoadedTrackAndAudioMode cubre VIS-1/FBK-1: la
// vista expone la pista cargada y el modo de audio de forma determinista.
func TestViewNowPlaying_ShowsLoadedTrackAndAudioMode(t *testing.T) {
	m, _, _ := newTransitionModel(t, audio.NewMockBackend())
	m.AudioMode = "procedural:rain"
	if got := m.View(); !strings.Contains(got, "loaded: none") || !strings.Contains(got, "mode: procedural:rain") {
		t.Fatalf("View inicial = %q", got)
	}
	m = applyKey(m, keyMsg('n'))
	first := m.View()
	if !strings.Contains(first, "loaded: b") {
		t.Fatalf("View tras n = %q", first)
	}
	if first != m.View() {
		t.Fatalf("View no es determinista entre renders")
	}
}

// TestLoadAndPlaySelected_FailureKeepsSelectedIdxAdvanced pin el
// contrato SelectedIdx cuando falla la carga: la intencion del usuario
// avanza (SelectedIdx = objetivo) pero LoadedID/PlayingID permanecen
// en la pista anterior. Asi, un `n` siguiente reintenta cargar la
// pista recien apuntada sin perder la posicion visual.
func TestLoadAndPlaySelected_FailureKeepsSelectedIdxAdvanced(t *testing.T) {
	backend := &flakyBackend{MockBackend: audio.NewMockBackend(), loadErr: fmt.Errorf("disk gone")}
	m, _, _ := newTransitionModel(t, backend)
	m = applyKey(m, keyMsg('n'))
	if m.SelectedIdx != 1 || m.LoadedID != "" || m.LoadedGeneration != 0 {
		t.Fatalf("idx=%d loaded=%q gen=%d, want idx=1 loaded=\"\" gen=0", m.SelectedIdx, m.LoadedID, m.LoadedGeneration)
	}
	if !strings.Contains(m.LastError, "disk gone") {
		t.Fatalf("LastError = %q, want banner con disk gone", m.LastError)
	}
}

// TestPauseResume_NoDuplicateHistoryAndPersist bloquea el camino de
// pausar y reanudar sobre la misma pista: ni History ni la persistencia
// se disparan porque no hay nuevo Play() exitoso que cambie de pista.
// El estado PlayingID queda en LoadedID (identidad consistente) sin
// sumar Saves.
func TestPauseResume_NoDuplicateHistoryAndPersist(t *testing.T) {
	backend := audio.NewMockBackend()
	m, state, saves := newTransitionModel(t, backend)
	m = applyKey(m, keyMsg('n')) // carga + persiste "b"
	if saves.Load() != 1 || len(state.History()) != 1 || state.History()[0] != "b" {
		t.Fatalf("primer play: saves=%d history=%v", saves.Load(), state.History())
	}
	prePausedHistory := append([]string{}, state.History()...)
	prePausedSaves := saves.Load()
	m = applyKey(m, spaceKeyMsg()) // pause
	m = applyKey(m, spaceKeyMsg()) // resume
	m = applyKey(m, spaceKeyMsg()) // pause
	if state.History()[0] != "b" || len(state.History()) != 1 {
		t.Fatalf("pause/resume duplico historial: %v", state.History())
	}
	if saves.Load() != prePausedSaves {
		t.Fatalf("pause/resume persistio: saves=%d, want %d", saves.Load(), prePausedSaves)
	}
	if m.PlayingID != "b" || m.LoadedID != "b" || m.Playing {
		t.Fatalf("identidad rota tras pause/resume: loaded=%q playing=%q flag=%v", m.LoadedID, m.PlayingID, m.Playing)
	}
	if !reflect.DeepEqual(state.History(), prePausedHistory) {
		t.Fatalf("history mutada: got %v want %v", state.History(), prePausedHistory)
	}
}
