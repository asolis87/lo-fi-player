package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/tui"
)

// captureStderr runs fn with os.Stderr redirected to a pipe and
// returns both fn's exit code and the captured stderr contents.
func captureStderr(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, r); close(done) }()
	code := fn()
	_ = w.Close()
	<-done
	_ = r.Close()
	os.Stderr = orig
	return code, buf.String()
}

// captureStdout runs fn with os.Stdout redirected to a pipe and
// returns the captured stdout contents. Used by runCredits and
// runList tests.
func captureStdout(t *testing.T, fn func() error) (error, string) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, r); close(done) }()
	callErr := fn()
	_ = w.Close()
	<-done
	_ = r.Close()
	os.Stdout = orig
	return callErr, buf.String()
}

// errStubTUI is a sentinel used by play_test.go to simulate a
// failing Bubble Tea program run.
var errStubTUI = errors.New("stub: simulated TUI failure")

// stubLauncher replaces launchTUI for the duration of a test so
// the dispatcher can exercise the TUI code path without taking
// over the caller's terminal. It records every call so assertions
// can verify what the composition root handed the Bubble Tea
// program.
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

// withStubLauncher swaps launchTUI for the stub's launch method
// for the duration of the test. The original launcher is restored
// on cleanup so parallel tests can keep using different stubs.
func withStubLauncher(t *testing.T, s *stubLauncher) {
	t.Helper()
	orig := launchTUI
	launchTUI = s.launch
	t.Cleanup(func() { launchTUI = orig })
}