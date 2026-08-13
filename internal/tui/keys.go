package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/config"
)

// tuiStatePersister: TUI-side save for q/Ctrl+C and volume (PERSIST-1).
var tuiStatePersister = func() error { return nil }

func (m Model) saveAndQuit() (tea.Model, tea.Cmd) {
	if err := tuiStatePersister(); err != nil {
		m.LastError = "save state: " + err.Error()
	}
	return m, tea.Quit
}

// keyAction is the internal representation of what the keymap
// handler should do for a given keypress. Keeping dispatch on
// internal actions (instead of bubble-tea Cmd values) lets the
// test suite drive the model directly through tea.KeyMsg.
type keyAction int

const (
	actionNone keyAction = iota
	actionPlayPause
	actionNext
	actionPrev
	actionVolUp
	actionVolDown
	actionNavQueue
	actionNavNowPlaying
	actionNavCatalog
	actionNavAttribution
	actionEsc
	actionQuit
)

// bindingFor maps a Bubble Tea key message to an internal action.
// Special keys (space, esc, ctrl+c) are matched on Type to avoid
// the awkward " " and "esc" string representations KeyMsg.String
// produces; rune keys are matched on the single rune. Keys that
// produce contextual REQ-TUI-2 behaviour ("q" quits on Now-Playing,
// navigates to Queue elsewhere) are dispatched as actionNavQueue
// and the contextual logic lives in applyAction.
func bindingFor(msg tea.KeyMsg) (keyAction, bool) {
	switch msg.Type {
	case tea.KeySpace:
		return actionPlayPause, true
	case tea.KeyEsc:
		return actionEsc, true
	case tea.KeyCtrlC:
		return actionQuit, true
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case '+':
			return actionVolUp, true
		case '-':
			return actionVolDown, true
		case 'p':
			return actionNavNowPlaying, true
		case 'q':
			return actionNavQueue, true
		case 'c':
			return actionNavCatalog, true
		case 'a':
			return actionNavAttribution, true
		case 'n':
			return actionNext, true
		case 'b':
			return actionPrev, true
		}
	}
	return actionNone, false
}

// applyAction: q (on NowPlaying/NoCatalog) and Ctrl+C route through
// saveAndQuit so the TUI persists state before tea.Quit fires.
func (m Model) applyAction(a keyAction) (tea.Model, tea.Cmd) {
	switch a {
	case actionPlayPause:
		return m.togglePlay(), nil
	case actionEsc:
		return m.handleEsc(), nil
	case actionQuit:
		return m.saveAndQuit()
	case actionNavNowPlaying:
		m.Mode = ModeNowPlaying
	case actionNavQueue:
		// Contextual per the prompt: "q (en now-playing) = quit",
		// otherwise "q" reaches the Queue view per REQ-TUI-1.
		// ModeNoCatalog no tiene Queue a donde ir -> la q sale
		// de la aplicacion, igual que en Now-Playing.
		if m.Mode == ModeNowPlaying || m.Mode == ModeNoCatalog {
			return m.saveAndQuit()
		}
		m.Mode = ModeQueue
	case actionNavCatalog:
		m.Mode = ModeCatalog
	case actionNavAttribution:
		m.Mode = ModeAttribution
	case actionVolUp:
		return m.bumpVolume(volumeStep), nil
	case actionVolDown:
		return m.bumpVolume(-volumeStep), nil
	case actionNext:
		return m.loadAndPlaySelected(1), nil
	case actionPrev:
		return m.loadAndPlaySelected(-1), nil
	}
	return m, nil
}

// togglePlay comparte la transicion con n/b: sin pista cargada carga y
// reproduce la seleccion vigente; sonando pausa preservando identidad;
// pausado reanuda sin consumir una nueva generacion ni volver a
// persistir (no es un cambio de pista).
func (m Model) togglePlay() Model {
	if playing, _, _ := m.Backend.State(); playing {
		if err := m.Backend.Pause(); err == nil {
			m.Playing = false
		}
		return m
	}
	if m.LoadedID != "" {
		if err := m.Backend.Play(); err != nil {
			m.LastError = "play: " + err.Error()
			return m
		}
		m.Playing, m.PlayingID = true, m.LoadedID
		return m
	}
	return m.loadAndPlaySelected(0)
}

