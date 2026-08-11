# release-sha-bake Specification

## Purpose

Defines the contract for injecting the real commit SHA of `catalog/v1/manifest.json` into the `FirstRunCommitSHA` variable at release time. The injection is automated by a GitHub Action that runs on `v*` tags and produces a release binary whose baked SHA matches the manifest commit.

## Requirements

### Requirement: Tagged-Release SHA Injection

A GitHub Action `release.yml` MUST run on every `v*` tag push. The action MUST read the SHA of the `catalog/v1/manifest.json` commit at HEAD, pass it to `go build` via `-ldflags "-X github.com/asolis87/lo-fi-player/internal/catalog.FirstRunCommitSHA=<sha>"`, and produce a release binary whose `FirstRunCommitSHA` matches the manifest commit.

#### Scenario: Tag push produces a correctly baked binary

- GIVEN a tag `v0.2.0` is pushed to the default branch
- AND the current `catalog/v1/manifest.json` is at commit `abc123…` (40 hex chars)
- WHEN the `release.yml` action runs
- THEN a binary is built with `FirstRunCommitSHA == "abc123…"`
- AND the binary is attached to the GitHub Release for `v0.2.0`

#### Scenario: Manual override is documented

- GIVEN a maintainer needs to ship a binary without a tag
- WHEN they consult `CONTRIBUTING.md`
- THEN the manual `go build -ldflags …` command is documented
- AND the example uses a real 40-hex SHA, not a placeholder

### Requirement: No Placeholder SHA in Released Binaries

The CI MUST run a grep test on every release binary that fails the release if the literal string `<PLACEHOLDER_SHA>` is present. This is a hard gate; placeholder SHAs MUST NOT reach a release artifact.

#### Scenario: Placeholder SHA fails the release

- GIVEN a release binary built without `-ldflags` (e.g. a manual local build was uploaded by mistake)
- WHEN the grep test runs on the binary
- THEN the test fails
- AND the release is blocked
- AND the action reports the offending artifact path

### Requirement: `FirstRunCommitSHA` Is a `var`, Not a `const`

`FirstRunCommitSHA` MUST remain a `var` in `internal/catalog/pinning.go` to allow `-ldflags -X` to override it at build time. Production code MUST NOT reassign it; the release pipeline is the only sanctioned writer.

#### Scenario: Test override is allowed

- GIVEN a test sets `catalog.FirstRunCommitSHA = "<valid 40-hex SHA>"`
- WHEN `ValidateURL` runs against a URL built from that SHA
- THEN validation succeeds
- AND the test does not modify any other global state
