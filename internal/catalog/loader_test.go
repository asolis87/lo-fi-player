package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTrackDir stages a single <id>/track.json under root from a
// mutated validTrack(). Tests use it to keep each fixture one line.
func writeTrackDir(t *testing.T, root string, id string, mutate func(*Track)) {
	t.Helper()
	tr := validTrack()
	tr.ID = id
	if mutate != nil {
		mutate(&tr)
	}
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "track.json"), data, 0o644); err != nil {
		t.Fatalf("write track.json for %s: %v", id, err)
	}
}

// TestLoadFromDir_ValidCatalog: three valid tracks + a stray
// LICENSE.txt MUST load to a 3-track catalog in sorted order.
func TestLoadFromDir_ValidCatalog(t *testing.T) {
	root := t.TempDir()
	writeTrackDir(t, root, "track-drizzle", nil)
	writeTrackDir(t, root, "track-rain", nil)
	writeTrackDir(t, root, "track-lofi-001", nil)
	if err := os.WriteFile(filepath.Join(root, "LICENSE.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("seed LICENSE.txt: %v", err)
	}

	cat, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if got, want := len(cat.Tracks), 3; got != want {
		t.Fatalf("len(Tracks) = %d, want %d", got, want)
	}
	for i, want := range []string{"track-drizzle", "track-lofi-001", "track-rain"} {
		if got := cat.Tracks[i].ID; got != want {
			t.Fatalf("Tracks[%d].ID = %q, want %q", i, got, want)
		}
	}
}

// TestLoadFromDir_ReportsOffendingTrackID: a bad track aborts
// the load with an error that names its id.
func TestLoadFromDir_ReportsOffendingTrackID(t *testing.T) {
	root := t.TempDir()
	writeTrackDir(t, root, "track-drizzle", nil)
	writeTrackDir(t, root, "track-bad", func(tr *Track) { tr.License = "CC-BY-NC" })
	writeTrackDir(t, root, "track-rain", nil)

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir: expected error for bad track, got nil")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !strings.Contains(err.Error(), "track-bad") {
		t.Fatalf("error %q lacks offending track id", err.Error())
	}
}