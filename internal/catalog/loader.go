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

// LoadFromDir walks root one level deep and returns a Catalog
// built from every immediate subdirectory that contains a
// track.json, sorted by id for stable iteration.
//
// Subdirectories without track.json are skipped (operators may
// stage asset-only directories). A track that fails Validate()
// aborts the whole load with an error naming the offending id:
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
		jsonPath := filepath.Join(root, entry.Name(), trackFileName)
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			// No track.json -> skip silently.
			continue
		}
		var tr Track
		if err := json.Unmarshal(data, &tr); err != nil {
			return nil, fmt.Errorf("catalog: parse %s: %w", jsonPath, err)
		}
		if err := tr.Validate(); err != nil {
			return nil, fmt.Errorf("catalog: invalid track id=%q in %s: %w", tr.ID, jsonPath, err)
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