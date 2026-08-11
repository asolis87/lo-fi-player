package catalog

import (
	"encoding/json"
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

// LoadFromDir walks root one level deep and returns a Catalog
// built from every immediate subdirectory that contains a
// track.json, sorted by id for stable iteration.
//
// Per the PR-D #3.1 contract: a subdirectory without track.json
// OR without audio.mp3 aborts the whole load with an error that
// names the offending subdir (slice #1 silently skipped those and
// shipped empty catalogs when the cache was half-populated).
// Stray non-directory entries at the root (e.g. README.md,
// LICENSE.txt) are still ignored. A track that fails Validate()
// still aborts the load with an error naming the offending id:
// shipping a partial catalog is worse than failing loud.
func LoadFromDir(root string) (*Catalog, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s: %w", root, err)
	}
	tracks := make([]Track, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subdir := filepath.Join(root, entry.Name())
		jsonPath := filepath.Join(subdir, trackFileName)
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			return nil, fmt.Errorf("catalog: missing %s in %s: %w", trackFileName, subdir, err)
		}
		audioPath := filepath.Join(subdir, audioFileName)
		if _, err := os.Stat(audioPath); err != nil {
			return nil, fmt.Errorf("catalog: missing %s in %s: %w", audioFileName, subdir, err)
		}
		var tr Track
		if err := json.Unmarshal(data, &tr); err != nil {
			return nil, fmt.Errorf("catalog: parse %s: %w", jsonPath, err)
		}
		if err := tr.Validate(); err != nil {
			return nil, fmt.Errorf("catalog: invalid track id=%q in %s: %w", tr.ID, jsonPath, err)
		}
		if err := Verify(audioPath, tr.ChecksumSHA256); err != nil {
			return nil, fmt.Errorf("catalog: checksum mismatch for id=%q in %s: %w", tr.ID, subdir, err)
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