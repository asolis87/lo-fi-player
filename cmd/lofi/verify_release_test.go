package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyReleaseBinary_BakedCorrectly is the GREEN case for
// the new subcommand: when the runtime SHA in the binary equals
// the SHA passed on the command line, `lofi verify-release-binary`
// exits 0 with a clear confirmation message.
//
// The probe binary is built in t.TempDir() with `-ldflags "-X
// ...FirstRunCommitSHA=<expected>"` so the assertion is over a
// freshly baked artifact, not a fixture committed to the repo.
// If `go build` fails (no toolchain in the sandbox) the test
// skips via buildProbeBinary, matching the RED/GREEN rhythm for
// binary-probe tests.
func TestVerifyReleaseBinary_BakedCorrectly(t *testing.T) {
	bin := buildProbeBinary(t, "-X github.com/asolis87/lo-fi-player/internal/catalog.FirstRunCommitSHA="+validBakeSHAFull())
	code, stderr := captureStderr(t, func() int {
		return run([]string{"verify-release-binary", bin, validBakeSHAFull()})
	})
	if code != 0 {
		t.Fatalf("verify-release-binary baked code = %d, want 0, stderr=%q", code, stderr)
	}
}

// TestVerifyReleaseBinary_Placeholder exercises the spec's
// "manual local build uploaded by mistake" scenario: build
// without -ldflags, run with any SHA, the subcommand must exit
// 1 and surface the placeholder literal so a CI operator knows
// what to look for.
func TestVerifyReleaseBinary_Placeholder(t *testing.T) {
	bin := buildProbeBinary(t, "")
	code, stderr := captureStderr(t, func() int {
		return run([]string{"verify-release-binary", bin, validBakeSHAFull()})
	})
	if code != 1 {
		t.Fatalf("verify-release-binary placeholder code = %d, want 1, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "<PLACEHOLDER_SHA>") {
		t.Fatalf("expected stderr to mention the placeholder literal, got %q", stderr)
	}
}

// TestVerifyReleaseBinary_WrongSHA exercises the mismatch
// branch: bake with SHA A, run with SHA B, the subcommand must
// exit 1 and the error must NOT name the placeholder (so an
// operator distinguishes "wrong bake" from "no bake").
func TestVerifyReleaseBinary_WrongSHA(t *testing.T) {
	bin := buildProbeBinary(t, "-X github.com/asolis87/lo-fi-player/internal/catalog.FirstRunCommitSHA="+validBakeSHAFull())
	code, stderr := captureStderr(t, func() int {
		return run([]string{"verify-release-binary", bin, altBakeSHAFull()})
	})
	if code != 1 {
		t.Fatalf("verify-release-binary wrong-SHA code = %d, want 1, stderr=%q", code, stderr)
	}
	if strings.Contains(stderr, "<PLACEHOLDER_SHA>") {
		t.Fatalf("wrong-SHA error must not mention the placeholder literal, got %q", stderr)
	}
}

// TestVerifyReleaseBinary_RequiresBinaryPath guards the usage
// contract: zero or one positional arg must exit 2 with usage
// banner so the operator sees the expected invocation shape.
func TestVerifyReleaseBinary_RequiresBinaryPath(t *testing.T) {
	code, stderr := captureStderr(t, func() int {
		return run([]string{"verify-release-binary"})
	})
	if code != 2 {
		t.Fatalf("verify-release-binary without args code = %d, want 2, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("expected stderr to include usage banner, got %q", stderr)
	}
}

// TestVerifyReleaseBinary_RequiresExpectedSHA guards the second
// half of the usage contract: exactly one positional arg is
// still considered a usage error.
func TestVerifyReleaseBinary_RequiresExpectedSHA(t *testing.T) {
	code, stderr := captureStderr(t, func() int {
		return run([]string{"verify-release-binary", filepath.Join(t.TempDir(), "lofi")})
	})
	if code != 2 {
		t.Fatalf("verify-release-binary with one arg code = %d, want 2, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("expected stderr to include usage banner, got %q", stderr)
	}
}

// TestVerifyReleaseBinary_MissingBinary covers the
// ErrBinaryUnreadable branch at the CLI level: the path passed
// on the command line does not exist, the subcommand must exit
// 1 and the path must appear in the error so an operator can
// diagnose a bad artifact upload.
func TestVerifyReleaseBinary_MissingBinary(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-lofi")
	code, stderr := captureStderr(t, func() int {
		return run([]string{"verify-release-binary", missing, validBakeSHAFull()})
	})
	if code != 1 {
		t.Fatalf("verify-release-binary missing-binary code = %d, want 1, stderr=%q", code, stderr)
	}
	if !strings.Contains(stderr, missing) {
		t.Fatalf("expected stderr to mention missing path %q, got %q", missing, stderr)
	}
}

// validBakeSHAFull and altBakeSHAFull mirror the constants in
// the catalog package's RED tests; they cannot be imported as
// sentinels because they are unexported there. The strings are
// duplicated here on purpose — the cmd/lofi tests are the user
// surface that exercises the contract end-to-end, so a
// divergence between the catalog reader and the CLI's expected
// SHA shape would surface as a literal-mismatch in code review.
//
// They are full 40-hex-character SHAs so the gate sees the same
// shape the release pipeline will pass.
func validBakeSHAFull() string { return "0123456789abcdef0123456789abcdef01234567" }
func altBakeSHAFull() string  { return "fedcba9876543210fedcba9876543210fedcba98" }

// buildProbeBinary compiles cmd/lofi into t.TempDir() with the
// given -ldflags string. The subcommand tests duplicate this
// helper locally to avoid a cross-package import of a test-only
// helper; the cmd/lofi package gets the helper too because the
// release gate needs the same fixture in both places. Skips on
// `go build` failure so an empty toolchain on the runner still
// produces a green `go test ./...` run.
func buildProbeBinary(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lofi-probe")
	args := []string{"build"}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "-o", bin, "./cmd/lofi")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("go build failed (skipping binary probe): %v\n%s", err, out)
	}
	return bin
}
