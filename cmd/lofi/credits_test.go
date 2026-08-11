package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

func writeCreditsCatalog(t *testing.T) {
	t.Helper()
	root := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "lofi-player", "catalog", "v1", "track")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(catalog.Track{SchemaVersion: 1, ID: "track", Title: "Track", Artist: "Artist", License: catalog.LicenseCC0, LicenseStatus: catalog.LicenseStatusVerified, SourceURL: "https://example.test", ChecksumSHA256: "ebc2689f897aa333887187a499a15658989ca923cbd49ecc8080b6eef955cdc6", AttributionText: "Track by Artist", DurationSeconds: 1, AudioFilename: "audio.mp3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "track.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "audio.mp3"), []byte("dummy bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCredits_Default_PrintsNotice(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeCreditsCatalog(t)
	err, stdout := captureStdout(t, func() error { return runCredits(nil) })
	if err != nil || !strings.Contains(stdout, "NOTICE: lo-fi-player") || !strings.Contains(stdout, "Track by Artist") {
		t.Fatalf("err=%v stdout=%q", err, stdout)
	}
}

func TestCredits_JSON_PrintsCatalog(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeCreditsCatalog(t)
	err, stdout := captureStdout(t, func() error { return runCredits([]string{"--json"}) })
	if err != nil || !strings.Contains(stdout, `"tracks"`) || !strings.Contains(stdout, `"id": "track"`) {
		t.Fatalf("err=%v stdout=%q", err, stdout)
	}
}

func TestCredits_UnknownFlag_Exits2(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	code, stderr := captureStderr(t, func() int { return codeFor(runCredits([]string{"--yaml"})) })
	if code != 2 || !strings.Contains(stderr, "Usage") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestCredits_NoCatalog_Exits1_WithSyncHint(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	code, stderr := captureStderr(t, func() int { return codeFor(runCredits(nil)) })
	if code != 1 || !strings.Contains(stderr, "lofi sync") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestCredits_ExtraPositional_Exits2(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	code, stderr := captureStderr(t, func() int { return codeFor(runCredits([]string{"extra"})) })
	if code != 2 || !strings.Contains(stderr, "Usage") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}
