package tui

import tea "github.com/charmbracelet/bubbletea"

// keyMsg synthesises a Bubble Tea KeyMsg carrying a single rune.
// It keeps the test files readable (one helper per rune, no
// verbose struct literal at every call site).
func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func spaceKeyMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeySpace}
}

func escKeyMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEsc}
}

func ctrlCKeyMsg() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlC}
}

// applyKey dispatches a KeyMsg through the model's Update method
// and unwraps the resulting tea.Model interface back into the
// concrete Model struct. It is the test-side equivalent of what
// the Bubble Tea runtime does internally between Update calls.
func applyKey(m Model, msg tea.KeyMsg) Model {
	out, _ := m.Update(msg)
	return out.(Model)
}

