package credits

import (
	"strings"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

func noticeCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		SchemaVersion: catalog.CurrentSchemaVersion,
		Version:       "2026.08",
		Tracks: []catalog.Track{
			{ID: "z-track", Title: "Zed", Artist: "Zed Artist", License: catalog.LicenseCC0, SourceURL: "https://example.test/z", ChecksumSHA256: "zzz", AttributionText: "Zed attribution literal"},
			{ID: "a-track", Title: "Alpha", Artist: "Alpha Artist", License: catalog.LicenseCCBY, SourceURL: "https://example.test/a", ChecksumSHA256: "aaa", AttributionText: "Alpha attribution literal"},
		},
	}
}

func TestFormatNotice_SortsByID(t *testing.T) {
	got := FormatNotice(noticeCatalog())
	if strings.Index(got, "a-track") > strings.Index(got, "z-track") {
		t.Fatalf("tracks are not sorted by id: %q", got)
	}
}

func TestFormatNotice_IncludesAttributionLine(t *testing.T) {
	got := FormatNotice(noticeCatalog())
	if !strings.Contains(got, "Alpha attribution literal") {
		t.Fatalf("notice missing literal attribution line: %q", got)
	}
}

func TestFormatNotice_HandlesEmptyCatalog(t *testing.T) {
	got := FormatNotice(&catalog.Catalog{SchemaVersion: catalog.CurrentSchemaVersion, Version: "1"})
	want := "NOTICE: lo-fi-player\nCatalog version: 1\n\nNo tracks available.\n"
	if got != want {
		t.Fatalf("empty notice = %q, want %q", got, want)
	}
}

func TestFormatNotice_Golden(t *testing.T) {
	want := "NOTICE: lo-fi-player\nCatalog version: 2026.08\n\nTrack: a-track\nTitle: Alpha\nArtist: Alpha Artist\nLicense: CC-BY\nSource: https://example.test/a\nChecksum: aaa\nAlpha attribution literal\n\nTrack: z-track\nTitle: Zed\nArtist: Zed Artist\nLicense: CC0\nSource: https://example.test/z\nChecksum: zzz\nZed attribution literal\n\n"

	if got := FormatNotice(noticeCatalog()); got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}
