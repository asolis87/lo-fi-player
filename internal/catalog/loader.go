package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// trackFileName is the basename every per-track directory MUST
// contain. Siblings (LICENSE.txt, audio bytes, cover art) are
// owned by other layers and intentionally ignored here.
const trackFileName = "track.json"

// audioFileName is the basename of the audio payload every
// per-track directory MUST contain alongside track.json. The
// field is informational (Track.AudioFilename records the
// manifest-level name); the loader enforces the on-disk
// contract — a subdir without audio.mp3 aborts the load — so a
// half-populated cache never produces an empty catalog.
const audioFileName = "audio.mp3"

// expectedTrackCount is the MVP catalog size the manifest-backed
// path of LoadFromDir enforces (REQ-CAT-MVP-1, PR-4 #4.2). It
// applies ONLY when manifest.json is present — legacy fixture
// dirs without a manifest retain the pre-MVP walk-subdirs flow.
const expectedTrackCount = 4

// ErrUnexpectedTrackCount wraps every "manifest declares N tracks,
// expected 4" failure. Callers match via errors.Is; the message
// always exposes both expected and observed counts.
var ErrUnexpectedTrackCount = errors.New("catalog: unexpected track count")

// LoadFromDir returns a Catalog built from the cache at root.
// Two paths, dispatched by the presence of <root>/manifest.json
// (the artifact Syncer.Apply writes after a successful sync):
//
//   - Manifest-backed (manifest.json present): the manifest IS
//     the source of truth for track count and metadata. The
//     loader validates the manifest via LoadFromFile, enforces
//     exactly expectedTrackCount tracks (MVP contract), then
//     verifies every declared track has a matching subdir with
//     audio.mp3 whose SHA matches the manifest's ChecksumSHA256.
//
//   - Legacy (no manifest.json): walk root one level deep and
//     trust every immediate subdirectory with track.json +
//     audio.mp3. No count gate — pre-MVP fixtures keep working.
//
// A subdirectory without track.json OR without audio.mp3 aborts
// the legacy load naming the offending subdir (PR-D #3.1). A
// track that fails Validate() aborts naming its id: shipping a
// partial catalog is worse than failing loud.
func LoadFromDir(root string) (*Catalog, error) {
	manifestPath := filepath.Join(root, manifestFileName)
	if _, err := os.Stat(manifestPath); err == nil {
		return loadManifestBackedDir(root, manifestPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("catalog: stat %s: %w", manifestPath, err)
	}
	return loadLegacyDir(root)
}

// loadManifestBackedDir enforces the MVP contract for the cache
// Syncer.Apply produces. The count gate runs AFTER LoadFromFile
// validates each track's metadata, so a malformed manifest reports
// the real validation cause (ErrInvalidTrack) instead of being
// misreported as a count mismatch. Subdir verification runs LAST
// and reports ErrChecksumMismatch for SHA drift on any declared
// track.
func loadManifestBackedDir(root, manifestPath string) (*Catalog, error) {
	cat, err := LoadFromFile(manifestPath)
	if err != nil {
		return nil, err
	}
	if len(cat.Tracks) != expectedTrackCount {
		return nil, fmt.Errorf("%w: expected %d, got %d",
			ErrUnexpectedTrackCount, expectedTrackCount, len(cat.Tracks))
	}
	for i := range cat.Tracks {
		declared := &cat.Tracks[i]
		subdir := filepath.Join(root, declared.ID)
		info, err := os.Stat(subdir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("catalog: manifest declares id=%q but %s has no subdir", declared.ID, root)
			}
			return nil, fmt.Errorf("catalog: stat %s: %w", subdir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("catalog: manifest declares id=%q but %s is not a directory", declared.ID, subdir)
		}
		audioPath := filepath.Join(subdir, audioFileName)
		if _, err := os.Stat(audioPath); err != nil {
			return nil, fmt.Errorf("catalog: missing %s in %s: %w", audioFileName, subdir, err)
		}
		if err := Verify(audioPath, declared.ChecksumSHA256); err != nil {
			return nil, fmt.Errorf("catalog: checksum mismatch for id=%q in %s: %w", declared.ID, subdir, err)
		}
	}
	sort.Slice(cat.Tracks, func(i, j int) bool { return cat.Tracks[i].ID < cat.Tracks[j].ID })
	return cat, nil
}

// loadLegacyDir walks root one level deep and returns a Catalog
// built from every immediate subdirectory that contains a
// track.json, sorted by id. No count gate — legacy fixture dirs
// keep their pre-MVP semantics.
func loadLegacyDir(root string) (*Catalog, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s: %w", root, err)
	}
	tracks := make([]Track, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		tr, err := loadTrackFromEntry(root, entry)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, tr)
	}
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].ID < tracks[j].ID })
	return &Catalog{
		SchemaVersion: CurrentSchemaVersion,
		Version:       "1",
		Tracks:        tracks,
	}, nil
}

// loadTrackFromEntry reads one per-track subdirectory, validates
// the track.json + audio.mp3 pair, and returns the parsed Track.
// Existing legacy error messages are preserved verbatim so the
// subdir-level tests in loader_test.go keep asserting on the same
// substrings.
func loadTrackFromEntry(root string, entry os.DirEntry) (Track, error) {
	subdir := filepath.Join(root, entry.Name())
	jsonPath := filepath.Join(subdir, trackFileName)
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return Track{}, fmt.Errorf("catalog: missing %s in %s: %w", trackFileName, subdir, err)
	}
	audioPath := filepath.Join(subdir, audioFileName)
	if _, err := os.Stat(audioPath); err != nil {
		return Track{}, fmt.Errorf("catalog: missing %s in %s: %w", audioFileName, subdir, err)
	}
	var tr Track
	if err := json.Unmarshal(data, &tr); err != nil {
		return Track{}, fmt.Errorf("catalog: parse %s: %w", jsonPath, err)
	}
	if err := tr.Validate(); err != nil {
		return Track{}, fmt.Errorf("catalog: invalid track id=%q in %s: %w", tr.ID, jsonPath, err)
	}
	if err := Verify(audioPath, tr.ChecksumSHA256); err != nil {
		return Track{}, fmt.Errorf("catalog: checksum mismatch for id=%q in %s: %w", tr.ID, subdir, err)
	}
	return tr, nil
}
