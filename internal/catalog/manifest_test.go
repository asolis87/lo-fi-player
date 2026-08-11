package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		t.Fatalf("manifest has 0 tracks; slice #1 seed must include at least one placeholder track")
	}
	for _, tr := range got.Tracks {
		if err := tr.Validate(); err != nil {
			t.Errorf("manifest track %q failed Validate: %v", tr.ID, err)
		}
	}
}

// TestManifestSchema_TrackSampleHasNeedConfirmation guards the
// Decision #326 re-verification gate: every track in the seeded
// catalog/v1/ manifest MUST carry license_status: "NEEDS
// CONFIRMATION" because the source licenses are uncertain from
// the sandbox where the seed was authored. Promotion to
// "VERIFIED" requires a later observation report from a
// non-blocked network; the loader already routes these tracks
// under "Unverified licenses" in the TUI (REQ-ATT-3).
func TestManifestSchema_TrackSampleHasNeedConfirmation(t *testing.T) {
	path := v1ManifestPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var got Catalog
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if len(got.Tracks) == 0 {
		t.Fatalf("manifest has 0 tracks; sample id=track-sample-001 placeholder must be present")
	}
	var found bool
	for _, tr := range got.Tracks {
		if tr.ID == "track-sample-001" {
			found = true
		}
		if tr.LicenseStatus != LicenseStatusNeedsConfirmation {
			t.Errorf("manifest track %q license_status = %q, want %q (Decision #326 re-verification gate)",
				tr.ID, tr.LicenseStatus, LicenseStatusNeedsConfirmation)
		}
		// The checksum placeholder is the SHA-256 of the empty
		// input ("0000...0000"). Until the MP3 bytes are committed
		// it MUST be exactly that 64-hex-zero string so the
		// checksum test cannot be silently bypassed.
		if !strings.HasPrefix(tr.ChecksumSHA256, "0000") || len(tr.ChecksumSHA256) != 64 {
			t.Errorf("manifest track %q checksum_sha256 = %q, want 64-hex-zero placeholder per Decision #326",
				tr.ID, tr.ChecksumSHA256)
		}
	}
	if !found {
		t.Errorf("seeded manifest must include track-sample-001 as the structural placeholder (Decision #326)")
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
	found := false
	for _, tr := range cat.Tracks {
		if tr.ID == "track-sample-001" {
			found = true
		}
	}
	if !found {
		t.Errorf("LoadFromFile seeded catalog missing track-sample-001 placeholder")
	}
}
