# Contributing to lo-fi-player

Thanks for your interest in `lo-fi-player`. This guide is the
contract between every contributor and every reviewer. It is short
on purpose: every rule here exists because the change chain forced
it.

## Ground rules

1. **One work unit per commit.** Each commit is a deliverable
   behaviour, fix, migration, or docs unit — not a file-type bucket.
   Tests and docs for that unit live in the same commit.
2. **Stay inside the 400-line review budget.** Authored additions
   plus deletions for any single PR must stay near or below 400
   lines. If the slice is bigger, split it into a chained PR before
   opening the request.
3. **Follow the chain strategy.** `lo-fi-player` uses a
   **feature-branch chain**: a tracker branch (`feat/lo-fi-player`)
   aggregates the slice, and every child PR targets the immediate
   previous PR branch. Child PRs never target `main` directly.
4. **Defer detail to the spec.** When in doubt, read
   `sdd/lo-fi-player/spec` and `sdd/lo-fi-player/design`. The spec
   is the acceptance criteria; the design is the architectural
   boundary. A PR that contradicts either must justify the change
   in the description.
5. **No AI co-author trailers.** Commit messages must follow the
   Conventional Commits format below. Do not include `Co-Authored-By:
   ...` or any AI-assist attribution.

## Branch naming

Branch names encode the PR slice and the area of work. The format
is:

```text
<type>/pr-<number>-<short-kebab-slug>
```

| Segment | Rule | Example |
| --- | --- | --- |
| `<type>` | One of `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`, `build`, `perf`. | `feat`, `chore` |
| `pr-<number>` | The zero-padded PR number from the chained-PR plan in `sdd/lo-fi-player/tasks`. PR #0 is the bootstrap. | `pr-0`, `pr-1` |
| `<short-kebab-slug>` | Lower-kebab summary of the slice (≤ 4 words). | `pr-0-bootstrap`, `mpv-ipc-adapter` |

Worked examples from slice #1:

- `feat/pr-0-bootstrap` — repository bootstrap (this PR).
- `feat/pr-1-go-skeleton` — Go module + `cmd/lofi` skeleton + `AudioBackend` port.
- `feat/pr-5-catalog-loader` — catalog loader + checksum verification.
- `fix/pr-7-credits-prefix` — a focused fix inside PR #7's slice.

The feature tracker's name is reserved: `feat/lo-fi-player` is the
aggregation branch for the entire slice and is never used for a
child PR.

## Commit messages — Conventional Commits

Every commit message **must** follow the
[Conventional Commits 1.0.0](https://www.conventionalcommits.org/)
specification. The header line is the contract; the body explains
the *why* and the reviewer hand-off.

### Format

```text
<type>(<optional-scope>): <imperative summary>

<body — what & why, not what files changed>

<footer — references, breaking changes, reviewers>
```

### Allowed `<type>` values

| Type | When to use |
| --- | --- |
| `feat` | A new user-visible behaviour, command, view, or API. |
| `fix` | A bug fix that changes user-visible behaviour. |
| `refactor` | Internal restructuring with no user-visible change. |
| `perf` | A change that improves a measurable performance metric. |
| `test` | Adding or fixing tests with no production change. |
| `docs` | Documentation-only change (README, CONTRIBUTING, design notes). |
| `chore` | Tooling, repository metadata, build glue. No production code. |
| `ci` | CI workflow files and CI-only changes. |
| `build` | Build system, dependency pinning, version bumps. |
| `revert` | A revert of a previous commit; body must reference the SHA. |

### Optional `<scope>`

The scope is a short noun that names the area of the codebase.
Use lowercase, single-token scopes that match the planned module
names. Examples: `audio`, `catalog`, `config`, `tui`, `cli`,
`credits`, `sync`, `bootstrap`.

### The body

The body answers three reviewer questions:

1. **What** did this commit deliver in one sentence?
2. **Why** was it needed (link to the spec scenario, design
   decision, or risk it closes)?
3. **What is the rollback boundary** (which files / behaviour can
   be reverted without removing unrelated work)?

### The footer

Use the footer for:

- `Refs:` references to spec IDs (e.g. `Refs: REQ-CLI-1, S-CLI-1`).
- `Closes:` references to GitHub issues.
- `BREAKING CHANGE:` paragraphs, when the commit forces a migration.

Do **not** add `Co-Authored-By:` trailers, including any generated
by AI tools.

### Worked examples

```text
feat(audio): add AudioBackend port and mock for the lo-fi-player

Introduce the audio.Backend interface that decouples the TUI and
the CLI dispatcher from concrete adapters. The interface mirrors
the surface in spec #287 (Load/Play/Pause/Stop/SetVolume/Seek/State/
Close) and ships with a deterministic mock so downstream slices
write tests against the port, not against mpv.

Rollback: deleting internal/audio/port.go and internal/audio/mock.go
leaves the upstream modules untouched.

Refs: REQ-AUD-1
```

```text
fix(catalog): reject floating refs in the first-run fetch URL

The SHA-pinned fetch contract from spec #289 forbids mutable refs.
A regression had re-enabled the `latest` fallback for one path;
this commit re-tightens the loader and adds a regression test.

Refs: REQ-FCH-1
```

## Pull requests

1. **One PR per work unit.** Use the chained-PR plan in
   `sdd/lo-fi-player/tasks` to find your PR number and slice.
2. **Branch from the previous PR's branch**, not from `main`.
   Feature-branch chain means each PR is a delta on top of the
   previous one.
3. **PR title is the Conventional Commits header.** The PR title
   doubles as the squash-merge commit subject; keep it under 72
   characters.
4. **PR description cites the spec.** Link the requirements and
   scenarios the PR closes. If the PR deviates from the design,
   justify the deviation in the description *before* requesting
   review.
5. **CI must be green.** The pipeline stubs in PR #0 deliberately
   fail until the Go module lands; from PR #1 onward, `go vet`,
   `go build`, and `go test` must all pass.
6. **Update the spec-driven artefacts if the contract changes.**
   Any user-visible contract change must travel with the spec
   delta, not behind it.

## Local checks

Before pushing a branch, run the same gates the CI runs:

```bash
go vet ./...
go build ./...
go test ./...
```

PR #0 is the only PR where these commands fail with a "no main
module" message; that is expected and documented in `README.md`.

## Code of conduct

Be kind, be precise, and assume good faith. Reviewers should
explain the *why* behind every requested change; contributors
should answer the *why* behind every implementation. Disagreement
is welcome — silence is not.
