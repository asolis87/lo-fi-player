package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// verifyReleaseBinaryUsage is the per-subcommand usage banner
// runVerifyReleaseBinary prints when argument parsing fails.
// The banner is split from the package-level usage so the
// dispatcher's "Usage:" grep keeps working when the operator
// mistypes the subcommand name or omits arguments.
const verifyReleaseBinaryUsage = `lofi verify-release-binary — release-pipeline gate (PR-E)

Usage:
  lofi verify-release-binary <binary-path> <expected-SHA>

Exit codes:
  0  runtime FirstRunCommitSHA matches <expected-SHA>
  1  binary is unreadable, missing, or carries the placeholder literal,
     or its runtime SHA does not match <expected-SHA>
  2  invocation error (wrong arity)
`

// runVerifyReleaseBinary implements `lofi verify-release-binary
// <binary-path> <expected-SHA>` by delegating to
// catalog.VerifyBakedSHA. Errors flow through commandError so the
// dispatcher's codeFor helper picks up the right exit code (0
// for nil, 2 for usage, 1 for verification failure).
//
// The subcommand exists to give the bash release script a
// Go-callable surface that does not depend on shell parsing:
// scripts/verify-release.sh runs `go run ./cmd/lofi
// verify-release-binary <bin> <sha>` and forwards the exit code.
// Downstream tooling can read the CLI's exit class without
// having to grep the binary itself.
//
// Argument contract:
//
//	runVerifyReleaseBinary([]string{})                  -> usage error
//	runVerifyReleaseBinary([]string{"path"})             -> usage error
//	runVerifyReleaseBinary([]string{"path", "sha", ...}) -> usage error
//
// Anything else runs the verification. The expectedSHA is taken
// verbatim from the second positional argument; we deliberately
// avoid validating its shape (40 hex chars) here because the
// release pipeline has already done that upstream and the gate
// only needs to compare bytes.
func runVerifyReleaseBinary(args []string) error {
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "lofi verify-release-binary: expected 2 arguments, got %d\n\n%s", len(args), verifyReleaseBinaryUsage)
		return &commandError{code: 2}
	}
	binaryPath, expectedSHA := args[0], args[1]
	if err := catalog.VerifyBakedSHA(binaryPath, expectedSHA); err != nil {
		switch {
		case errors.Is(err, catalog.ErrPlaceholderFound):
			fmt.Fprintf(os.Stderr, "lofi verify-release-binary: %v\n", err)
		case errors.Is(err, catalog.ErrSHAMismatch):
			fmt.Fprintf(os.Stderr, "lofi verify-release-binary: %v\n", err)
		case errors.Is(err, catalog.ErrBinaryUnreadable):
			fmt.Fprintf(os.Stderr, "lofi verify-release-binary: %v\n", err)
		default:
			fmt.Fprintf(os.Stderr, "lofi verify-release-binary: unexpected error: %v\n", err)
		}
		return &commandError{code: 1}
	}
	fmt.Fprintf(os.Stdout, "RELEASE OK: %s baked in %s\n", expectedSHA, binaryPath)
	return nil
}
