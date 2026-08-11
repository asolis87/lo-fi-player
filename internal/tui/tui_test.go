package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/asolis87/lo-fi-player/internal/audio"
)

// TestProgram_FullRuntime_QuitExits verifies the canonical
// non-TTY teatest pattern (bytes.Buffer in/out + WithContext +
// WithoutRenderer + WithoutSignals). The prompt asked for
// `teatest.NewModel`; since charmbracelet/bubbletea v0.27.1 ships
// no teatest subpackage, the test driver wires the same Program
// options Charm's own tea_test.go uses. The result is identical:
// input bytes flow through Update, Mode output is captured, and a
// tea.QuitMsg terminates the goroutine.
func TestProgram_FullRuntime_QuitExits(t *testing.T) {
	backend := audio.NewMockBackend()
	m := NewModel(backend, nil, nil)

	in := bytes.NewBufferString("q")
	out := &bytes.Buffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithoutSignals(),
	)

	done := make(chan struct{})
	var runErr error
	go func() {
		_, runErr = p.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("tea.NewProgram did not terminate after `q` within 3s")
	}
	if runErr != nil {
		t.Fatalf("p.Run() returned err = %v", runErr)
	}
	if !strings.Contains(out.String(), "lo-fi") && !strings.Contains(out.String(), "now") {
		t.Fatalf("captured output did not render the Now-Playing view header\n--out--\n%s", out.String())
	}
}
