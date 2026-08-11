package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/tui"
)

// tuiLauncher runs the Bubble Tea program. Tests swap it to a
// stub so they can capture the model without taking over the
// terminal (Bubble Tea's WithInput / WithOutput paths require a
// TTY in some environments).
type tuiLauncher func(m tui.Model) error

// launchTUI is the production launcher. Override in tests only.
var launchTUI tuiLauncher = func(m tui.Model) error {
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

// runPlay dispatches the `play` subcommand. With no positional
// argument the TUI takes over the terminal (REQ-CLI-2 / REQ-TUI-1);
// with an argument the headless path prints a not-yet-implemented
// banner and exits non-zero so PR #9 (CLI dispatch + queue) and the
// seeding work in PR #10 can land it later without breaking
// existing dispatcher tests.
func runPlay(args []string) error {
	if len(args) == 0 {
		backend := audio.NewMockBackend()
		m := tui.NewModel(backend, nil, nil)
		if err := launchTUI(m); err != nil {
			fmt.Fprintf(os.Stderr, "lofi play: tui: %v\n", err)
			return &commandError{code: 1}
		}
		return nil
	}
	fmt.Fprintf(os.Stderr, "lofi play: headless mode for %v is not yet implemented\n", args)
	return &commandError{code: 1}
}
