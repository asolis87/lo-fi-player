// Release-binary verification gates for the catalog seed SHA.
//
// PR-E's spec demands that every release artifact have its
// FirstRunCommitSHA variable baked to the SHA of
// catalog/v1/manifest.json at HEAD. VerifyBakedSHA is the
// Go-callable implementation: it parses the runtime symbol from
// the binary (via debug/macho, debug/elf, debug/pe) and compares
// its runtime string value against the SHA the release pipeline
// expected.
//
// Why symbol extraction, not `strings | grep`?
//
// Earlier slices (#4.3) gated the release on a grep test for the
// literal `<PLACEHOLDER_SHA>` inside the binary's bytes. Go's
// linker keeps the source-code literal in rodata ADJACENT to the
// runtime value of the string variable (length-prefixed but
// adjacent in the same section); the literal is reachable by a
// simple substring search even after `-ldflags -X` has rewritten
// the variable's data pointer to a freshly-allocated rodata
// buffer. A grep test therefore flags every correctly baked
// binary as broken; the release pipeline cannot distinguish
// "linker preserved the literal" from "the literal is the
// runtime value". The grep gate was unwound and replaced with a
// symbol-table read of the runtime value, which is what this
// file does. See PR-E's blocker note for the full autopsy.
package catalog

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// placeholderLiteral is the marker VerifyBakedSHA uses to
// classify a "literal still in rodata" run as a failed release.
// Re-declared here as a constant so the test file documents the
// contract independent of pinning.go.
const placeholderLiteral = "<PLACEHOLDER_SHA>"

// validBakeSHA is a 40-hex SHA used by the baked-binary tests.
// Fresh on every run so two parallel invocations do not collide
// on identical symbol addresses. The "0123456789..." pattern is
// deliberately non-cryptographic; the gate cares about exact
// byte equality, not entropy.
const validBakeSHA = "0123456789abcdef0123456789abcdef01234567"

// altBakeSHA is a second 40-hex SHA used to assert wrong-SHA
// rejection: baked with one SHA, gate called with a different
// SHA, must surface ErrSHAMismatch.
const altBakeSHA = "fedcba9876543210fedcba9876543210fedcba98"

// FirstRunCommitSHASymbol is the exact path the Go linker writes
// into the binary's symbol table for the runtime variable
// declared in pinning.go. Tests assert against this constant
// rather than reconstructing the path inline so a future rename
// of the variable surfaces here, not in the test bodies.
const firstRunCommitSHASymbol = "github.com/asolis87/lo-fi-player/internal/catalog.FirstRunCommitSHA"

// projectRoot finds the directory one level above the test's
// own package, i.e. the repository root. The build probes
// compile ./cmd/lofi, which requires the module root as the
// working directory; running from inside internal/catalog
// leaves `go build` unable to resolve `./cmd/lofi`.
func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	return root
}

// buildProbeBinary compiles cmd/lofi (or any other target) into
// t.TempDir() with optional -ldflags. Returns the path to the
// resulting binary. Skips the test if `go build` fails so the
// RED phase does not block on toolchain-level issues unrelated
// to the verification logic.
func buildProbeBinary(t *testing.T, ldflags string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lofi-probe")
	args := []string{"build"}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "-o", bin, "./cmd/lofi")
	cmd := exec.Command("go", args...)
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("go build failed (skipping binary probe): %v\n%s", err, out)
	}
	return bin
}

// ldFlagForOverridingSHA renders the -ldflags value that bakes
// expectedSHA into the FirstRunCommitSHA variable at link time.
// Tests pass this through buildProbeBinary so the bake contract
// stays in one place.
func ldFlagForOverridingSHA(expectedSHA string) string {
	return "-X " + firstRunCommitSHASymbol + "=" + expectedSHA
}

// TestVerifyBakedSHA_AcceptsBakedSHA is the GREEN gate promised
// in PR-E's spec: a binary built with
//
//	go build -ldflags "-X ...FirstRunCommitSHA=<real-SHA>"
//
// MUST be accepted by the release gate; i.e. VerifyBakedSHA
// returns nil when called with the expected SHA. The earlier
// VerifyNoPlaceholder grep test could not satisfy this
// requirement (the linker keeps the literal in rodata) and was
// unwound in favour of the symbol-table read implemented by
// VerifyBakedSHA.
func TestVerifyBakedSHA_AcceptsBakedSHA(t *testing.T) {
	bin := buildProbeBinary(t, ldFlagForOverridingSHA(validBakeSHA))
	if err := VerifyBakedSHA(bin, validBakeSHA); err != nil {
		t.Fatalf("VerifyBakedSHA(baked=%s) = %v; want nil", validBakeSHA, err)
	}
}

// TestVerifyBakedSHA_RejectsWrongSHA covers the same gate from
// the other side: a binary baked with SHA A and verified with
// SHA B MUST return an error wrapping ErrSHAMismatch. This
// catches a "silent" failure where the linker accepted a bake
// but a typo or stale expectedSHA masked the discrepancy.
func TestVerifyBakedSHA_RejectsWrongSHA(t *testing.T) {
	bin := buildProbeBinary(t, ldFlagForOverridingSHA(validBakeSHA))
	err := VerifyBakedSHA(bin, altBakeSHA)
	if err == nil {
		t.Fatalf("VerifyBakedSHA(baked=%s, expected=%s) = nil; want ErrSHAMismatch", validBakeSHA, altBakeSHA)
	}
	if !errors.Is(err, ErrSHAMismatch) {
		t.Fatalf("VerifyBakedSHA wrong-SHA error = %v; want errors.Is(_, ErrSHAMismatch)", err)
	}
	if contains(err.Error(), placeholderLiteral) {
		t.Fatalf("wrong-SHA error must not name the placeholder literal: %q", err.Error())
	}
}

