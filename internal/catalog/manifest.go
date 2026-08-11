package catalog

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadFromFile reads a track catalog JSON file from disk, validates
// its schema_version against CurrentSchemaVersion, and runs every
// track through Track.Validate so the caller gets a fully-checked
// Catalog back or a single error naming the offending field. The
// function is the single-load counterpart to LoadFromDir: the
// first-run fetch reads a manifest.json from the pinned URL, and
// the seeded catalog/v1/manifest.json is loaded by the same path
// so production and the seed cannot drift.
//
// A manifest that fails validation does NOT leave a partial
// catalog behind: the returned Catalog is nil and the error
// wraps ErrInvalidTrack so callers can match on errors.Is.
func LoadFromFile(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s: %w", path, err)
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("catalog: parse %s: %w", path, err)
	}
	if c.SchemaVersion != CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version=%d, want %d",
			ErrInvalidTrack, c.SchemaVersion, CurrentSchemaVersion)
	}
	if c.Version == "" {
		return nil, fmt.Errorf("%w: version is empty", ErrInvalidTrack)
	}
	for i := range c.Tracks {
		if err := c.Tracks[i].Validate(); err != nil {
			return nil, fmt.Errorf("catalog: invalid track id=%q in %s: %w",
				c.Tracks[i].ID, path, err)
		}
	}
	return &c, nil
}
