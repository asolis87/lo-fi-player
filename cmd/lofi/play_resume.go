package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
	"github.com/asolis87/lo-fi-player/internal/tui"
)

// resumePromptText es el unico texto que runInteractiveResume
// escribe a stdout antes de leer stdin.
const resumePromptText = "Resume previous session? [y/N]: "

// loadCatalogForResume provee el catalogo para la sesion
// interactiva. nil degrada a ModeNoCatalog (CATALOG-1).
var loadCatalogForResume = func() (*catalog.Catalog, error) {
	root, err := catalogCacheDir()
	if err != nil {
		return nil, err
	}
	cat, err := catalog.LoadFromDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return cat, nil
}

// loadStateForResume devuelve el PlaybackState persistido o nil
// si no hay estado previo (caller omite el prompt).
var loadStateForResume = config.LoadPlaybackState

// stdinIsTTY detecta terminal interactiva via os.Stdin.Stat()
// para evitar promover github.com/charmbracelet/x/term.
var stdinIsTTY = func() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// resumeReader es el io.Reader del que resumePrompter lee.
var resumeReader io.Reader = os.Stdin

// resumePrompter escribe el prompt y lee un byte. y/Y -> resume;
// n/N/EOF -> decline. Errores distintos de EOF se propagan.
var resumePrompter = func(w io.Writer, r io.Reader) (bool, error) {
	if _, err := fmt.Fprint(w, resumePromptText); err != nil {
		return false, err
	}
	buf := make([]byte, 1)
	n, err := r.Read(buf)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	switch buf[0] {
	case 'y', 'Y':
		return true, nil
	}
	return false, nil
}

// runInteractiveResume es la rama sin argumentos de `lofi play`:
// RESUME-1 TTY prompt, RESUME-2 non-TTY auto-hydrate, o silencio
// si no hay estado previo. La rama headless (`<id>`) sigue en
// play.go via runHeadlessPlay (explicit-id-wins).
func runInteractiveResume() error {
	cat, _ := loadCatalogForResume()
	state := loadStateForResume()
	hydrated, err := decideResumeState(state, cat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: resume: %v\n", err)
		return &commandError{code: 1}
	}
	backend := audio.NewMockBackend()
	m := tui.NewModelWithState(backend, cat, hydrated, nil)
	if err := launchTUI(m); err != nil {
		fmt.Fprintf(os.Stderr, "lofi play: tui: %v\n", err)
		return &commandError{code: 1}
	}
	return nil
}

// decideResumeState: RESUME-1/2 + FILTER-1/2. Filtra historia (FILTER-1);
// declinacion conserva volumen y vacia historia (RESUME-1 s2); volume-only
// hidrata sin prompt (PERSIST-1).
func decideResumeState(state *config.PlaybackState, cat *catalog.Catalog) (*config.PlaybackState, error) {
	if state == nil {
		return nil, nil
	}
	resume := true
	if stdinIsTTY() {
		var err error
		resume, err = resumePrompter(os.Stdout, resumeReader)
		if err != nil {
			return nil, err
		}
	}
	hydrated := config.Default()
	hydrated.Volume = state.EffectiveVolume()
	if resume {
		hydrated.History = filterHistoryAgainstCatalog(state.History(), cat)
	}
	return config.NewPlaybackState(hydrated), nil
}
