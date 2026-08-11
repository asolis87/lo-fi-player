package credits

import (
	"fmt"
	"sort"
	"strings"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// FormatNotice renders the deterministic human-readable catalog attribution block.
func FormatNotice(c *catalog.Catalog) string {
	var b strings.Builder
	b.WriteString("NOTICE: lo-fi-player\n")
	b.WriteString("Catalog version: ")
	b.WriteString(c.Version)
	b.WriteString("\n\n")
	tracks := append([]catalog.Track(nil), c.Tracks...)
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].ID < tracks[j].ID })
	if len(tracks) == 0 {
		b.WriteString("No tracks available.\n")
		return b.String()
	}
	for _, track := range tracks {
		b.WriteString(fmt.Sprintf("Track: %s\nTitle: %s\nArtist: %s\nLicense: %s\nSource: %s\nChecksum: %s\n%s\n\n", track.ID, track.Title, track.Artist, track.License, track.SourceURL, track.ChecksumSHA256, track.AttributionText))
	}
	return b.String()
}
