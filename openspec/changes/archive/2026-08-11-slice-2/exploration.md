# sdd/slice-2/explore — slice #2 candidate exploration

> Transcribed from Engram observation #4782 (`sdd/slice-2/explore`, created 2026-08-11 16:40:05) for use by the `gentle-ai` SDD dispatcher in `hybrid` artifact store mode. The Engram observation remains the authoritative source; this file is the filesystem mirror.

## Method

Investigated `lo-fi-player` at `/Users/alvasolis/DEV/poc/lo-fi-player` post-merge of slice #1 (15 chained PRs including post-slice-1 follow-ups #12–#15). Read in parallel:

- Memory #283 (slice #1 exploration), #286 (slice #1 proposal, including §18 parking lot), #287 (spec with REQ-*/S-* contracts), #291 (design), #293 (slice #1 chained-PR plan with budget overruns), #292 (deferred audio/license decisions), #289 (commit-SHA URL pinning contract), #290 (credits CLI contract), #333 (silent-procedural bug discovery), #339–#341 (PR #14/#15 merges and the "good stopping point" call), #321 (teatest risk), #314 (PR #4 budget overrun), #303–#310 (apply-progress per PR), #327/328/331 (README updates and overclaim correction).
- Code: `internal/audio/{port.go,mpv.go,procedural.go,procedural_backend.go,device.go,select.go}`, `internal/catalog/{loader.go,manifest.go,schema.go,pinning.go,syncer.go}`, `internal/config/store.go`, `internal/tui/{model.go,views.go,keys.go}`, `cmd/lofi/{main.go,dispatch.go,play.go,list.go,sync.go,credits.go,cache.go}`, `catalog/v1/{manifest.json,README.md}`, `.github/workflows/ci.yml`, `README.md`, `CONTRIBUTING.md`, `go.mod`.

Did NOT re-derive the slice #1 chain or proposal (memory is authoritative). Did NOT examine external libraries beyond what `go.mod` already pins.

---

## Current state (relevant to slice #2 decisions)

