package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
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

func TestRun_NoArgsExitsTwoWithUsage(t *testing.T) {
	code, stderr := captureStderr(t, func() int { return run(nil) })
	if code != 2 {
		t.Fatalf("run(nil) exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("stderr missing usage banner, got %q", stderr)
	}
}

func TestRun_UnknownSubcommandExitsTwoWithUsage(t *testing.T) {
	code, stderr := captureStderr(t, func() int { return run([]string{"bogus"}) })
	if code != 2 {
		t.Fatalf("run([bogus]) exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage") || !strings.Contains(stderr, "bogus") {
		t.Fatalf("stderr should mention unknown subcommand and usage, got %q", stderr)
	}
}

func TestRun_KnownPlaceholderSubcommandsExitZero(t *testing.T) {
	for _, sub := range []string{"play", "list", "sync"} {
		if code, _ := captureStderr(t, func() int { return run([]string{sub}) }); code != 0 {
			t.Errorf("run([%q]) exit code = %d, want 0", sub, code)
		}
	}
}
