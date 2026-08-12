# catalog-loader Specification

## Purpose

Defines the behavior of `LoadFromDir` and `LoadFromFile` after the loader-drift fix: per-track subdirs are load-bearing, `AudioFilename` is required, and checksums are verified at load time.

## Requirements

### Requirement: `LoadFromDir` Subdir Contract

`LoadFromDir(root)` MUST walk one level of subdirectories under `root`, expect each to be a per-track directory containing `track.json` and `audio.mp3`, and validate the track. If a subdirectory is missing `track.json` or `audio.mp3`, `LoadFromDir` MUST report an error naming the subdirectory id; subdirectories without `track.json` MUST NOT be silently skipped (slice #1 silently skipped them, masking the placeholder seed).


#### Scenario: Subdir with `track.json` and `audio.mp3` loads

- GIVEN a subdir `<id>/` with both files present and valid
- WHEN `LoadFromDir` walks the catalog root
- THEN the track is loaded, validated, and included in the catalog
- AND the catalog is sorted by track id (ascending)

#### Scenario: Subdir missing `audio.mp3` is reported

- GIVEN a subdir `<id>/` with `track.json` but no `audio.mp3`
- WHEN `LoadFromDir` walks the catalog root
- THEN the load aborts with an error naming `<id>`
- AND no partial catalog is returned

#### Scenario: Non-track entry at the root is ignored

- GIVEN a non-directory file (e.g. `README.md`) at the catalog root alongside track subdirs
- WHEN `LoadFromDir` walks the catalog root
- THEN the file is ignored
- AND only the track subdirs contribute to the catalog

### Requirement: Checksum Verification at Load

`LoadFromDir` and `LoadFromFile` MUST verify `Track.ChecksumSHA256` against the actual bytes of the audio file before returning the catalog. A mismatch MUST fail the load and report the expected vs. actual hash.


#### Scenario: Matching checksum is accepted

- GIVEN a track with `checksum_sha256` matching the SHA-256 of `<id>/audio.mp3`
- WHEN the loader runs
- THEN the track is included in the catalog
- AND no warning is emitted

#### Scenario: Mismatched checksum is rejected

- GIVEN a track with `checksum_sha256: "0000…0000"` but `audio.mp3` with non-zero hash
- WHEN the loader runs
- THEN the load aborts with `ErrChecksumMismatch`
- AND the error names the track id and the expected vs. actual hash

### Requirement: `LoadFromFile` Is Retained for Tests and Manifest Inspection

`LoadFromFile(path)` MUST continue to read a single `manifest.json` and return a `*Catalog` without walking the directory. The CLI runtime MUST continue to use `LoadFromDir`; `LoadFromFile` is reserved for the manifest test and for any future admin tool that inspects the manifest in isolation.


#### Scenario: `LoadFromFile` reads a manifest without directory walking

- GIVEN a valid `manifest.json` at a given path
- WHEN `LoadFromFile(path)` is called
- THEN a `*Catalog` is returned with the tracks listed in the manifest
- AND no filesystem walk occurs
