// Package tui hosts the Bubble Tea program that drives the lo-fi
// player's interactive mode. It owns the keymap, the four views
// (Now-Playing, Queue, Catalog, Attribution), and the non-fatal
// error banner triggered by audio backend events.
//
// The package imports only the AudioBackend port (internal/audio)
// and the catalog types (internal/catalog); it never reaches into
// concrete adapters. The composition root (cmd/lofi) wires the
// real backend and catalog before handing the Model to
// tea.NewProgram.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
)

// Mode identifies the four REQ-TUI-1 screens the user can switch
// between with single keys (p, q, c, a). The name is "Mode" rather
// than "View" because the Bubble Tea interface already exposes a
// View() method on the root model; colliding identifiers trigger
// Go compile errors.
type Mode int

const (
	// ModeNowPlaying is the default landing view; it surfaces the
	// playback controls and the non-fatal crash banner.
	ModeNowPlaying Mode = iota
	// ModeQueue shows the upcoming track list ordered like the catalog.
	ModeQueue
	// ModeCatalog lists every track grouped by license status.
	ModeCatalog
	// ModeAttribution renders the five REQ-ATT-2 fields for the
	// currently selected track.
	ModeAttribution
	// ModeNoCatalog se muestra cuando el catalogo es nulo o vacio;
	// exhibe la guia de CATALOG-1 sin invocar la red.
	ModeNoCatalog
)

// defaultVolume is the slice-#1 starting volume. It matches the
// spec's mid-point (S-AUD-2 keeps volume above 0 and below 100).
// Re-export from internal/config.DefaultVolume para que el modelo
// y el estado vivo compartan una sola fuente de verdad (VOL-1).
const defaultVolume = config.DefaultVolume

// volumeStep is how many percentage points + and - change the
// volume by per keypress.
const volumeStep = 5

// Model is the Bubble Tea root state. It owns the backend port,
// the catalog pointer, the current view, the selected track index,
// the cached volume level, the latest non-fatal error message, and
// the audio Event channel the composition root feeds asynchronously.
// SelectedIdx and Playing are not surfaced to the user yet (PR #8
// covers structure only). State is the optional live PlaybackState
// (slice-3 B1/B5); when non-nil, volume and q/Ctrl+C persist via
// tuiStatePersister.
// Las tres identidades son distintas y no deben colapsarse:
// SelectedIdx es la intencion del usuario, LoadedID lo que el backend
// acepto en el ultimo Load exitoso y PlayingID lo que confirmo Play.
// NextGeneration es el contador monotono de tokens de carga
// (slice-4 #4877); LoadedGeneration es el token del Load vigente.
// AudioMode etiqueta el backend activo para la guia visible (FBK-1) y
// ResolvePath traduce un id de catalogo a su ruta en disco: la
// composicion lo inyecta para que la TUI no conozca el layout de cache.
// RebindBackend es el seam que la composicion inyecta para que la TUI
// pueda solicitar el swap a procedural:rain cuando el backend reporta
// un error sticky irrecuperable (EVT-2 / S-EVT-2-sticky). nil inhibe
// el swap; la TUI conserva banner y backend en su lugar.
type Model struct {
	Backend          audio.AudioBackend
	Catalog          *catalog.Catalog
	Mode             Mode
	SelectedIdx      int
	Volume           int
	LastError        string
	ErrCh            chan audio.Event
	Playing          bool
	State            *config.PlaybackState
	LoadedID         string
	PlayingID        string
	LoadedGeneration uint64
	NextGeneration   uint64
	AudioMode        string
	ResolvePath      func(trackID string) string
	RebindBackend    func() (audio.AudioBackend, string, error)
}

// hasTracks reporta si el catalogo puede sostener Load/Play (NAV-2).
func (m Model) hasTracks() bool {
	return m.Catalog != nil && len(m.Catalog.Tracks) > 0
}

// pathFor resuelve la ruta en disco via el seam inyectado; sin seam
// devuelve cadena vacia y el backend decide como fallar.
func (m Model) pathFor(trackID string) string {
	if m.ResolvePath == nil {
		return ""
	}
	return m.ResolvePath(trackID)
}

// NewModel builds a fresh Model with the default mode, volume, and
// no error. The backend and catalog are stored as-is so the TUI can
// reach into them on every keypress. ErrCh may be nil — the Model
// treats a nil channel as "no async event source". La firma se
// conserva para no romper el slice-1; delega a NewModelWithState
// pasando nil como state para preservar el comportamiento anterior.
func NewModel(backend audio.AudioBackend, cat *catalog.Catalog, errCh chan audio.Event) Model {
	return NewModelWithState(backend, cat, nil, errCh)
}

