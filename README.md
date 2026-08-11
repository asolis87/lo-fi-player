# lo-fi-player

> Terminal-native, license-clean lo-fi music player for developers.

`lo-fi-player` is a single-binary, keyboard-first CLI/TUI that plays
ambient, attribution-clean audio from a curated catalog. It does not
stream from Spotify, YouTube, or SoundCloud; it does not require a
browser, a GUI, or an account. Once the first-run fetch lands, the
player runs offline.

## Status

The chained PR plan that built slice #1 is **shipped**: PR #0 through
PR #11 are merged into `feat/lo-fi-player`, CI is green, and the
binary builds and tests clean. The repository is no longer in
bootstrap.

| Area | Status |
| --- | --- |
| Repository metadata + CI | shipped (PR #0) |
| Go module + `cmd/lofi` skeleton + `AudioBackend` port | shipped (PR #1) |
| XDG-aware atomic TOML config | shipped (PR #2) |
| mpv JSON-IPC adapter (primary backend) | shipped (PR #3) |
| Pure-Go procedural fallback (`procedural:rain`) | shipped (PR #4) |
| Catalog loader + streaming SHA-256 verify | shipped (PR #5) |
| First-run fetch + transactional sync | shipped (PR #6) |
| `lofi credits` CLI (NOTICE + `--json`) | shipped (PR #7) |
| TUI views (Bubble Tea) wired to `lofi play` | shipped (PR #8) |
| CLI dispatch + headless `lofi play <id-or-station>` | shipped (PR #9) |
| `catalog/v1/` seed + `LICENSE` (MIT) | shipped (PR #10) |

The bundled `catalog/v1/` is a **placeholder seed** behind two
distinct gates:

- `lofi sync` refuses the current build because the first-run fetch
  SHA baked into the binary is the literal placeholder
  `<PLACEHOLDER_SHA>` (see `internal/catalog/pinning.go`); the
  SHA-pinning regex rejects it before any network call. A real
  commit SHA must be baked in at release time before sync can run.
- Even once sync succeeds, every track ships with
  `license_status: "NEEDS CONFIRMATION"` per spec #287, and
  promotion to `VERIFIED` requires a signed report appended to
  `catalog/v1/verification/`.

## Why this exists

Developers who live in the terminal want ambient music that does not
break keyboard focus or force a browser. GUI players demand context
switches; YouTube/Spotify CLI wrappers are fragile and legally
exposed; shell scripts around `mpv` or `cmus` lack discovery, queue,
and attribution. `lo-fi-player` fills the gap with a terminal-native
player that ships its own catalog, surfaces attribution inline, and
stays inside the keyboard-first workflow.

## Subcommands

```
lofi play [track|procedural:station]
lofi list
lofi credits [--json]
lofi sync
```

- `lofi play` — opens the TUI (track browser, now-playing, queue, and
  attribution views; press `?` inside for the keymap).
- `lofi play <track-id>` — plays a single track headlessly and exits
  when the track ends.
- `lofi play procedural:rain` — plays the pure-Go procedural
  ambient-rain station headlessly. This is the offline fallback: it
  needs no `mpv`, no audio file, and no network.
- `lofi list` — prints the local catalog as a table (or `--json` for
  the raw manifest).
- `lofi credits` — prints the per-track attribution NOTICE block, or
  `--json` for the same data structured.
- `lofi sync` — fetches the SHA-pinned manifest from the
  catalog source and applies it atomically into the local store.

Unknown subcommands and `lofi --help` print the usage block above and
exit with code 2.

## Quick start

```bash
go build -o lofi ./cmd/lofi
./lofi sync      # one-time: fetch the SHA-pinned catalog
./lofi play      # open the TUI
./lofi list      # or browse the catalog from the shell
./lofi credits   # or inspect attribution from the shell
```

If `mpv` is not installed, `lofi play procedural:rain` still works —
`audio.Select()` falls through to the pure-Go procedural backend
instead of failing.

## Repository layout

```text
cmd/lofi/           composition root, CLI dispatcher, subcommand wiring
internal/tui/       Bubble Tea model, four views, keymap
internal/audio/     AudioBackend port + mpv adapter + procedural fallback
internal/catalog/   Track schema, LoadFromDir, streaming SHA-256 verify, Syncer
internal/config/    XDG-aware atomic TOML store with corruption recovery
internal/credits/   NOTICE block renderer (text + --json)
internal/license/   per-track license parsing and status classification
internal/skeleton/  composition helpers used by the dispatcher
catalog/v1/         versioned, license-clean track tree (placeholder seed)
.github/workflows/  CI pipeline (go vet, go build, go test)
LICENSE             MIT
```

## Contributing

Read [`CONTRIBUTING.md`](./CONTRIBUTING.md) before opening a pull
request. It covers the branch naming convention, the
Conventional Commits format, the commit-by-work-unit rule, and the
PR chain strategy.

## License

The repository is licensed under **MIT**. See [`LICENSE`](./LICENSE).
Per-track licenses in the bundled catalog are surfaced through
`lofi credits` and the TUI attribution view and remain independent
of the repository license.

## Acknowledgments

- Internet Archive — likely source for the bundled CC0/CC-BY catalog
  (license claims flagged `NEEDS CONFIRMATION` until re-verified
  from an unrestricted network).
- `mpv` — JSON-IPC audio backend.
- Bubble Tea, Lip Gloss, and the Charmbracelet ecosystem — TUI.
