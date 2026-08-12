# Proposal: Slice #2 — catalog seed audit (real audio, signed verification, baked SHA)

## Intent

Shipped binary is a procedural-rain TUI shell. Catalog is empty (`checksum_sha256: "0000…0000"`, `NEEDS CONFIRMATION`, no MP3). `internal/catalog/pinning.go:30` holds `<PLACEHOLDER_SHA>` so `lofi sync` is rejected pre-network. Even if sync worked, `lofi list` would find zero tracks (loader drift: runtime uses `LoadFromDir`, seed ships a single `manifest.json`). This slice closes that gap: real audio, real licenses, real SHA, real `lofi play <id>` decoding MP3.

## Scope

### In Scope
- 3–5 CC0/CC-BY tracks (Internet Archive / ccMixter / Pixabay) committed as `<id>/track.json` + `<id>/LICENSE.txt` + `<id>/audio.mp3`.
- Signed verification reports in `catalog/v1/verification/<id>.md` (URL + HTML snapshot + re-hashed SHA-256 + operator signature).
- `catalog/v1/manifest.json` restaged with real checksums.
- `Track.AudioFilename` field added (currently silently dropped). Seed restructured to `<id>/track.json` subdirs; `LoadFromDir` stays as runtime default.
- GitHub Action `release.yml` bakes the real commit SHA into `pinning.go:30` via `-ldflags "-X ...FirstRunCommitSHA=<sha>"` on tagged releases.

### Out of Scope
- Persistent queue / `REQ-CFG-2` round-trip (parallel slice #2 workstream, separate change).
- Go 1.22→1.24 bump, ALSA stderr noise, tracker FF (operational bundle, separate change).
- User-import, M3U/PLS, MPRIS, crossfade, scrobbling — parked or rejected per slice #1 non-goals.
- `hajimehoshi/go-mp3` decoder — slice #1 explicitly rescinded; mpv remains the decoder.

## Capabilities

### New Capabilities
- `catalog-seed`: per-track `<id>/` directory contract, `track.json` schema, audio bytes, verification report.
- `release-sha-bake`: tagged release → SHA injection via `-ldflags`; no manual edits.

### Modified Capabilities
- `catalog-loader`: `Track.AudioFilename` required; subdir contract load-bearing; checksum verified at load.
- `cli-sync`: consume baked SHA on init; clear error if binary is stale.
- `audio-backend`: `lofi play <id>` resolves `Path` to cached audio bytes (currently passes `Path: ""`).

## Approach

Five chained PRs on `feat/pr-10-catalog-seed`:
- **PR-A** curate 3–5 tracks, commit per-track dirs (~300–600 LOC + 10–100 MB audio).
- **PR-B** signed verification reports + `internal/license/` tooling (~200 LOC).
- **PR-C** restage `manifest.json` with real checksums (~100 LOC).
- **PR-D** schema/loader drift fix — `AudioFilename`, subdir contract, `LoadFromDir` validated (~300 LOC).
- **PR-E** `release.yml` action baking SHA on `v*` tags (~200 LOC).

## Affected Areas

New: `catalog/v1/<id>/*`, `catalog/v1/verification/`, `.github/workflows/release.yml`. Modified: `catalog/v1/manifest.json`, `internal/catalog/{schema,loader}.go`, `internal/audio/port.go`, `cmd/lofi/play.go`.

## Risks

- **Mis-attribution (High)** — breaks license-clean promise. Mit: user re-verifies from non-sandbox host; reports carry URL + re-hashed checksum.
- **Catalog bloat (High)** — committed audio. Mit: 3–5 tracks this slice; LFS or remote-only later.
- **`LoadFromDir` regression (Med)** — update `loader_test.go` for subdir contract; add per-track contract test.
- **SHA bake fragility (Med)** — PR-E small + reviewable; manual override documented in `CONTRIBUTING.md`.

## Rollback Plan

Per-PR revert. PR-A rollback → placeholder seed restored, sync resumes placeholder rejection. PR-D rollback → old `LoadFromDir` with pre-fix seed (zero tracks, as before). No slice #1 capability mutated.

## Dependencies

User has non-sandbox host for license verification. Go 1.22 + `oto/v3 v3.3.3` (no bumps). `mpv` external runtime, unchanged.

## Success Criteria

- [ ] `lofi sync` succeeds end-to-end against a real pinned manifest; exit 0.
- [ ] `lofi list` shows ≥ 3 verified tracks with `license_status: "VERIFIED"`.
- [ ] `lofi play <id>` decodes MP3 via the mpv adapter.
- [ ] `loader_test.go` + `schema_test.go` cover new `AudioFilename` + subdir contract.
- [ ] No `PLACEHOLDER_SHA` literal in any released binary (CI grep test).