// NewModelWithState hidrata el Model desde un PlaybackState vivo.
// state no-nil inicializa Volume y enlaza tuiStatePersister.
// nil/empty catalog -> ModeNoCatalog (CATALOG-1).
func NewModelWithState(backend audio.AudioBackend, cat *catalog.Catalog, state *config.PlaybackState, errCh chan audio.Event) Model {
	vol := defaultVolume
	if state != nil {
		vol = state.EffectiveVolume()
		initPersisterForState(state)
	}
	mode := ModeNowPlaying
	if cat == nil {
		mode = ModeNoCatalog
	}
	return Model{
		Backend: backend,
		Catalog: cat,
		Mode:    mode,
		Volume:  vol,
		ErrCh:   errCh,
		State:   state,
	}
}

// Init suscribe el primer Cmd de escucha sobre el canal Events() del
// backend si este implementa audio.EventSource. Init corre una sola
// vez al arrancar el programa; Update mantiene la cadena viva
// re-suscribiendose tras cada audio.Event procesado.
func (m Model) Init() tea.Cmd {
	return m.waitForAudioEventCmd()
}

// Update is the Bubble Tea Update protocol entry point. It dispatches
// keys to the keymap and accepts audio.Event values to surface a
// non-fatal banner via LastError (S-TUI-2) y para auto-avanzar el
// catalogo cuando llega EventEnd valido (EVT-1). Tras procesar un
// audio.Event re-suscribe el Cmd para mantener la cadena viva; los
// mensajes no-audio no devuelven Cmd, conservando el suscriptor
// previo hasta el proximo audio.Event.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		action, ok := bindingFor(msg)
		if !ok {
			return m, nil
		}
		return m.applyAction(action)
	case audio.Event:
		// Canal cerrado (swap/Close) produce Event{} con Type=="":
		// re-suscribimos sin tocar estado.
		if msg.Type == "" {
			return m, m.waitForAudioEventCmd()
		}
		return m.applyAudioEvent(msg), m.waitForAudioEventCmd()
	case tea.WindowSizeMsg:
		// No responsive layout in slice #1; keep the rendered text
		// static so the QUIT/keypress contract stays deterministic.
		return m, nil
	}
	return m, nil
}

// applyAudioEvent dispatch un audio.Event a su handler:
//   - EventEnd con generacion vigente: auto-avanza circularmente y
//     reproduce la siguiente pista via loadAndPlaySelected(1). Sin
//     catalogo o sin generacion vigente: ignora el evento.
//   - EventEnd con generacion obsoleta (mismatch con
//     LoadedGeneration): se ignora sin navegar, Load, Play ni
//     persistencia. Asi un end atrasado del backend no produce
//     saltos espurios.
//   - EventError: persiste el banner via LastError y, si RebindBackend
//     esta cableado, pide el swap a procedural:rain. Tras swap
//     exitoso resetea LoadedID/PlayingID/LoadedGeneration/NextGeneration
//     para que la siguiente carga asigne una generacion nueva; el
//     backend viejo queda cerrado por la composicion. Si RebindBackend
//     falla o no esta inyectado, conserva banner y backend vigentes.
func (m Model) applyAudioEvent(ev audio.Event) Model {
	switch ev.Type {
	case audio.EventEnd:
		if ev.Generation != m.LoadedGeneration {
			return m
		}
		if !m.hasTracks() {
			return m
		}
		return m.loadAndPlaySelected(1)
	case audio.EventError:
		m.LastError = ev.Message
		if m.RebindBackend == nil {
			return m
		}
		newBackend, newMode, err := m.RebindBackend()
		if err != nil || newBackend == nil {
			return m
		}
		m.Backend = newBackend
		m.AudioMode = newMode
		m.LoadedID = ""
		m.PlayingID = ""
		m.LoadedGeneration = 0
		m.NextGeneration = 0
		return m
	}
	return m
}

// waitForAudioEventCmd devuelve un tea.Cmd que lee UN evento del
// canal Events() del backend actual y lo entrega como Msg. Si el
// backend no implementa audio.EventSource retorna nil. Un canal
// cerrado (swap/Close) rinde audio.Event{} con Type=="" y Update lo
// ignora para re-suscribirse de inmediato.
func (m Model) waitForAudioEventCmd() tea.Cmd {
	src, ok := m.Backend.(audio.EventSource)
	if !ok {
		return nil
	}
	ch := src.Events()
	return func() tea.Msg {
		ev, _ := <-ch
		return ev
	}
}

// View delegates to the per-mode renderer. A non-fatal error banner
// is appended in the dispatcher so individual modes stay focused
// on their subject matter. ModeNoCatalog enruta a viewNoCatalog
// sin mostrar controles (CATALOG-1).
func (m Model) View() string {
	switch m.Mode {
	case ModeNoCatalog:
		return viewNoCatalog(m)
	case ModeQueue:
		return viewQueue(m)
	case ModeCatalog:
		return viewCatalog(m)
	case ModeAttribution:
		return viewAttribution(m)
	default:
		return viewNowPlaying(m)
	}
}
