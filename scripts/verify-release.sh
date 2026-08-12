#!/usr/bin/env bash
# verify-release.sh — PR-E release-pipeline gate (task 4.3).
#
# Runs `strings <binary> | grep` for the catalog seed placeholder
# and exits non-zero if any match survives. The release workflow
# (release.yml) calls this step right after the build so a binary
# uploaded without `-ldflags -X` is caught before it reaches a
# GitHub Release.
#
# Implementation note: pipefail is intentionally NOT set. macOS
# `strings` keeps writing after `grep -q` exits on a match, which
# raises SIGPIPE (exit 141) inside `strings`. With pipefail that
# 141 would mask the actual match result; without pipefail the
# `if` only sees grep's exit code (0 on hit, 1 on miss). The
# `set -eu` pair is kept so unset variables and individual
# command failures still abort the script before the gate runs.
#
# Usage: ./scripts/verify-release.sh <path-to-binary>
# Exit codes:
#   0  binary is safe to ship (no placeholder hit)
#   1  binary contains <PLACEHOLDER_SHA>; release blocked
#   2  invocation error (missing arg, file unreadable)

set -eu

if [[ $# -ne 1 ]]; then
	echo "usage: $0 <path-to-binary>" >&2
	exit 2
fi

binary="$1"

if [[ ! -f "$binary" ]]; then
	echo "RELEASE BLOCKED: $binary is not a regular file" >&2
	exit 2
fi

placeholder="<PLACEHOLDER_SHA>"

# grep without -q: -q exits early on the first match, which can
# leave `strings` writing into a closed pipe (SIGPIPE) and
# confuse shell pipelines. Using a plain grep avoids that race.
if strings "$binary" | grep -F "$placeholder" > /dev/null; then
	echo "RELEASE BLOCKED: $placeholder found in binary ($binary)" >&2
	exit 1
fi

echo "RELEASE OK: no placeholder SHA in $binary"