# Tasks: Slice #2 — catalog seed audit

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | ~1500 aggregate code + ~30 MB audio; ≤400 per PR |
| 400-line budget risk | Low per slice; aggregate is data-heavy, not code-heavy |
| Chained PRs recommended | Yes |
| Suggested split | PR-A → PR-B → PR-C → PR-D → PR-E (feature-branch-chain) |
| Delivery strategy | ask-on-risk |
| Chain strategy | feature-branch-chain (cached from slice #1, obs #4737) |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test | Rollback |
|---|---|---|---|---|
| 1 | Curate 3–5 CC0/CC-BY tracks with per-track dirs | PR-A | `go test ./internal/catalog/...` | remove dirs; revert seed |
| 2 | Signed verification reports + license parser | PR-B | `go test ./internal/license/...` | remove reports + parser |
| 3 | Restage `manifest.json` with real checksums | PR-C | `go test ./internal/catalog/...` | revert manifest |
| 4 | Schema/loader drift fix + path resolution in `lofi play` | PR-D | `go test ./internal/catalog/... ./cmd/lofi/...` | revert schema/loader/play |
| 5 | `release.yml` SHA bake + placeholder grep test (RED) | PR-E | new CI step asserts grep failure | remove workflow + grep test |

## Phase 1: Foundation (PR-D prelude)

- [ ] 1.1 Add `AudioFilename string \`json:"audio_filename"\`` to `Track` in `internal/catalog/schema.go`
- [ ] 1.2 Extend `Track.Validate()` to require non-empty `AudioFilename` (RED in `schema_test.go`)
- [ ] 1.3 Create empty subdirs `catalog/v1/track-{001,002,003}/` with placeholder `track.json`
- [ ] 1.4 Update `TestManifestSchema_TrackSampleHasNeedConfirmation` to assert subdirs exist

## Phase 2: Core Implementation (PR-A + PR-B + PR-C)

- [ ] 2.1 PR-A: Source 3–5 tracks from Internet Archive / ccMixter / Pixabay; verify licenses
- [ ] 2.2 PR-A: Commit `audio.mp3` + `LICENSE.txt` per track subdir (~30 MB)
- [ ] 2.3 PR-A: Fill per-track `track.json` (id, title, artist, license, source_url, attribution, duration, audio_filename)
- [ ] 2.4 PR-B: Create `internal/license/parser.go` reading `LICENSE.txt` (RED: parser test)
- [ ] 2.5 PR-B: Write `catalog/v1/verification/track-{001..003}.md` (URL, snapshot, re-hashed SHA-256, operator signature)
- [ ] 2.6 PR-C: Hash each `audio.mp3`, write real `checksum_sha256` into `manifest.json`
- [ ] 2.7 PR-C: Promote `license_status` to `VERIFIED` for tracks with valid reports

## Phase 3: Integration (PR-D)

- [ ] 3.1 Update `LoadFromDir` to require `<id>/audio.mp3`; abort on missing (RED: `loader_test.go`)
- [ ] 3.2 Add checksum verification at load (RED: mismatch case in `loader_test.go`)
- [ ] 3.3 Modify `cmd/lofi/play.go:runHeadlessPlay` to resolve `<id>/audio.mp3` and pass `Path` (RED: `play_test.go`)
- [ ] 3.4 Remove `LoadFromFile` test fixture dependency on the slice #1 placeholder manifest

## Phase 4: Testing & Verification (PR-E + threat-matrix RED)

- [ ] 4.1 PR-E: Create `.github/workflows/release.yml` triggered on `v*` tags (RED: fails on missing artifact)
- [ ] 4.2 PR-E: Action runs `go build -ldflags "-X ...FirstRunCommitSHA=<sha>"` (RED: build without `-ldflags` fails grep)
- [ ] 4.3 PR-E: CI step greps built binary for `<PLACEHOLDER_SHA>`; release blocks on hit
- [ ] 4.4 PR-E: `gh release upload` attaches binary (RED: upload with no artifacts fails)
- [ ] 4.5 Integration: `lofi sync` end-to-end against `httptest.Server` returning a pinned manifest

## Phase 5: Cleanup & Docs

- [ ] 5.1 Update `README.md`: replace placeholder-seed caveat with verified-track flow
- [ ] 5.2 Update `catalog/v1/README.md`: per-track subdir contract + verification report format
- [ ] 5.3 Update `CONTRIBUTING.md`: document `release.yml` SHA-bake flow + manual override