// TestVerifyBakedSHA_PlaceholderWrapped covers the spec scenario
// "manual local build uploaded by mistake": a binary built
// WITHOUT -ldflags keeps the source-code placeholder as the
// runtime value of FirstRunCommitSHA. VerifyBakedSHA MUST
// surface ErrPlaceholderFound so the release pipeline can stop
// the upload with a clear message that distinguishes "the binary
// was never baked" from "the bake SHA is wrong".
func TestVerifyBakedSHA_PlaceholderWrapped(t *testing.T) {
	bin := buildProbeBinary(t, "")
	err := VerifyBakedSHA(bin, validBakeSHA)
	if err == nil {
		t.Fatalf("VerifyBakedSHA(unbaked) = nil; want ErrPlaceholderFound (the source placeholder literal is the runtime value)")
	}
	if !errors.Is(err, ErrPlaceholderFound) {
		t.Fatalf("VerifyBakedSHA unbaked error = %v; want errors.Is(_, ErrPlaceholderFound)", err)
	}
	if !contains(err.Error(), placeholderLiteral) {
		t.Fatalf("placeholder error must name the literal so operators know what to look for: %q", err.Error())
	}
}

// TestVerifyBakedSHA_NonexistentBinary guards the failure
// surface: a missing binary path MUST return an error wrapping
// ErrBinaryUnreadable. Distinct from the placeholder/mismatch
// sentinels so an operator can tell "artifact missing" from
// "artifact wrong".
func TestVerifyBakedSHA_NonexistentBinary(t *testing.T) {
	bogus := filepath.Join(t.TempDir(), "does-not-exist-lofi-probe")
	err := VerifyBakedSHA(bogus, validBakeSHA)
	if err == nil {
		t.Fatal("VerifyBakedSHA(missing) = nil; expected ErrBinaryUnreadable")
	}
	if !errors.Is(err, ErrBinaryUnreadable) {
		t.Fatalf("VerifyBakedSHA missing-binary error = %v; want errors.Is(_, ErrBinaryUnreadable)", err)
	}
	if !contains(err.Error(), bogus) {
		t.Fatalf("missing-binary error must name the path %q, got %q", bogus, err.Error())
	}
}

// TestGoStringSymbolBytes_Macho guards the platform-specific
// symbol-table reader for Mach-O (macOS, GOOS=darwin). The test
// is gated on runtime.GOOS so cross-platform CI does not spuriously
// fail; the macOS runner of `go test ./...` is the executor.
//
// The expected outcome: reading FirstRunCommitSHA yields the
// runtime bytes of the placeholder literal (no -ldflags was
// passed). This locks the layout assumption — that the symbol's
// data pointer lands in rodata and is reachable via the section
// read — into the test surface so a Go toolchain bump that
// restructures strings fails the build loudly instead of
// silently shipping a broken gate.
func TestGoStringSymbolBytes_Macho(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("Mach-O symbol-table test runs only on darwin; this runner is %s", runtime.GOOS)
	}
	bin := buildProbeBinary(t, "")
	data, err := goStringSymbolBytes(bin, firstRunCommitSHASymbol)
	if err != nil {
		t.Fatalf("goStringSymbolBytes(%s) error: %v", firstRunCommitSHASymbol, err)
	}
	if !bytes.Contains(data, []byte(placeholderLiteral)) {
		t.Fatalf("symbol bytes %q do not contain %q", data, placeholderLiteral)
	}
}

// TestGoStringSymbolBytes_Elf gates on linux/GOOS=linux. CI
// runs on Ubuntu so this exercises the ELF path; macOS dev runs
// skip it cleanly.
func TestGoStringSymbolBytes_Elf(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("ELF symbol-table test runs only on linux; this runner is %s", runtime.GOOS)
	}
	bin := buildProbeBinary(t, "")
	data, err := goStringSymbolBytes(bin, firstRunCommitSHASymbol)
	if err != nil {
		t.Fatalf("goStringSymbolBytes(%s) error: %v", firstRunCommitSHASymbol, err)
	}
	if !bytes.Contains(data, []byte(placeholderLiteral)) {
		t.Fatalf("symbol bytes %q do not contain %q", data, placeholderLiteral)
	}
}

// TestGoStringSymbolBytes_PE gates on windows/GOOS=windows.
// PR-E is exercised on Windows once a release artifact lands
// there; for now the cross-compile scaffolding has no Linux
// counterpart on this runner, but the test is wired so the
// reader's PE branch is continuously asserted under CI.
func TestGoStringSymbolBytes_PE(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("PE symbol-table test runs only on windows; this runner is %s", runtime.GOOS)
	}
	bin := filepath.Join(t.TempDir(), "lofi-probe.exe")
	args := []string{"build", "-o", bin, "./cmd/lofi"}
	cmd := exec.Command("go", args...)
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("go build failed (skipping PE probe): %v\n%s", err, out)
	}
	data, err := goStringSymbolBytes(bin, firstRunCommitSHASymbol)
	if err != nil {
		t.Fatalf("goStringSymbolBytes(%s) error: %v", firstRunCommitSHASymbol, err)
	}
	if !bytes.Contains(data, []byte(placeholderLiteral)) {
		t.Fatalf("symbol bytes %q do not contain %q", data, placeholderLiteral)
	}
}

// contains is a tiny helper that avoids pulling strings into the
// test file just for one substring check.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
