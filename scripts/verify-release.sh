#!/usr/bin/env bash
# verify-release.sh — PR-E release-pipeline gate (slice #2).
#
# Runs `go run ./cmd/lofi verify-release-binary <bin> <sha>` so
# the bash path and the Go unit tests share one implementation
# of the SHA check. The previous slice (#4.3) implemented the
# gate as `strings <bin> | grep <PLACEHOLDER_SHA>`, which is
# structurally broken: Go's linker keeps the source-code literal
# in rodata adjacent to the runtime value of FirstRunCommitSHA,
# so the substring search flags every correctly baked binary as
# failed. The new gate reads the runtime value of the symbol via
# debug/{macho,elf,pe} and compares it against the expected SHA.
#
# Usage: ./scripts/verify-release.sh <path-to-binary> <expected-SHA>
# Exit codes:
#   0  binary is safe to ship (runtime SHA == expected SHA)
#   1  binary carries the placeholder literal, has the wrong
#      runtime SHA, or cannot be read
#   2  invocation error (missing args, file unreadable,
#      `go run` failed to start)
#
# Dependencies: a working `go` toolchain in $PATH.

set -eu

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <path-to-binary> <expected-SHA>" >&2
	exit 2
fi

binary="$1"
expected_sha="$2"

if [[ ! -f "$binary" ]]; then
	echo "RELEASE BLOCKED: $binary is not a regular file" >&2
	exit 2
fi

# Hand off to the Go subcommand. `go run ./cmd/lofi ...` exits
# non-zero on any verification failure, and the subcommand
# already formats a human-readable error to stderr so we do not
# need to add anything here. Forwarding the exit code is what
# makes this script usable from a CI step.
set +e
go run ./cmd/lofi verify-release-binary "$binary" "$expected_sha"
status=$?
set -e

if [[ "$status" -eq 0 ]]; then
	echo "RELEASE OK: $expected_sha baked in $binary"
	exit 0
fi

echo "RELEASE BLOCKED: $binary failed the SHA gate (exit $status)" >&2
exit "$status"
