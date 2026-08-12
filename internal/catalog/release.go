// Release-binary verification gate for the catalog seed SHA.
//
// The release pipeline (release.yml) builds a binary via
// `go build -ldflags "-X ...FirstRunCommitSHA=<sha>"` and runs
// scripts/verify-release.sh against it before uploading. This
// file gives the same gate a Go-callable surface so unit tests
// can exercise it without spawning a shell.
//
// Spec background (openspec/changes/slice-2/specs/release-sha-bake/spec.md):
// a release MUST be blocked if the placeholder literal
// `<PLACEHOLDER_SHA>` survives in the binary. The placeholder is
// the value FirstRunCommitSHA ships with in pinning.go until the
// release pipeline rewrites it; if the pipeline was skipped (a
// manual `go build` was uploaded by mistake) the literal is what
// the binary will bake and the release MUST be stopped.
//
// NOTE on Go linker behavior: `go build -ldflags -X` rewrites
// the runtime value of the target string variable, but the
// linker ALSO keeps the original source-code literal in the
// rodata section (it appears as a substring of an adjacent
// runtime string when read with the `strings` utility, because
// Go rodata strings are length-prefixed and not null-terminated).
// This is why the bytes-only grep here intentionally matches on
// the literal characters; the production gate is conservative
// and prefers a false-positive over shipping a binary whose
// runtime SHA was never overridden.
package catalog

import (
	"fmt"
	"os"
	"regexp"
)

// PlaceholderLiteral is the exact substring the release gate
// searches for in a release binary. It is exported so the bash
// script and any downstream tooling share the same constant.
const PlaceholderLiteral = "<PLACEHOLDER_SHA>"

// ErrPlaceholderFoundInBinary is wrapped around every
// VerifyNoPlaceholder failure whose root cause is the literal
// appearing in the bytes of the binary under inspection. Callers
// match with errors.Is so a CI step can branch on the failure
// class.
var ErrPlaceholderFoundInBinary = fmt.Errorf("catalog: placeholder %s found in release binary", PlaceholderLiteral)

// ErrBinaryUnreadable is wrapped around every VerifyNoPlaceholder
// failure whose root cause is the file itself (missing,
// permission-denied, not a regular file). Callers match with
// errors.Is so a missing artifact can be distinguished from a
// placeholder leak.
var ErrBinaryUnreadable = fmt.Errorf("catalog: cannot read release binary")

// placeholderPattern compiles once at package init. The literal
// is short and ASCII so the regex never does any work that a
// plain strings.Contains would not, but using regexp keeps the
// implementation symmetrical with future relaxations (e.g. allow
// the literal if it is followed by a marker that disambiguates
// "linting residual" from "baked-in default").
var placeholderPattern = regexp.MustCompile(regexp.QuoteMeta(PlaceholderLiteral))

// VerifyNoPlaceholder reads binaryPath and returns an error if
// the placeholder literal appears anywhere in its bytes. The
// function is the Go-callable counterpart to
// scripts/verify-release.sh: both call paths must agree, which is
// why the bash script delegates the actual byte scan to this
// function via `go run` if invoked in-tree. A missing file
// returns an error wrapping ErrBinaryUnreadable; a present
// placeholder returns an error wrapping
// ErrPlaceholderFoundInBinary. nil means the binary is safe to
// ship.
func VerifyNoPlaceholder(binaryPath string) error {
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrBinaryUnreadable, binaryPath, err)
	}
	if placeholderPattern.Match(data) {
		return fmt.Errorf("%w: path=%s", ErrPlaceholderFoundInBinary, binaryPath)
	}
	return nil
}