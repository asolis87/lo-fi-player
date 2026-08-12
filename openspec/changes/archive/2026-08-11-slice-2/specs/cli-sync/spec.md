# cli-sync Specification (Delta vs Slice #1)

## Purpose

Defines the behavior of the `lofi sync` subcommand after the SHA-bake fix: the binary MUST validate its own baked SHA at startup, reject placeholder SHAs with a clear error, and surface a distinct error if the binary is stale relative to the latest catalog commit.

## Requirements

### Requirement: Placeholder SHA Is Rejected at First Network Call

`lofi sync` MUST validate the baked `FirstRunCommitSHA` against the SHA-pinning regex `^[0-9a-f]{40}$` before issuing any network request. A SHA that fails the regex MUST cause `lofi sync` to exit with code 1 and the message `lofi sync: manifest URL rejected: catalog: floating ref or malformed SHA`.

(Previously: this guard existed but only the regex match was enforced; the placeholder rejection worked correctly in slice #1, but the message wording was a source of confusion and was clarified in PR #13.)

#### Scenario: Placeholder SHA exits 1 with a clear message

- GIVEN the binary's `FirstRunCommitSHA` is `<PLACEHOLDER_SHA>` (a release was built without `-ldflags`)
- WHEN `lofi sync` runs
- THEN it exits with code 1
- AND stderr contains the `floating ref or malformed SHA` message
- AND no network call is made

#### Scenario: Valid SHA proceeds to fetch

- GIVEN the binary's `FirstRunCommitSHA` is a valid 40-hex SHA
- WHEN `lofi sync` runs
- THEN `ValidateURL` passes
- AND `Syncer.Fetch` is called
- AND the network request is issued against `raw.githubusercontent.com`

### Requirement: Stale Binary Warning

If `lofi sync` succeeds against the pinned manifest but the manifest's `version` field differs from the version the binary was built against, `lofi sync` SHOULD print a notice on stderr suggesting the user upgrade. Slice #2 does not enforce version comparison automatically; this is a soft warning only.

(Previously: no version comparison existed.)

#### Scenario: Stale binary prints a notice

- GIVEN a binary built against manifest version `v1.0`
- AND the pinned manifest is now at version `v1.1`
- WHEN `lofi sync` runs and succeeds
- THEN stderr contains `lofi sync: note: binary built against v1.0, manifest is at v1.1; consider upgrading`
- AND the cache is updated normally
- AND the exit code is 0

### Requirement: Network Errors Do Not Corrupt the Cache

`lofi sync` MUST treat any network error (transport failure, non-2xx response, oversized body) as `ErrOffline` and MUST NOT call `Syncer.Apply`. The local cache MUST remain byte-identical to its pre-sync state on any failure.

(Previously: this was already enforced; the slice #2 spec re-states it for clarity and as a regression test target.)

#### Scenario: Network failure leaves the cache intact

- GIVEN the local cache has a `manifest.json` from a prior successful sync
- AND the network is offline
- WHEN `lofi sync` runs
- THEN `Syncer.Fetch` returns `ErrOffline`
- AND `Syncer.Apply` is not called
- AND the local `manifest.json` is byte-identical to its pre-sync content