// loadAndPlaySelected es la unica transicion de carga+reproduccion que
// comparten n, b y space. Orden fijo: elegir objetivo con wraparound,
// consumir una generacion, Load(Track{Generation}) y luego Play.
// Loaded* solo avanza tras Load exitoso; Playing* e historial solo tras
// Play exitoso, y la persistencia ocurre exactamente una vez por
// transicion exitosa (NAV-1 + PERSIST-1). Un catalogo nil o vacio no
// invoca Load ni Play (NAV-2).
func (m Model) loadAndPlaySelected(delta int) Model {
	if !m.hasTracks() {
		return m
	}
	m = m.advanceQueue(delta)
	track := m.Catalog.Tracks[m.SelectedIdx]
	m.NextGeneration++
	gen := m.NextGeneration
	if err := m.Backend.Load(audio.Track{ID: track.ID, Path: m.pathFor(track.ID), Generation: gen}); err != nil {
		m.LastError = "load: " + err.Error()
		return m
	}
	m.LoadedID, m.LoadedGeneration, m.LastError = track.ID, gen, ""
	if err := m.Backend.Play(); err != nil {
		m.LastError = "play: " + err.Error()
		return m
	}
	m.Playing, m.PlayingID = true, track.ID
	return m.recordPlayed(track.ID)
}

// recordPlayed registra la pista al frente del historial MRU y persiste
// una sola vez via el seam existente. Un fallo deja audio y sesion
// activos y solo levanta el banner (PERSIST-1 saveFail).
func (m Model) recordPlayed(trackID string) Model {
	if m.State == nil {
		return m
	}
	if err := m.State.RecordPlayed(trackID); err != nil {
		m.LastError = "save state: " + err.Error()
		return m
	}
	if err := tuiStatePersister(); err != nil {
		m.LastError = "save state: " + err.Error()
	}
	return m
}

// bumpVolume clamps to [0, 100] and forwards to backend. Live state
// writes the volume and persists synchronously (PERSIST-1 volume).
func (m Model) bumpVolume(delta int) Model {
	next := m.Volume + delta
	if next > 100 {
		next = 100
	}
	if next < 0 {
		next = 0
	}
	m.Volume = next
	_ = m.Backend.SetVolume(next)
	if m.State != nil {
		if err := m.State.SetVolume(next); err == nil {
			if perr := tuiStatePersister(); perr != nil {
				m.LastError = "save state: " + perr.Error()
			}
		}
	}
	return m
}

// initPersisterForState binds tuiStatePersister to the supplied state.
func initPersisterForState(state *config.PlaybackState) {
	if state == nil {
		return
	}
	tuiStatePersister = func() error { return state.Persist() }
}

// handleEsc implements the prompt's "esc = back" rule: any
// non-home view returns to Now-Playing. From Now-Playing esc is a
// no-op so the user has to press `q` or ctrl+c to leave the TUI.
func (m Model) handleEsc() Model {
	if m.Mode == ModeNowPlaying {
		return m
	}
	m.Mode = ModeNowPlaying
	return m
}

// advanceQueue moves SelectedIdx across the catalog wrapping around
// at both ends. A nil or empty catalog is a no-op so the TUI does
// not crash when no sync has happened yet.
func (m Model) advanceQueue(delta int) Model {
	if m.Catalog == nil || len(m.Catalog.Tracks) == 0 {
		return m
	}
	idx := m.SelectedIdx + delta
	if idx < 0 {
		idx = len(m.Catalog.Tracks) - 1
	}
	if idx >= len(m.Catalog.Tracks) {
		idx = 0
	}
	m.SelectedIdx = idx
	return m
}
