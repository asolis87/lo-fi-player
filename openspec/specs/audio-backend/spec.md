# audio-backend Specification

## Purpose

Defines the contract for `lofi play <id>` resolving the audio path of a catalog track and dispatching it to the mpv backend.

## Requirements

### Requirement: `lofi play <id>` Resolves the Cached Audio Path

`runHeadlessPlay(target)` MUST resolve `target` (a track id) against the local catalog and pass the resolved audio path to the `AudioBackend.Load` call. The path MUST come from the per-track `<id>/audio.mp3` location under the cache root (`$XDG_CACHE_HOME/lo-fi-player/catalog/v1/`).


#### Scenario: A resolved track id plays the right bytes

- GIVEN the cache contains `track-001/track.json` and `track-001/audio.mp3` (1 MB)
- WHEN the user runs `lofi play track-001`
- THEN the catalog is loaded
- AND `track-001` is found
- AND `backend.Load(audio.Track{ID: "track-001", Path: "<cache>/track-001/audio.mp3"})` is called
- AND mpv plays the bytes at that path

#### Scenario: An unknown track id exits 2

- GIVEN the cache has no track with id `track-doesnt-exist`
- WHEN the user runs `lofi play track-doesnt-exist`
- THEN `findTrackByID` returns `ErrTrackNotFound`
- AND the CLI prints a clear error to stderr
- AND the exit code is 2 (usage error)

### Requirement: Procedural Stations Bypass the Catalog Path

`lofi play procedural:<station>` MUST continue to short-circuit on the `procedural:` prefix and dispatch to the procedural backend directly, never touching the catalog. The CLI MUST reject any station not on the whitelist with exit 2.


#### Scenario: `procedural:rain` plays without loading the catalog

- GIVEN the user runs `lofi play procedural:rain`
- WHEN the CLI dispatches
- THEN the catalog is NOT loaded
- AND the procedural backend's `procedural:rain` station is selected
- AND audio is produced via `oto/v3` (or the `noopDevice` fallback on hosts without an audio device)

#### Scenario: An unknown station exits 2

- GIVEN the user runs `lofi play procedural:brown`
- WHEN the CLI dispatches
- THEN the procedural-station whitelist rejects `brown`
- AND stderr contains `lofi play: unknown procedural station: brown`
- AND the exit code is 2

### Requirement: Backend Selection Falls Back Gracefully

`audio.Select` MUST prefer the mpv backend when `mpv` is on `$PATH`, and MUST return `ErrNoAudioBackend` if neither mpv nor the procedural path applies. The CLI MUST surface `ErrNoAudioBackend` as a clear install-instruction message on stderr with exit 1.


#### Scenario: `mpv` missing and a non-procedural target is requested

- GIVEN `mpv` is not on `$PATH`
- AND the user runs `lofi play track-001` (a catalog track)
- WHEN `audio.Select` runs
- THEN `ErrNoAudioBackend` is returned
- AND stderr contains `lofi: mpv is required to play catalog tracks; install mpv or use 'lofi play procedural:rain'`
- AND the exit code is 1
