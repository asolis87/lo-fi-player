package catalog

import (
	"strings"
	"testing"
)

// validTrack returns a Track that passes Validate() so individual
// tests only mutate the field they care about.
func validTrack() Track {
	return Track{
		SchemaVersion:   CurrentSchemaVersion,
		ID:              "track-drizzle",
		Title:           "Slow Rain on a Tin Roof",
		Artist:          "Anonymous",
		License:         LicenseCCBY,
		LicenseStatus:   LicenseStatusVerified,
		SourceURL:       "https://archive.org/details/ia-drizzle",
		ChecksumSHA256:  "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12",
		AttributionText: `"Slow Rain on a Tin Roof" by Anonymous, licensed CC-BY-4.0. Source: archive.org/ia-drizzle`,
		DurationSeconds: 217,
	}
}

// TestTrack_Validate_AcceptsAllowedLicenses guards REQ-CAT-2.
func TestTrack_Validate_AcceptsAllowedLicenses(t *testing.T) {
	for _, lic := range []License{LicenseCC0, LicenseCCBY, LicenseCCBYSA} {
		t.Run(string(lic), func(t *testing.T) {
			tr := validTrack()
			tr.License = lic
			if err := tr.Validate(); err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", lic, err)
			}
		})
	}
}

// TestTrack_Validate_RejectsCCBYNC locks down REQ-CAT-2.
func TestTrack_Validate_RejectsCCBYNC(t *testing.T) {
	tr := validTrack()
	tr.License = "CC-BY-NC"
	err := tr.Validate()
	if err == nil {
		t.Fatal("Validate(CC-BY-NC) = nil, want error")
	}
	if !strings.Contains(err.Error(), "license") {
		t.Fatalf("error %q lacks license context", err.Error())
	}
}

// TestTrack_Validate_RejectsUnknownSchemaVersion covers schema
// migration: future or corrupted shapes MUST fail loud.
func TestTrack_Validate_RejectsUnknownSchemaVersion(t *testing.T) {
	tr := validTrack()
	tr.SchemaVersion = CurrentSchemaVersion + 99
	err := tr.Validate()
	if err == nil {
		t.Fatal("Validate(unknown schema_version) = nil, want error")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("error %q lacks schema_version context", err.Error())
	}
}