1. **The shipped binary cannot play real music.** `catalog/v1/manifest.json` is a placeholder with `checksum_sha256: "0000…0000"`, `license_status: "NEEDS CONFIRMATION"`, and `_placeholder_note` documenting the gap. `internal/catalog/pinning.go:30` still holds `FirstRunCommitSHA = "<PLACEHOLDER_SHA>"`, so the URL-validity regex rejects `lofi sync` before any network call. No MP3 bytes are committed.
2. **Schema/loader drift in the catalog.** The seed ships a single `manifest.json`. Runtime callers (`runList`, `runPlay`, `runCredits`) all use `LoadFromDir`, which walks subdirs looking for `track.json`. `LoadFromFile` (the path that actually reads `manifest.json`) is only exercised by the manifest test, not by any CLI command. As a result, even if `lofi sync` succeeded, `lofi list` would currently find zero tracks because the cache dir has no subdirs with `track.json`. The Go `Track` struct (`internal/catalog/schema.go:53-64`) does not include `audio_filename`, but the JSON manifest has it (silently dropped on parse).
3. **REQ-CFG-2 (queue round-trip) is unimplemented.** `config.Config{LastQueue, LastTrackIndex, Volume}` exists (`internal/config/store.go:28-32`) and `runHeadlessPlay` calls `config.LoadOrDefault()` (no-op read), but nothing ever writes. The TUI does not load or save.
4. **The procedural fallback is wired end-to-end** (PR #4 + PR #14). `lofi play procedural:rain` actually produces audio through `oto/v3`. On hosts without an audio device, `defaultDeviceFactory` falls back to a `noopDevice` so the binary still runs (PR #14 follow-up already addresses that path).
5. **mpv adapter ships** but the runtime path for catalog tracks short-circuits on `procedural:` and never reaches `audio.Select` (no `mpv` install required to play procedural).
6. **Tracker divergence.** `feat/lo-fi-player` is at `767db0d` (pre-PR-#6); `feat/pr-10-catalog-seed` is at `f99bf7a` (PR #10 + #12–#14 merged). The tracker needs a fast-forward before any release ships.
7. **Slice #1 non-goals** are reaffirmed in proposal #286 §8 and spec #287 footer. Anything that conflicts with those is rejected without ceremony: streaming from YT/Spotify/SoundCloud/Apple Music, algorithmic recommendation, GUI, accounts/cloud sync/telemetry, mobile/desktop/browser clients, plugin systems, headless server, lyrics/EQ/visualizations, system media keys in slice #1.

---

## Candidates evaluated

### 1. Catalog seed audit — real audio bytes, license confirmation, verification reports, SHA baking

- **Value (user)**: Closes the biggest gap between the shipped product and the product the README describes. Today the binary is a procedural-rain TUI shell; the catalog is empty. After this, `lofi list` shows real tracks, `lofi sync` works end-to-end, `lofi play <track-id>` actually decodes MP3, the attribution modal renders real data. Both personas win: Focused Developer gets ambient music that isn't just rain; Open-Source Tinkerer gets an auditable bundle.
- **Scope**: 3–5 chained PRs, ~1500–3000 aggregate lines (much of it is data, not code).
  - PR-A: pick 3–5 CC0/CC-BY sources (Internet Archive / ccMixter / Pixabay), commit per-track directories (`<id>/track.json`, `<id>/LICENSE.txt`, `<id>/audio.mp3`) in `catalog/v1/`. ~300–600 lines plus 10–100 MB of audio.
  - PR-B: write signed verification reports under `catalog/v1/verification/<id>.md` (license URL, HTML snapshot path, re-hashed SHA-256, operator signature). Per spec #287, only these reports promote `license_status` to `VERIFIED`.
  - PR-C: restage `catalog/v1/manifest.json` with real checksums and either keep all as `NEEDS CONFIRMATION` until PR-D or promote with PR-D inline.
  - PR-D: fix the schema/loader drift (see Current State #2): either make `LoadFromDir` the runtime default and add the per-track subdirs, OR switch the runtime to `LoadFromFile` (which already handles `manifest.json`). Bake a real commit SHA into `internal/catalog/pinning.go:30` (replacing `<PLACEHOLDER_SHA>`).
  - PR-E (optional): release tooling — a `Makefile` target or GitHub Action that bakes the SHA via `-ldflags "-X ...FirstRunCommitSHA=<sha>"` so future releases can swap without touching `pinning.go`.
- **Dependencies**:
  - Network access to verify licenses from an unrestricted environment (sandbox blocked IA/FMA/ccMixter per risk #4 in exploration #283; user must do this from a non-blocked host).
  - Decision on release pipeline (where the SHA gets baked; manual edit vs. `-ldflags` vs. CI step).
  - Decision on signed verification report format (plain-text operator note? Markdown? GPG-signed `.asc`?). Slice #1 spec just says "signed report" without a format spec.
  - MP3 decoder: the mpv adapter handles MP3 via mpv itself; if a future slice ever wants to play without mpv, `hajimehoshi/go-mp3` is needed (slice #1 explicitly rescinded it).
- **Risks**:
  - **Legal exposure** is the dominant risk. A mis-attributed track is the kind of bug that breaks the product's license-clean promise permanently. Mitigation: verification reports must come from a non-sandbox operator and reference the source-of-truth URL plus a re-hashed checksum, not a copy-paste claim.
  - **Catalog bloat.** 20–50 × 4–6 MB tracks is ~80–300 MB in repo. Per proposal #286 §12 and design #291 the original choice was "first-run fetch from pinned URL, binary stays small." Committing audio bytes reverses that decision; it is a deliberate trade for the verification report contract.
  - **License-fence dilution.** Once `lofi add <user-track>` lands (parking lot), the loader has to enforce that user-imported tracks also carry a valid license claim. The seed audit forces that fence into the loader anyway; the user-import work benefits as a side-effect.
  - **Schema/loader drift** (see Current State #2) must be fixed as part of this slice; leaving it would mean even after the seed ships, `lofi list` still finds nothing.
  - **SHA bake at release time** is operationally fragile. Without a release pipeline, every release is a hand-edit of `pinning.go`. PR-E in scope but easy to under-budget.
- **Fit with non-goals**: ✅ No conflict. License-clean, auditable, no streaming, no scraping (assuming IA/ccMixter sources are properly attributed and `lofi sync` continues to pin to a SHA, not a floating tag).
- **Sequencing**: This is the foundation. Until the catalog has real entries, every other slice #2 candidate that touches catalog tracks (`lofi play <id>`, `lofi list`, `lofi credits` for real tracks, M3U import, user-imported libraries) is theatre. User-imported libraries and M3U/PLS import explicitly depend on having a baseline catalog to coexist with.

### 2. Persistent queue + config round-trip (REQ-CFG-2 closer)

- **Value (user)**: `lofi play` resumes your last queue and volume instead of resetting every restart. Small UX delta, but it is the only outstanding spec scenario from slice #1 that did not get implemented. Focused Developer benefits most: open a new terminal pane, `lofi play`, get yesterday's mix back.
- **Scope**: 1–2 chained PRs, ~400–700 lines.
- **Sequencing**: Standalone — does not depend on the catalog seed audit and does not block it.

### 3. Operational follow-up bundle (Go 1.22→1.24 bump + ALSA stderr noise suppression + tracker fast-forward + SDD artifacts in repo)

- **Value (user)**: Mixed — partly operational (CI quality, build hygiene), partly adoption polish (SDD-in-repo is a force-multiplier for outside contributors).
- **Scope**: 1–2 chained PRs, ~200–600 lines total.

### 4. User-imported libraries (`lofi add <track-dir>` / `lofi remove <track-id>`)
- Parking lot per proposal #286 §18. Re-elevate only with explicit user trigger and after the seed audit lands a real baseline catalog.

### 5. M3U/PLS import-export
- Parking lot. Depends on user-imported libraries. License tag loss in standard formats.

### 6. MPRIS / OS media key integration
- Parking lot. Cross-platform pain. Re-elevate when Focused Developer persona explicitly asks.

### 7. Crossfade / gapless tuning
- Parking lot. Quality bar high. Meaningless with placeholder tracks.

### 8. Scrobbling (last.fm)
- ❌ **REJECT, not defer.** Conflicts with slice #1 non-goal §8 (telemetry + accounts). The only reconcilable variant (local-only listening log without network egress) is a different feature.

### 9. Streaming, algorithmic recommendation, GUI
- ❌ Direct non-goal violations. Not candidates.

---

## Ranking

1. **Catalog seed audit (Candidate 1)** — biggest product unlock.
2. **Persistent queue + config round-trip (Candidate 2)** — cleanest spec closer.
3. **Operational follow-up bundle (Candidate 3)** — four follow-ups from the orchestrator's preflight.
4. (Honorable mention, **deferred not rejected**) **User-imported libraries (Candidate 4)** — re-elevate only with explicit user trigger.

---

## Recommendation

Take **catalog seed audit (Candidate 1)** as the slice #2 anchor, with **persistent queue + config round-trip (Candidate 2)** as the parallel technical workstream and **operational follow-up bundle (Candidate 3)** as the slice #2 PR-A quick win while the seed audit's data work happens offline.

Rationale: the product is functionally a procedural-rain TUI shell today. Every other candidate either presumes a real catalog exists or is operational polish. Closing the product gap is the highest-leverage work the team can do, and it unblocks every future "real catalog" feature at zero extra cost.

**Two assumptions must be confirmed with the user before writing the slice #2 proposal**:

1. **License-verification host availability.** The sandbox where slice #1 was built blocks IA / FMA / ccMixter / Pixabay. The seed audit requires the user (or an operator they trust) to re-verify every track's license from an unrestricted network and sign a verification report.
2. **Release pipeline ownership.** Someone has to bake the commit SHA into `pinning.go` on every release.

**Both assumptions have since been resolved** (see `state.yaml` in this change folder):
- (1) User has a non-sandbox host; signs verification reports there.
- (2) GitHub Action automated, replaces `<PLACEHOLDER_SHA>` via `-ldflags -X`.
- (3) Schema/loader drift fix: reestructurar seed a `<id>/track.json` subdirs, mantener `LoadFromDir` como runtime default, agregar `AudioFilename` al struct.

---

## Explicitly deferred (not rejected)

See proposal #286 §18 parking lot. Includes MPRIS, M3U/PLS, user-imported libraries, crossfade, Windows support.

## Explicitly rejected (non-goal violations)

Scrobbling (last.fm), streaming from YouTube/Spotify/SoundCloud/Apple Music, algorithmic recommendation, GUI/Electron/Tauri/web frontend, user accounts/cloud sync/telemetry, mobile/desktop/browser clients, plugin systems/scripting APIs, headless server/multi-user daemon, visualizer/EQ/replay-gain/lyrics in slice #1.
