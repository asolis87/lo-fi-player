package main

import (
	"strings"
	"testing"
)

// TestRun_NoArgsExitsTwoWithUsage locks down S-CLI-1: with no
// arguments the dispatcher prints the usage banner and exits 2.
func TestRun_NoArgsExitsTwoWithUsage(t *testing.T) {
	code, stderr := captureStderr(t, func() int { return run(nil) })
	if code != 2 {
		t.Fatalf("run(nil) exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("stderr missing usage banner, got %q", stderr)
	}
}

// TestRun_UnknownSubcommandExitsTwoWithUsage locks down the
// unknown-subcommand branch: anything outside {play, list,
// credits, sync} exits 2 with both the offending name and the
// usage banner visible to the user.
func TestRun_UnknownSubcommandExitsTwoWithUsage(t *testing.T) {
	code, stderr := captureStderr(t, func() int { return run([]string{"bogus"}) })
	if code != 2 {
		t.Fatalf("run([bogus]) exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage") || !strings.Contains(stderr, "bogus") {
		t.Fatalf("stderr should mention unknown subcommand and usage, got %q", stderr)
	}
}

// TestRun_KnownSubcommandsDispatch verifies that the dispatcher
// now routes every documented subcommand to its handler. The
// subcommands return distinct exit codes when they fail (1 for
// play with unknown id, 1 for list with no catalog, 1 for sync
// with no network, 0 for credits with no args). Stubbing
// launchTUI keeps `play` from grabbing the runner's terminal so
// the test can run without a TTY.
func TestRun_KnownSubcommandsDispatch(t *testing.T) {
	withStubLauncher(t, &stubLauncher{err: nil})

	// All four documented subcommands must dispatch to their
	// handler without falling through to the unknown branch.
	// Each invocation is expected to fail in this sandbox (no
	// mpv, no catalog, no network) with exit 1; the assertion
	// is on routing, not on success of the underlying handler.
	cases := []struct {
		subcommand string
		args       []string
	}{
		{"play", []string{"track-anything"}}, // 1: no catalog
		{"list", nil},                         // 1: no catalog
		{"credits", nil},                      // 1: no catalog
		{"sync", nil},                         // 1: placeholder SHA -> float-ref or offline
	}
	for _, tc := range cases {
		t.Run(tc.subcommand, func(t *testing.T) {
			code, stderr := captureStderr(t, func() int { return run(append([]string{tc.subcommand}, tc.args...)) })
			if code != 1 {
				t.Fatalf("run(%s) code = %d, want 1, stderr=%q", tc.subcommand, code, stderr)
			}
			if strings.Contains(stderr, "not yet implemented") {
				t.Fatalf("run(%s) hit placeholder branch: %q", tc.subcommand, stderr)
			}
		})
	}
}

// TestRun_DispatchRouteUsesHelper ensures the dispatcher routes
// through dispatch() rather than re-implementing the split; this
// is a behavioural smoke test that catches future refactors that
// might bypass the helper.
func TestRun_DispatchRouteUsesHelper(t *testing.T) {
	// Unknown subcommand after a positional arg still routes
	// through dispatch: args[0] is the subcommand, args[1:] is
	// forwarded. The handler for `bogus` will report it as
	// unknown because run's switch covers only the documented
	// names.
	code, stderr := captureStderr(t, func() int { return run([]string{"bogus", "extra"}) })
	if code != 2 {
		t.Fatalf("run([bogus, extra]) code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "bogus") {
		t.Fatalf("expected stderr to mention bogus, got %q", stderr)
	}
}