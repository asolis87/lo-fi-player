// Package catalog defines the on-disk track descriptor, the
// validation rules that gate every track, and the read-only
// filesystem loader. Pure Go stdlib so it can be embedded in any
// audit/CI/credits tool without dragging in audio or config deps.
package catalog

import (
	"errors"
	"fmt"
)

// CurrentSchemaVersion is the schema_version every shipped
// track.json MUST carry. Future versions reject the old shape
// loudly rather than silently parse.
const CurrentSchemaVersion = 1

// License is the closed enum REQ-CAT-2 enforces. Adding a value
// here is a hard product/legal decision.
type License string

const (
	LicenseCC0    License = "CC0"
	LicenseCCBY   License = "CC-BY"
	LicenseCCBYSA License = "CC-BY-SA"
)

func (l License) valid() bool {
	switch l {
	case LicenseCC0, LicenseCCBY, LicenseCCBYSA:
		return true
	}
	return false
}

// LicenseStatus is the verification flag REQ-ATT-3 surfaces under
// "Unverified licenses" until a track is re-verified.
type LicenseStatus string

const (
	LicenseStatusVerified          LicenseStatus = "VERIFIED"
	LicenseStatusNeedsConfirmation LicenseStatus = "NEEDS CONFIRMATION"
)

func (s LicenseStatus) valid() bool {
	switch s {
	case LicenseStatusVerified, LicenseStatusNeedsConfirmation:
		return true
	}
	return false
}

// Track mirrors the spec wire contract (REQ-CAT-* / REQ-ATT-*).
type Track struct {
	SchemaVersion   int           `json:"schema_version"`
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	Artist          string        `json:"artist"`
	License         License       `json:"license"`
	LicenseStatus   LicenseStatus `json:"license_status"`
	SourceURL       string        `json:"source_url"`
	ChecksumSHA256  string        `json:"checksum_sha256"`
	AttributionText string        `json:"attribution_text"`
	DurationSeconds int           `json:"duration_seconds"`
	AudioFilename   string        `json:"audio_filename"`
}

// Catalog is the root document LoadFromDir returns. Tracks are
// sorted by id so consumers iterate stably.
type Catalog struct {
	SchemaVersion int     `json:"schema_version"`
	Version       string  `json:"version"`
	Tracks        []Track `json:"tracks"`
}

// ErrInvalidTrack is wrapped around every Validate() failure so
// callers match the class via errors.Is while the message keeps
// the offending field name.
var ErrInvalidTrack = errors.New("catalog: invalid track")

// Validate enforces schema/license/required-field rules. Checksum
// verification is Verify's job and is intentionally not done here
// so a single bad audio file does not poison a whole catalog load.
func (t *Track) Validate() error {
	if t.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("%w: schema_version=%d, want %d", ErrInvalidTrack, t.SchemaVersion, CurrentSchemaVersion)
	}
	if !t.License.valid() {
		return fmt.Errorf("%w: license=%q not in {CC0, CC-BY, CC-BY-SA}", ErrInvalidTrack, t.License)
	}
	if !t.LicenseStatus.valid() {
		return fmt.Errorf("%w: license_status=%q not in {VERIFIED, NEEDS CONFIRMATION}", ErrInvalidTrack, t.LicenseStatus)
	}
	if t.ID == "" {
		return fmt.Errorf("%w: id is empty", ErrInvalidTrack)
	}
	if t.ChecksumSHA256 == "" {
		return fmt.Errorf("%w: checksum_sha256 is empty", ErrInvalidTrack)
	}
	if t.AudioFilename == "" {
		return fmt.Errorf("%w: audio_filename is empty", ErrInvalidTrack)
	}
	if t.DurationSeconds < 1 {
		return fmt.Errorf("%w: duration_seconds=%d, want >= 1", ErrInvalidTrack, t.DurationSeconds)
	}
	return nil
}