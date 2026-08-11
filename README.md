# lo-fi-player

> Terminal-native, license-clean lo-fi music player for developers.

`lo-fi-player` is a single-binary, keyboard-first CLI/TUI that plays
ambient, attribution-clean audio from a curated catalog. It does not
stream from Spotify, YouTube, or SoundCloud; it does not require a
browser, a GUI, or an account. Once the first-run fetch lands, the
player runs offline.

## Status

This repository is in **bootstrap**. PR #0 establishes the
stack-agnostic repository metadata (this file, `CONTRIBUTING.md`,
`.gitignore`, and a CI workflow stub). The Go module, the ports, the
audio adapters, and the catalog all land in later chained PRs.

| Area | Status |
| --- | --- |
| Repository metadata | PR #0 (this commit chain) |
| Go module + `cmd/lofi` skeleton | PR #1 |
| `AudioBackend` port + mpv adapter | PR #1, PR #3 |
| Catalog loader + first-run fetch | PR #5, PR #6 |
| TUI views (Bubble Tea) | PR #8 |
| CLI credits + dispatcher | PR #7, PR #9 |
| Catalog seed (`catalog/v1/`) + `LICENSE` | PR #10 *(gated — MIT vs Apache-2.0 unresolved)* |

Until PR #1 lands, `go test ./...` and `go build ./...` fail with
a clear "no main module" message. That is expected.

## Why this exists

Developers who live in the terminal want ambient music that does not
break keyboard focus or force a browser. GUI players demand context
switches; YouTube/Spotify CLI wrappers are fragile and legally
exposed; shell scripts around `mpv` or `cmus` lack discovery, queue,
and attribution. `lo-fi-player` fills the gap with a terminal-native
player that ships its own catalog, surfaces attribution inline, and
stays inside the keyboard-first workflow.

## Repository layout (planned)

```text
cmd/lofi/          composition root and CLI dispatcher (PR #1)
internal/tui/      Bubble Tea model, views, keymap (PR #8)
internal/audio/    AudioBackend port + mpv + oto adapters (PR #1, #3, #4)
internal/catalog/  loader, checksum, SHA-pinned fetch (PR #5, #6)
internal/config/   XDG-aware atomic TOML store (PR #2)
catalog/v1/        versioned, license-clean track tree (PR #10, gated)
.github/workflows/ CI pipeline (this PR)
```

## Quick start

There is nothing to run yet. Once PR #1 ships:

```bash
go build ./cmd/lofi
./lofi --help
```

The expected first-run flow is `lofi play` (cold-start to first
audible playback under two seconds, offline after the first fetch).

## Contributing

Read [`CONTRIBUTING.md`](./CONTRIBUTING.md) before opening a pull
request. It covers the branch naming convention, the
Conventional Commits format, the commit-by-work-unit rule, and the
PR chain strategy.

## Documentation

Design notes, spec delta, and the chained-PR plan live in the
project's spec-driven change (`sdd/lo-fi-player`). For slice #1 the
authoritative artefacts are:

- `sdd/lo-fi-player/proposal` — problem, users, scope, success metrics
- `sdd/lo-fi-player/spec` — requirements, scenarios, embedded contracts
- `sdd/lo-fi-player/design` — module architecture, threat matrix
- `sdd/lo-fi-player/tasks` — chained PR decomposition

## License

The repository license is **deferred to Phase 6** (PR #10). MIT is
the recommended default pending explicit approval, but no `LICENSE`
file ships with PR #0 so the maintainer can choose between MIT and
Apache-2.0 without a rewrite. Per-track licenses in the bundled
catalog remain independent and are always surfaced in the
attribution view.

## Acknowledgments

- Internet Archive — likely source for the bundled CC0/CC-BY catalog
  (license claims flagged `NEEDS CONFIRMATION` until re-verified
  from an unrestricted network).
- `mpv` — JSON-IPC audio backend.
- `oto/v3` — pure-Go audio backend for the `procedural:rain`
  fallback station.
- Bubble Tea, Lip Gloss, and the Charmbracelet ecosystem — TUI.
