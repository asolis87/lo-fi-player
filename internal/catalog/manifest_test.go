package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// v1ManifestPath resolves the path to the seeded catalog/v1/manifest.json
// from the test's runtime directory. manifest_test.go lives in
// internal/catalog/, so two levels up is the repo root and
// catalog/v1/manifest.json is the seeded index Phase 6 ships.
func v1ManifestPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "catalog", "v1", "manifest.json"))
	if err != nil {
		t.Fatalf("abs manifest path: %v", err)
	}
	return abs
}

// v1CatalogRoot resolves the directory that holds the per-track
// subdirectories referenced by manifest.json. The companion of
// v1ManifestPath; both share the same ../.. path so the test
// bundle resolves the seeded catalog from the repo root.
func v1CatalogRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "catalog", "v1"))
	if err != nil {
		t.Fatalf("abs catalog root: %v", err)
	}
	return abs
}

// sha256File streams path through crypto/sha256 and returns the
// lowercase hex digest. Mirrors the loader's checksum verifier
// (internal/catalog/checksum.go:Verify) so the test asserts the
// same value the runtime would compute against the same bytes.
func sha256File(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := h.Write([]byte{}); err != nil {
		t.Fatalf("hash: %v", err)
	}
	buf := make([]byte, 1<<16)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := h.Write(buf[:n]); werr != nil {
				t.Fatalf("hash read: %v", werr)
			}
		}
		if err != nil {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestManifestSchema_LoadsFromDisk guards Phase 6.2: the seeded
// catalog/v1/manifest.json MUST exist on disk and parse as a
// Catalog (schema_version, version, tracks). This is the index
// the binary uses on first-run fetch and the slice #1 contract
// that the bundled catalog is reproducible from a single file.
func TestManifestSchema_LoadsFromDisk(t *testing.T) {
	path := v1ManifestPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var got Catalog
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse manifest: %v\n%s", err, data)
	}
	if got.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("manifest schema_version = %d, want %d", got.SchemaVersion, CurrentSchemaVersion)
	}
	if got.Version == "" {
		t.Errorf("manifest version is empty")
	}
	if len(got.Tracks) == 0 {
		t.Fatalf("manifest has 0 tracks; slice #1 seed must include at least one track")
	}
	for _, tr := range got.Tracks {
		if err := tr.Validate(); err != nil {
			t.Errorf("manifest track %q failed Validate: %v", tr.ID, err)
		}
	}
}

// TestManifestSchema_RealCatalog guards PR-A slice-2 the
// real-catalog seed: 4 licensed CC-BY 4.0 tracks (LOFI LION + 3
// Lee Rosevere) bundled at catalog/v1/ with the matching
// subdirectory, audit report, and audio bytes. The test
// supersedes the slice-1 placeholder assertions
// (TestManifestSchema_TrackSampleHasNeedConfirmation + the
// track-sample-001 references in TestLoadFromFile_LoadsSeededManifest)
// which guarded the NEEDS CONFIRMATION seed that PR-A removed.
//
// Asserts:
//   - schema_version = 1, version = "v1.0"
//   - exactly 4 tracks, all with license_status = VERIFIED
//   - every track.id has a matching <id>/ subdirectory
//   - every track.checksum_sha256 matches the live SHA-256 of
//     <id>/audio.mp3 (the bytes the loader will Verify at load)
//   - every track.audio_filename resolves to a real file on disk
//   - no track id is the slice-1 placeholder "track-sample-001"
func TestManifestSchema_RealCatalog(t *testing.T) {
	path := v1ManifestPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var got Catalog
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse manifest: %v\n%s", err, data)
	}
	if got.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, CurrentSchemaVersion)
	}
	if got.Version != "v1.0" {
		t.Errorf("version = %q, want %q", got.Version, "v1.0")
	}
	const wantTracks = 4
	if len(got.Tracks) != wantTracks {
		t.Fatalf("len(tracks) = %d, want %d", len(got.Tracks), wantTracks)
	}
	seen := make(map[string]bool, len(got.Tracks))
	for _, tr := range got.Tracks {
		if tr.ID == "track-sample-001" {
			t.Errorf("manifest still references slice-1 placeholder track-sample-001; PR-A replaces it with real tracks")
		}
		if seen[tr.ID] {
			t.Errorf("duplicate track id %q in manifest", tr.ID)
		}
		seen[tr.ID] = true
		if tr.LicenseStatus != LicenseStatusVerified {
			t.Errorf("track %q license_status = %q, want %q", tr.ID, tr.LicenseStatus, LicenseStatusVerified)
		}
		if err := tr.Validate(); err != nil {
			t.Errorf("track %q Validate: %v", tr.ID, err)
		}
		root := v1CatalogRoot(t)
		subdir := filepath.Join(root, tr.ID)
		if info, err := os.Stat(subdir); err != nil {
			t.Errorf("track %q subdir missing: %v", tr.ID, err)
		} else if !info.IsDir() {
			t.Errorf("track %q entry %s is not a directory", tr.ID, subdir)
		}
		audioPath := filepath.Join(subdir, tr.AudioFilename)
		if _, err := os.Stat(audioPath); err != nil {
			t.Errorf("track %q audio %s missing: %v", tr.ID, audioPath, err)
			continue
		}
		gotHash := sha256File(t, audioPath)
		if gotHash != tr.ChecksumSHA256 {
			t.Errorf("track %q checksum_sha256 = %q, actual sha256(%s) = %q",
				tr.ID, tr.ChecksumSHA256, audioPath, gotHash)
		}
	}
}

// TestLoadFromFile_LoadsSeededManifest exercises the catalog
// loader against the seeded manifest.json so a downstream slice
// that pulls the file via first-run fetch can rely on the same
// parsing path as the on-disk cache. The test doubles as a
// regression for the loader's schema_version and Validate()
// gates.
func TestLoadFromFile_LoadsSeededManifest(t *testing.T) {
	path := v1ManifestPath(t)
	cat, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile(%s): %v", path, err)
	}
	if cat == nil {
		t.Fatal("LoadFromFile returned nil catalog with no error")
	}
	if len(cat.Tracks) == 0 {
		t.Fatal("LoadFromFile returned catalog with no tracks")
	}
	// The slice-1 placeholder is gone; assert the real seed
	// ids are present so a regression that silently drops the
	// PR-A restage fails loud.
	want := map[string]bool{
		"lofi-lion-tame-the-beast": false,
		"bigger-questions":         false,
		"going-in-circles":         false,
		"it-was-like-that-when-i-got-here": false,
	}
	for _, tr := range cat.Tracks {
		if _, ok := want[tr.ID]; ok {
			want[tr.ID] = true
		}
	}
	for id, ok := range want {
		if !ok {
			t.Errorf("LoadFromFile seeded catalog missing %q from PR-A real seed", id)
		}
	}
}
