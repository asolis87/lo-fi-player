package tui

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// viewNowPlaying is the default REQ-TUI-1 landing view. It shows
// the playback controls, the navigation legend, and the non-fatal
// crash banner required by S-TUI-2.
func viewNowPlaying(m Model) string {
	var b strings.Builder
	b.WriteString("lo-fi player - Now Playing\n\n")
	b.WriteString("[space] play/pause   [n] next   [b] prev   [+/-] volume\n")
	b.WriteString("[a] attribution   [c] catalog   [q] queue\n")
	if m.LastError != "" {
		b.WriteString("\n! error: ")
		b.WriteString(m.LastError)
		b.WriteString("\n")
	}
	return b.String()
}

// viewQueue shows every catalog track as the slice-#1 queue.
// Navigation legend matches the prompt; rows are formatted with
// text/tabwriter so the TUI stays free of external layout deps.
func viewQueue(m Model) string {
	var b strings.Builder
	b.WriteString("lo-fi player - Queue\n\n")
	b.WriteString("[p] now-playing   [c] catalog   [a] attribution   [esc] back\n\n")
	b.WriteString(formatTrackTable(m.Catalog))
	return b.String()
}

// viewCatalog splits the catalog into verified tracks and an
// "Unverified licenses" group for tracks with
// license_status = NEEDS CONFIRMATION (REQ-ATT-3 / S-ATT-2). The
// label is rendered BEFORE the unverified rows so reviewers see
// the grouping at a glance.
func viewCatalog(m Model) string {
	var b strings.Builder
	version := "n/a"
	if m.Catalog != nil {
		version = m.Catalog.Version
	}
	fmt.Fprintf(&b, "lo-fi player - Catalog (v%s)\n\n", version)
	b.WriteString("[p] now-playing   [q] queue   [a] attribution   [esc] back\n\n")

	if m.Catalog == nil || len(m.Catalog.Tracks) == 0 {
		b.WriteString("catalog is empty.\n")
		return b.String()
	}

	verified, unverified := splitByLicenseStatus(m.Catalog.Tracks)
	b.WriteString(formatTrackTable(&catalog.Catalog{Tracks: verified}))
	if len(unverified) > 0 {
		b.WriteString("\nUnverified licenses\n")
		b.WriteString(formatTrackTable(&catalog.Catalog{Tracks: unverified}))
	}
	return b.String()
}

// viewAttribution renders the five REQ-ATT-2 fields (title,
// artist, license, source URL, checksum) for the selected track.
// SelectedIdx is clamped to the catalog range so a stale index
// never panics.
func viewAttribution(m Model) string {
	var b strings.Builder
	b.WriteString("lo-fi player - Attribution\n\n")
	b.WriteString("[esc] back   [p] now-playing   [q] queue\n\n")

	if m.Catalog == nil || len(m.Catalog.Tracks) == 0 {
		b.WriteString("no track available - attribution requires a synced catalog.\n")
		return b.String()
	}

	idx := m.SelectedIdx
	if idx < 0 || idx >= len(m.Catalog.Tracks) {
		idx = 0
	}
	tr := m.Catalog.Tracks[idx]

	fmt.Fprintf(&b, "Title   : %s\n", tr.Title)
	fmt.Fprintf(&b, "Artist  : %s\n", tr.Artist)
	fmt.Fprintf(&b, "License : %s   [%s]\n", tr.License, tr.LicenseStatus)
	fmt.Fprintf(&b, "Source  : %s\n", tr.SourceURL)
	fmt.Fprintf(&b, "Checksum: sha256:%s\n", tr.ChecksumSHA256)
	b.WriteString("\n")
	b.WriteString(tr.AttributionText)
	b.WriteString("\n")
	return b.String()
}

// formatTrackTable prints ID, title, artist aligned with
// text/tabwriter. A nil or empty catalog yields a placeholder line
// so callers always have something to append.
func formatTrackTable(c *catalog.Catalog) string {
	if c == nil || len(c.Tracks) == 0 {
		return "no tracks loaded.\n"
	}
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tARTIST")
	for _, tr := range c.Tracks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", tr.ID, tr.Title, tr.Artist)
	}
	_ = tw.Flush()
	return b.String()
}

// splitByLicenseStatus sorts and partitions the catalog into
// verified (VERIFIED) and unverified (NEEDS CONFIRMATION) tracks.
// The unverified slice is sorted by id so the rendering order is
// stable across renders.
func splitByLicenseStatus(in []catalog.Track) (verified, unverified []catalog.Track) {
	for _, tr := range in {
		if tr.LicenseStatus == catalog.LicenseStatusNeedsConfirmation {
			unverified = append(unverified, tr)
		} else {
			verified = append(verified, tr)
		}
	}
	sort.Slice(verified, func(i, j int) bool { return verified[i].ID < verified[j].ID })
	sort.Slice(unverified, func(i, j int) bool { return unverified[i].ID < unverified[j].ID })
	return verified, unverified
}
