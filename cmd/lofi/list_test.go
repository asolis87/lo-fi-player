package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// writeListCatalog stages a tiny two-track catalog under the test's
// XDG cache root so runList can load it. The tracks are sorted by
// id so the table-output assertions are deterministic.
func writeListCatalog(t *testing.T) {
	t.Helper()
	root := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "lofi-player", "catalog", "v1")
	for _, id := range []string{"track-rain", "track-drizzle"} {
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		tr := catalog.Track{
			SchemaVersion:   catalog.CurrentSchemaVersion,
			ID:              id,
			Title:           "Title " + id,
			Artist:          "Artist " + id,
			License:         catalog.LicenseCCBY,
			LicenseStatus:   catalog.LicenseStatusVerified,
			SourceURL:       "https://example.test/" + id,
			ChecksumSHA256:  "ebc2689f897aa333887187a499a15658989ca923cbd49ecc8080b6eef955cdc6",
			AttributionText: "by Artist " + id,
			DurationSeconds: 60,
			AudioFilename:   "audio.mp3",
		}
		data, err := json.MarshalIndent(tr, "", "  ")
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "track.json"), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "audio.mp3"), []byte("dummy bytes"), 0o644); err != nil {
			t.Fatalf("write audio.mp3 for %s: %v", id, err)
		}
	}
}

// TestList_PrintsTableAndJSON guards the lofi list happy path:
// the default mode prints a tabular listing sorted by track id;
// --json prints the same Catalog bytes loadFromDir produced.
func TestList_PrintsTableAndJSON(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeListCatalog(t)

	t.Run("table", func(t *testing.T) {
		err, stdout := captureStdout(t, func() error { return runList(nil) })
		if err != nil {
			t.Fatalf("runList(nil): %v", err)
		}
		// Header + both rows present, sorted by id.
		for _, want := range []string{"ID", "TITLE", "ARTIST", "LICENSE", "track-drizzle", "track-rain"} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("table output missing %q:\n%s", want, stdout)
			}
		}
		// drizzle must appear before rain (sorted by id).
		if i := strings.Index(stdout, "track-drizzle"); i >= 0 {
			if j := strings.Index(stdout, "track-rain"); j >= 0 && i > j {
				t.Fatalf("table not sorted by id: drizzle@%d rain@%d", i, j)
			}
		}
	})

	t.Run("json", func(t *testing.T) {
		err, stdout := captureStdout(t, func() error { return runList([]string{"--json"}) })
		if err != nil {
			t.Fatalf("runList(--json): %v", err)
		}
		var got catalog.Catalog
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("json output is not a Catalog: %v\n%s", err, stdout)
		}
		if len(got.Tracks) != 2 {
			t.Fatalf("len(Tracks) = %d, want 2", len(got.Tracks))
		}
		if got.Tracks[0].ID != "track-drizzle" || got.Tracks[1].ID != "track-rain" {
			t.Fatalf("tracks out of order: %+v", got.Tracks)
		}
	})
}

// TestList_NoCatalogExits1WithSyncHint mirrors the credits-no-catalog
// path: when the cache directory has no catalog yet, runList MUST
// exit 1 and surface a hint that points at `lofi sync`.
func TestList_NoCatalogExits1WithSyncHint(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	code, stderr := captureStderr(t, func() int { return codeFor(runList(nil)) })
	if code != 1 {
		t.Fatalf("runList(nil) with no catalog code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "lofi sync") {
		t.Fatalf("expected lofi sync hint in stderr, got %q", stderr)
	}
}

// TestList_UnknownFlag_Exits2 keeps the flag parser consistent with
// runCredits: any unknown positional exits 2 with usage.
func TestList_UnknownFlag_Exits2(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	code, stderr := captureStderr(t, func() int { return codeFor(runList([]string{"--yaml"})) })
	if code != 2 {
		t.Fatalf("runList(--yaml) code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage") {
		t.Fatalf("expected usage banner, got %q", stderr)
	}
}