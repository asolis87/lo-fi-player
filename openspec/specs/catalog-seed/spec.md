# catalog-seed Specification

## Purpose

Defines the per-track directory contract for the bundled catalog: `<id>/track.json` + `<id>/LICENSE.txt` + `<id>/audio.mp3`, plus the signed verification report that promotes a track from `NEEDS CONFIRMATION` to `VERIFIED`. The bundled catalog lives at `catalog/v1/` in the repo and is mirrored to `$XDG_CACHE_HOME/lo-fi-player/catalog/v1/` by `lofi sync`.

## Requirements

### Requirement: Per-Track Directory Contract

The catalog SHALL ship each track as a directory `<id>/` containing exactly three files: `track.json` (metadata), `LICENSE.txt` (license text), `audio.mp3` (audio bytes, MP3 192 kbps). The directory MUST be a sibling of `manifest.json` under `catalog/v1/`.

#### Scenario: A valid per-track directory loads

- GIVEN a directory `catalog/v1/track-001/` containing `track.json`, `LICENSE.txt`, `audio.mp3`
- WHEN `LoadFromDir` walks the catalog root
- THEN the track is loaded and validated
- AND `Track.AudioFilename == "audio.mp3"` matches the on-disk file

#### Scenario: Missing `audio.mp3` fails validation

- GIVEN a directory `catalog/v1/track-002/` with `track.json` and `LICENSE.txt` but no `audio.mp3`
- WHEN `LoadFromDir` walks the catalog root
- THEN the loader reports an error naming `track-002`
- AND no track is returned in the catalog

### Requirement: `Track.AudioFilename` Is Required

`Track` MUST carry an `AudioFilename` field (`json: "audio_filename"`). The loader MUST reject any track where `AudioFilename` is empty or where the named file is missing from the track directory.

#### Scenario: Empty `AudioFilename` is rejected

- GIVEN a `track.json` with `audio_filename: ""`
- WHEN the track is validated
- THEN validation fails with `ErrInvalidTrack`
- AND the error names the offending track id

### Requirement: License Status Lifecycle

Each track MUST carry a `license_status` field with one of two values: `NEEDS CONFIRMATION` or `VERIFIED`. A track in `NEEDS CONFIRMATION` MUST NOT be promoted to `VERIFIED` without a corresponding signed verification report at `catalog/v1/verification/<id>.md`.

#### Scenario: Promotion requires a signed report

- GIVEN a track with `license_status: "NEEDS CONFIRMATION"` and no file at `catalog/v1/verification/<id>.md`
- WHEN the loader runs
- THEN the track remains `NEEDS CONFIRMATION` in the displayed catalog
- AND it appears under the "Unverified licenses" group in the TUI

#### Scenario: Promotion succeeds with a valid report

- GIVEN a track with `license_status: "NEEDS CONFIRMATION"` and a valid signed report at `catalog/v1/verification/<id>.md`
- WHEN a maintainer changes `license_status` to `VERIFIED` and signs the report
- THEN the loader accepts the track
- AND the track moves out of the "Unverified licenses" group

### Requirement: Verification Report Format

A verification report MUST be a Markdown file at `catalog/v1/verification/<id>.md` containing: the source URL of the license claim, an HTML snapshot path, the re-hashed SHA-256 of the audio bytes, and an operator signature. The loader MUST NOT auto-parse signatures in slice #2; the report is a human-checked contract, and `license_status` is the loader's signal of trust.

#### Scenario: Report without operator signature is invalid

- GIVEN a report at `catalog/v1/verification/track-001.md` missing an operator signature line
- WHEN a maintainer reviews it
- THEN the report is rejected as invalid
- AND the track's `license_status` is not changed to `VERIFIED`
