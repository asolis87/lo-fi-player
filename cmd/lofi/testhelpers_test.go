package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/config"
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

// withStdinIsTTYStub swaps the stdinIsTTY predicate for the
// duration of the test. resumePrompter (y/N read) is the only
// caller; non-TTY tests assert that the prompter NEVER invokes
// Read on resumeReader.
func withStdinIsTTYStub(t *testing.T, isTTY bool) {
	t.Helper()
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return isTTY }
	t.Cleanup(func() { stdinIsTTY = orig })
}

// withResumeReaderStub swaps resumeReader for the supplied reader
// for the duration of the test. Tests pair it with
// withStdinIsTTYStub(true) to drive the TTY accept/decline
// decision deterministically without opening a real terminal.
func withResumeReaderStub(t *testing.T, r io.Reader) {
	t.Helper()
	orig := resumeReader
	resumeReader = r
	t.Cleanup(func() { resumeReader = orig })
}

// countingReader counts every Read call without consuming any
// bytes. Tests use it to assert the non-TTY path never touches
// stdin (RESUME-2).
type countingReader struct {
	calls *atomic.Int32
}

func newCountingReader() *countingReader {
	return &countingReader{calls: &atomic.Int32{}}
}

func (c *countingReader) Read(_ []byte) (int, error) {
	c.calls.Add(1)
	return 0, io.EOF
}

func (c *countingReader) Calls() int32 { return c.calls.Load() }

// withLoadStateForResumeStub swaps loadStateForResume for the
// duration of the test. The original is restored on cleanup so
// parallel tests stay isolated.
func withLoadStateForResumeStub(t *testing.T, fn func() *config.PlaybackState) {
	t.Helper()
	orig := loadStateForResume
	loadStateForResume = fn
	t.Cleanup(func() { loadStateForResume = orig })
}

// withCLIFinalizeStub swaps cliSignalFinalize and resets cliFinalizeOnce.
func withCLIFinalizeStub(t *testing.T, fn func(*config.PlaybackState) error) {
	t.Helper()
	orig := cliSignalFinalize
	cliSignalFinalize = fn
	cliFinalizeOnce = sync.Once{}
	t.Cleanup(func() { cliSignalFinalize = orig })
}
