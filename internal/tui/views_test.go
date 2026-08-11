package tui

import (
	"strings"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// TestViewAttribution_RendersAllFiveFields is mandatory (REQ-ATT-2):
// CC-BY and CC-BY-SA tracks must show title, artist, license,
// source URL, and SHA-256.
func TestViewAttribution_RendersAllFiveFields(t *testing.T) {
	track := catalog.Track{
		SchemaVersion:   catalog.CurrentSchemaVersion,
		ID:              "rain_drizzle",
		Title:           "Slow Rain on a Tin Roof",
		Artist:          "Anonymous",
		License:         catalog.LicenseCCBY,
		LicenseStatus:   catalog.LicenseStatusVerified,
		SourceURL:       "https://archive.org/details/rain-drizzle",
		ChecksumSHA256:  "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		AttributionText: "Slow Rain on a Tin Roof by Anonymous, licensed CC-BY-4.0.",
		DurationSeconds: 42,
	}
	c := &catalog.Catalog{SchemaVersion: catalog.CurrentSchemaVersion, Version: "1", Tracks: []catalog.Track{track}}
	m := NewModel(audio.NewMockBackend(), c, nil)
	m.Mode = ModeAttribution

	out := viewAttribution(m)

	wants := []string{
		"Slow Rain on a Tin Roof",   // title
		"Anonymous",                 // artist
		"CC-BY",                     // license
		"https://archive.org/details/rain-drizzle", // source
		"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", // checksum
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("attribution view missing %q\n--view--\n%s", want, out)
		}
	}
}

func TestViewAttribution_EmptyCatalogRendersPlaceholder(t *testing.T) {
	m := NewModel(audio.NewMockBackend(), nil, nil)
	m.Mode = ModeAttribution
	out := viewAttribution(m)
	if !strings.Contains(out, "no track") {
		t.Fatalf("expected placeholder, got %q", out)
	}
}

// TestViewCatalog_GroupsUnverifiedUnderLabel is mandatory (REQ-ATT-3
// / S-ATT-2): every NEEDS CONFIRMATION track MUST appear under the
// "Unverified licenses" group.
func TestViewCatalog_GroupsUnverifiedUnderLabel(t *testing.T) {
	verified := catalog.Track{
		SchemaVersion:   catalog.CurrentSchemaVersion,
		ID:              "verified_track",
		Title:           "Verified Title",
		Artist:          "Verified Artist",
		License:         catalog.LicenseCC0,
		LicenseStatus:   catalog.LicenseStatusVerified,
		SourceURL:       "https://archive.org/details/verified",
		ChecksumSHA256:  strings.Repeat("a", 64),
		AttributionText: "verified",
		DurationSeconds: 60,
	}
	unverified := verified
	unverified.ID = "needs_check"
	unverified.Title = "Unverified Title"
	unverified.Artist = "Unverified Artist"
	unverified.LicenseStatus = catalog.LicenseStatusNeedsConfirmation
	unverified.ChecksumSHA256 = strings.Repeat("b", 64)

	c := &catalog.Catalog{SchemaVersion: catalog.CurrentSchemaVersion, Version: "1", Tracks: []catalog.Track{verified, unverified}}
	m := NewModel(audio.NewMockBackend(), c, nil)
	m.Mode = ModeCatalog

	out := viewCatalog(m)

	if !strings.Contains(out, "Unverified licenses") {
		t.Fatalf("catalog view missing \"Unverified licenses\" group label\n--view--\n%s", out)
	}
	// The unverified track MUST appear AFTER the "Unverified licenses"
	// label (so it is in the group, not bundled silently).
	idxLabel := strings.Index(out, "Unverified licenses")
	idxUnverified := strings.Index(out, "needs_check")
	idxVerified := strings.Index(out, "verified_track")
	if idxLabel < 0 || idxUnverified < 0 || idxVerified < 0 {
		t.Fatalf("missing section elements: label=%d unverified=%d verified=%d", idxLabel, idxUnverified, idxVerified)
	}
	if !(idxLabel < idxUnverified) {
		t.Fatalf("unverified track appears before label: label=%d unverified=%d\n%s", idxLabel, idxUnverified, out)
	}
	if idxVerified > idxLabel {
		t.Fatalf("verified track appears inside the unverified group: verified=%d label=%d\n%s", idxVerified, idxLabel, out)
	}
}

// TestViewNowPlaying_HandlesMpvCrashBanner is mandatory (S-TUI-2):
// a backend crash must surface as a non-fatal banner so the user
// can recover with another keypress.
func TestViewNowPlaying_HandlesMpvCrashBanner(t *testing.T) {
	m := NewModel(audio.NewMockBackend(), nil, nil)
	m.Mode = ModeNowPlaying
	m.LastError = "mpv crashed: audio backend unavailable"

	out := viewNowPlaying(m)
	if !strings.Contains(out, "error") || !strings.Contains(out, "mpv crashed") {
		t.Fatalf("now-playing view does not show crash banner\n--view--\n%s", out)
	}

	// The banner MUST NOT clobber the playback controls.
	for _, want := range []string{"space", "play"} {
		if !strings.Contains(strings.ToLower(out), want) {
			t.Fatalf("crash banner hid controls (%q missing)\n%s", want, out)
		}
	}
}

func TestViewQueue_RendersTracksFromCatalog(t *testing.T) {
	tracks := []catalog.Track{
		{ID: "zzz", Title: "Last", LicenseStatus: catalog.LicenseStatusVerified, DurationSeconds: 30},
		{ID: "aaa", Title: "First", LicenseStatus: catalog.LicenseStatusVerified, DurationSeconds: 30},
	}
	c := &catalog.Catalog{SchemaVersion: catalog.CurrentSchemaVersion, Version: "1", Tracks: tracks}
	m := NewModel(audio.NewMockBackend(), c, nil)
	m.Mode = ModeQueue

	out := viewQueue(m)
	if !strings.Contains(out, "First") || !strings.Contains(out, "Last") {
		t.Fatalf("queue view missing tracks: %s", out)
	}
}
