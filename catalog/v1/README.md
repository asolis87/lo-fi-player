# catalog/v1/ — bundled catalog index

This directory holds the versioned catalog that ships with each
`lo-fi-player` release. The contents are intentionally minimal
in slice #1: a single `manifest.json` index plus a placeholder
`README.md` and `.gitkeep` so the directory survives in an
empty tree before any audio bytes land.

## Layout

```
catalog/v1/
├── manifest.json   # the index (this file is the contract)
├── README.md       # this file
└── .gitkeep        # keeps the directory under version control
```

When a track is added later, the layout is:

```
catalog/v1/
├── manifest.json
├── <track-id>/
│   ├── track.json    # per-track metadata (spec #287 / schema #287)
│   ├── audio.mp3     # the actual audio bytes (192 kbps MP3 per decision #326)
│   └── LICENSE.txt   # the source license text
├── README.md
└── .gitkeep
```

## Why the manifest is an index, not the data

`manifest.json` is the single file the first-run fetch downloads
from the SHA-pinned URL (decision #289). It is small, human-
auditable, and validated by `internal/catalog.LoadFromFile` so a
tampered manifest fails loud before any audio plays. The per-
track directory layout exists for the events the manifest
predicts (checksum verification, attribution rendering, `lofi
credits` NOTICE output) but is intentionally NOT joined back
into the manifest so the index stays byte-stable.

## Status of the seeded tracks

Every track in this seed carries `license_status: "NEEDS
CONFIRMATION"` per decision #326. The embedded checksum is the
SHA-256 of the empty input (64 hex zeros) so the loader cannot
silently accept a placeholder as a real hash. Promotion to
`VERIFIED` requires a signed verification report appended to
`catalog/v1/verification/` once the source licenses are
re-confirmed from a non-blocked network.

The MP3 bytes referenced by the `audio_filename` field are
deliberately NOT committed in this PR. They will land in a
follow-up commit that preserves the existing manifest.jsone
SHA-256 contract.
