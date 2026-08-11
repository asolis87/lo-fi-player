package main

import (
	"strings"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/tui"
)

// stubLauncher replaces launchTUI for the duration of a test so the
// dispatcher can exercise the TUI code path without taking over the
// caller's terminal. It records every call so assertions can verify
// what the composition root handed the Bubble Tea program.
type stubLauncher struct {
	called bool
	model  tui.Model
	err    error
}

func (s *stubLauncher) launch(m tui.Model) error {
	s.called = true
	s.model = m
	return s.err
}

func withStubLauncher(t *testing.T, s *stubLauncher) {
	t.Helper()
	orig := launchTUI
	launchTUI = s.launch
	t.Cleanup(func() { launchTUI = orig })
}

// TestRun_Play_NoArgs_LaunchesTUI covers REQ-CLI-2: with no
// argument, `lofi play` MUST launch the interactive TUI. The stub
// captures the model so the assertion does not need a real TTY.
func TestRun_Play_NoArgs_LaunchesTUI(t *testing.T) {
	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)

	code, _ := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
	if code != 0 {
		t.Fatalf("runPlay(nil) code = %d, want 0", code)
	}
	if !stub.called {
		t.Fatalf("runPlay(nil) did not invoke the TUI launcher")
	}
	if stub.model.Backend == nil {
		t.Fatalf("TUI Model.Backend is nil; composition root did not wire the audio port")
	}
	// The Model surfaces the audio port through the AudioBackend
	// interface; the concrete type in this test is the slice-#1
	// mock. PR #9 will swap this for the procedural / mpv
	// adapter based on availability.
	if _, ok := stub.model.Backend.(*audio.MockBackend); !ok {
		t.Fatalf("TUI Model.Backend = %T, want *audio.MockBackend", stub.model.Backend)
	}
	if stub.model.Mode != tui.ModeNowPlaying {
		t.Fatalf("TUI Model.Mode = %v, want ModeNowPlaying", stub.model.Mode)
	}
}

// TestRun_Play_WithArg_KeepsHeadlessPlaceholder preserves the
// pre-PR-#8 behaviour for the headless form until PR #9 wires
// the queue playback through `lofi play track_id`.
func TestRun_Play_WithArg_KeepsHeadlessPlaceholder(t *testing.T) {
	stub := &stubLauncher{err: nil}
	withStubLauncher(t, stub)

	code, stderr := captureStderr(t, func() int {
		return codeFor(runPlay([]string{"track_drizzle"}))
	})
	if code != 1 {
		t.Fatalf("runPlay([track_drizzle]) code = %d, want 1 (placeholder)", code)
	}
	if stub.called {
		t.Fatalf("headless path must not invoke the TUI launcher")
	}
	if !strings.Contains(stderr, "headless") {
		t.Fatalf("expected headless placeholder banner, got %q", stderr)
	}
}

// TestRun_Play_NoArgs_TUIErrorPropagates ensures a non-nil launcher
// error surfaces as exit code 1 so reviewers can distinguish a TUI
// runtime failure from a successful clean exit.
func TestRun_Play_NoArgs_TUIErrorPropagates(t *testing.T) {
	stub := &stubLauncher{err: errStubTUI}
	withStubLauncher(t, stub)

	code, stderr := captureStderr(t, func() int {
		return codeFor(runPlay(nil))
	})
	if code != 1 {
		t.Fatalf("runPlay(nil) with failing launcher: code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "tui") {
		t.Fatalf("expected TUI failure surfaced to stderr, got %q", stderr)
	}
}
