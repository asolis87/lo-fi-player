# Design: Slice #2 — catalog seed audit

## Technical Approach

Five chained PRs on `feat/pr-10-catalog-seed` materialize a real catalog (PR-A), signed verification reports (PR-B), a restaged manifest (PR-C), a schema/loader drift fix (PR-D), and a release pipeline that bakes the real SHA into `pinning.go:30` via `-ldflags -X` (PR-E). The approach is incremental: each PR is independently mergeable, the runtime's `LoadFromDir` is preserved, and `mpv` remains the only audio decoder. The change is contained to `internal/catalog/`, `internal/audio/`, `cmd/lofi/`, `catalog/v1/`, and `.github/workflows/release.yml`.

## Architecture Decisions

### Decision: Per-Track Subdir Contract over Flat Manifest

**Choice**: Each track ships as `<id>/{track.json, LICENSE.txt, audio.mp3}` and the manifest is an index, not the source of audio bytes.
**Alternatives considered**: (a) single `manifest.json` with base64-encoded audio (rejected: 33% size bloat, harder to diff); (b) remote-only audio with on-demand fetch (rejected: violates "first-run fetch a pinned manifest, binary stays small" intent from design #291).
**Rationale**: per-track subdirs match the layout the slice #1 README already promised (`catalog/v1/README.md:18-29`) and let `LoadFromDir` validate `<id>/audio.mp3` against `Track.AudioFilename` directly. They're also git-friendly: audio bytes are diffable per-track.

### Decision: Commit Audio Bytes, Not LFS or Remote-Only

**Choice**: 3–5 MP3s land in the repo as part of the `catalog/v1/<id>/audio.mp3` files.
**Alternatives considered**: (a) Git LFS (rejected: LFS bandwidth on `lofi sync` clones is hostile to first-run UX; LFS isn't free on GitHub); (b) remote-only with `lofi sync` pulling bytes (rejected: requires syncer to download arbitrary bytes and the SHA-pinning contract was designed for a single small manifest, not audio).
**Rationale**: 3–5 × 4–6 MB is ~30 MB total, well under GitHub's soft limit. The trade-off (binary doesn't fetch audio at runtime) is explicit in the proposal's risks.

### Decision: `-ldflags -X` via GitHub Action, No Manual Edits

**Choice**: `.github/workflows/release.yml` reads the manifest-commit SHA at HEAD on `v*` tags, passes it via `-ldflags "-X ...FirstRunCommitSHA=<sha>"` to `go build`, and attaches the binary to the GitHub Release.
**Alternatives considered**: (a) manual `go build` for each release (rejected: error-prone, doesn't scale, easy to ship a placeholder); (b) post-build `sed` of the binary (rejected: brittle, version-skew).
**Rationale**: `-ldflags -X` is the idiomatic Go pattern; the action runs on every tag, so release hygiene is enforced, not aspirational.

### Decision: `LoadFromDir` Stays as the Runtime Default

**Choice**: PR-D adds `AudioFilename` to `Track` and updates `LoadFromDir` to validate `<id>/audio.mp3` exists. Runtime callers (`runList`, `runPlay`, `runCredits`) keep calling `LoadFromDir`.
**Alternatives considered**: switch runtime to `LoadFromFile` (rejected: the manifest doesn't carry audio bytes; `LoadFromDir` is the only path that resolves them).
**Rationale**: minimal change, maximum compatibility with slice #1's runtime contract.

## Data Flow

```
lofi sync                              lofi play <id>
    │                                       │
    ▼                                       ▼
FirstRunCommitSHA                   cmd/lofi/play.go:runHeadlessPlay
    │                                       │
    ▼                                       ▼
ValidateURL (pinning regex)          LoadFromDir(cacheRoot)
    │                                       │
    ▼                                       ▼
Syncer.Fetch (HTTPS GET)             findTrackByID(cat, target)
    │                                       │
    ▼                                       ▼
Syncer.Apply (atomic write)          backend.Load(Track{ID, Path: <cache>/<id>/audio.mp3})
    │                                       │
    ▼                                       ▼
manifest.json on disk                 mpv.Play → audio out
```

`release.yml` flow (PR-E): tag push → checkout → `git rev-parse HEAD:<catalog/v1/manifest.json>` → `go build -ldflags -X ...FirstRunCommitSHA=<sha>` → `gh release upload`.

## File Changes

| File | Action | Description |
|---|---|---|
| `catalog/v1/track-{001..003}/{track.json,LICENSE.txt,audio.mp3}` | Create | Per-track dirs (audio ~4–6 MB each) |
| `catalog/v1/verification/track-{001..003}.md` | Create | Signed reports (PR-B) |
| `catalog/v1/manifest.json` | Modify | Real checksums, `VERIFIED` status (PR-C) |
| `internal/catalog/schema.go` | Modify | Add `Track.AudioFilename` + validation (PR-D) |
| `internal/catalog/loader.go` | Modify | Subdir contract; missing `audio.mp3` aborts; checksum verify at load (PR-D) |
| `internal/license/parser.go` | Create | Read `LICENSE.txt` for credits output (PR-B) |
| `cmd/lofi/play.go` | Modify | Resolve `<id>/audio.mp3`, pass to `backend.Load` (PR-D) |
| `.github/workflows/release.yml` | Create | Tag-triggered SHA bake + release upload (PR-E) |

## Interfaces / Contracts

```go
// internal/catalog/schema.go
type Track struct {
    // ... existing fields
    AudioFilename string `json:"audio_filename"`
}
func (t Track) Validate() error // + non-empty AudioFilename

// internal/catalog/loader.go
func LoadFromDir(root string) (*Catalog, error)
// walk root, expect <id>/{track.json,audio.mp3}, validate, verify checksum
```

## Testing Strategy

| Layer | What | How |
|---|---|---|
| Unit | `AudioFilename` validation | `schema_test.go`: empty/missing field cases |
| Unit | `LoadFromDir` subdir contract | `loader_test.go`: missing `audio.mp3` aborts |
| Unit | Checksum verify at load | `loader_test.go`: mismatched SHA-256 aborts |
| Unit | `runHeadlessPlay` path resolution | `play_test.go`: `Path` is resolved cache file |
| Unit | No-placeholder grep | New CI hook: `<PLACEHOLDER_SHA>` absent from built binary |
| Integration | `lofi sync` end-to-end | `syncer_test.go`: add happy-path case (currently only offline is covered) |

## Threat Matrix

| Boundary | Applicability | Reason |
|---|---|---|
| Git repository selection | N/A | `release.yml` uses `actions/checkout@v4` on the runner's own checkout. |
| PR commands (`gh release upload`) | Applicable | Release with no artifacts fails action; release with placeholder-built binary fails grep test. Propagates to tasks.md as PR-E RED test. |
| Documentation-like paths | N/A | No new executable `.sh` or `.md` runners. |
| Commit state | N/A | Action only reads HEAD on a tag; no commit mutation. |

## Migration / Rollout

No data migration. `catalog/v1/manifest.json` is replaced wholesale in PR-C. Users with a stale slice #1 cache get the new catalog on next `lofi sync`; PR-D's `LoadFromDir` validates from disk. Cache layout unchanged.

## Open Questions

- [ ] `internal/license/parser.go` vs extending `internal/credits/notice.go` to read per-track `LICENSE.txt`?
- [ ] `release.yml` grep test: scan binary or source tree? (Binary is more honest.)
- [ ] PR-D: refuse pre-slice-2 `track.json` without `AudioFilename`, or migration shim?
