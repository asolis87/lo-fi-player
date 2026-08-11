package tui

import tea "github.com/charmbracelet/bubbletea"

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

// applyAction turns a keymap action into model state and, when the
// action triggers the program to exit, into a tea.Quit command.
// Returning the model by value is the Bubble Tea convention.
func (m Model) applyAction(a keyAction) (tea.Model, tea.Cmd) {
	switch a {
	case actionPlayPause:
		return m.togglePlay(), nil
	case actionEsc:
		return m.handleEsc(), nil
	case actionQuit:
		return m, tea.Quit
	case actionNavNowPlaying:
		m.Mode = ModeNowPlaying
	case actionNavQueue:
		// Contextual per the prompt: "q (en now-playing) = quit",
		// otherwise "q" reaches the Queue view per REQ-TUI-1.
		if m.Mode == ModeNowPlaying {
			return m, tea.Quit
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
		return m.advanceQueue(1), nil
	case actionPrev:
		return m.advanceQueue(-1), nil
	}
	return m, nil
}

// togglePlay delegates to the AudioBackend port. It does NOT call
// Load because the queue selection belongs to PR #9 (CLI dispatch
// + queue); the MockBackend in slice-1 tests just flips its
// in-memory flag.
func (m Model) togglePlay() Model {
	playing, _, _ := m.Backend.State()
	if playing {
		if err := m.Backend.Pause(); err == nil {
			m.Playing = false
		}
		return m
	}
	if err := m.Backend.Play(); err == nil {
		m.Playing = true
	}
	return m
}

// bumpVolume clamps the new value to the [0, 100] range that the
// AudioBackend port enforces (ErrVolumeOutOfRange) and forwards the
// call to the backend. Underflow is impossible because of the clamp,
// matching S-AUD-2.
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
	return m
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
