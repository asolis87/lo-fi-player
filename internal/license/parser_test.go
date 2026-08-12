// Package license guards the repository-level LICENSE file and parses
// per-track LICENSE.txt / verification reports that the catalog seed
// audit (slice #2 / spec #287 / PR-B) relies on.
package license

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixturePath resolves a fixture path relative to this test file's
// directory. Using testdata/ keeps fixtures out of the package's
// compiled surface and lets the parser tests live alongside the
// fixtures they exercise.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

// TestParseLicenseFile_ReadsCC0 covers the CC0 1.0 Universal marker.
// The fixture's marker line "Creative Commons CC0 1.0 Universal" is
// the canonical phrase the parser matches to Type "CC0".
func TestParseLicenseFile_ReadsCC0(t *testing.T) {
	got, err := ParseLicenseFile(fixturePath(t, "cc0.LICENSE.txt"))
	if err != nil {
		t.Fatalf("ParseLicenseFile(CC0) = %v, want nil", err)
	}
	if got.Type != "CC0" {
		t.Errorf("Type = %q, want %q", got.Type, "CC0")
	}
	if !strings.Contains(got.CanonicalName, "CC0") {
		t.Errorf("CanonicalName = %q, want it to mention CC0", got.CanonicalName)
	}
	if got.SourcePath == "" {
		t.Error("SourcePath is empty, want the path of the read file")
	}
	if got.Body == "" {
		t.Error("Body is empty, want the file's full content")
	}
}

// TestParseLicenseFile_ReadsCCBY covers the CC-BY 4.0 marker. The
// fixture contains "Creative Commons Attribution 4.0 International"
// without the ShareAlike clause, so the parser must classify it as
// CC-BY (not CC-BY-SA — that requires ShareAlike explicitly).
func TestParseLicenseFile_ReadsCCBY(t *testing.T) {
	got, err := ParseLicenseFile(fixturePath(t, "cc-by.LICENSE.txt"))
	if err != nil {
		t.Fatalf("ParseLicenseFile(CC-BY) = %v, want nil", err)
	}
	if got.Type != "CC-BY" {
		t.Errorf("Type = %q, want %q", got.Type, "CC-BY")
	}
}

// TestParseLicenseFile_ReadsCCBYSA covers the CC-BY-SA 4.0 marker.
// Priority matters: the parser MUST detect ShareAlike before plain
// Attribution, because "Attribution-ShareAlike" contains
// "Attribution" as a substring.
func TestParseLicenseFile_ReadsCCBYSA(t *testing.T) {
	got, err := ParseLicenseFile(fixturePath(t, "cc-by-sa.LICENSE.txt"))
	if err != nil {
		t.Fatalf("ParseLicenseFile(CC-BY-SA) = %v, want nil", err)
	}
	if got.Type != "CC-BY-SA" {
		t.Errorf("Type = %q, want %q", got.Type, "CC-BY-SA")
	}
}

// TestParseLicenseFile_NotFound asserts the sentinel error wraps
// ErrLicenseNotFound so callers match the class via errors.Is.
func TestParseLicenseFile_NotFound(t *testing.T) {
	_, err := ParseLicenseFile(filepath.Join("testdata", "does-not-exist.LICENSE.txt"))
	if err == nil {
		t.Fatal("ParseLicenseFile(missing) = nil, want error")
	}
	if !errors.Is(err, ErrLicenseNotFound) {
		t.Fatalf("error %v does not wrap ErrLicenseNotFound", err)
	}
}

// TestParseLicenseFile_Unrecognized asserts the sentinel error wraps
// ErrLicenseUnrecognized for files that match none of the CC
// markers. The body is arbitrary prose — no CC phrase at all.
func TestParseLicenseFile_Unrecognized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.LICENSE.txt")
	if err := writeFile(path, "Lorem ipsum dolor sit amet. No Creative Commons here.\n"); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	_, err := ParseLicenseFile(path)
	if err == nil {
		t.Fatal("ParseLicenseFile(garbage) = nil, want error")
	}
	if !errors.Is(err, ErrLicenseUnrecognized) {
		t.Fatalf("error %v does not wrap ErrLicenseUnrecognized", err)
	}
}

// TestVerifyReport_Valid checks the happy path: a Markdown report
// with all required sections parses into a populated Report whose
// Validate() passes.
func TestVerifyReport_Valid(t *testing.T) {
	got, err := VerifyReport(fixturePath(t, "valid.report.md"))
	if err != nil {
		t.Fatalf("VerifyReport(valid) = %v, want nil", err)
	}
	if got.TrackID != "track-sample-001" {
		t.Errorf("TrackID = %q, want %q", got.TrackID, "track-sample-001")
	}
	if got.LicenseURL != "https://creativecommons.org/licenses/by/4.0/" {
		t.Errorf("LicenseURL = %q", got.LicenseURL)
	}
	if got.HTMLSnapshotPath != "docs/snapshots/track-sample-001-license.html" {
		t.Errorf("HTMLSnapshotPath = %q", got.HTMLSnapshotPath)
	}
	if got.RehashedSHA256 != "ebcd29e89f4d10b6e1bce7d8d4b6a4f3a3c2d1e0f9b8c7a6d5e4f3c2b1a09e8f" {
		t.Errorf("RehashedSHA256 = %q", got.RehashedSHA256)
	}
	wantSig := "A. Operator <ops@example.com> 2026-08-11"
	if got.OperatorSignature != wantSig {
		t.Errorf("OperatorSignature = %q, want %q", got.OperatorSignature, wantSig)
	}
	wantTime := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	if !got.SignedAt.Equal(wantTime) {
		t.Errorf("SignedAt = %v, want %v", got.SignedAt, wantTime)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestVerifyReport_MissingField covers a report that omits
// License URL. Validate() must reject it; the parser surfaces the
// failure either via parse-time or via Validate().
func TestVerifyReport_MissingField(t *testing.T) {
	r, err := VerifyReport(fixturePath(t, "missing-field.report.md"))
	if err == nil && r.Validate() == nil {
		t.Fatal("missing-field report accepted, want error")
	}
	if err == nil && r != nil {
		if vErr := r.Validate(); vErr == nil {
			t.Fatal("Validate() = nil, want error naming LicenseURL")
		}
	}
}

// TestVerifyReport_BadSHA256 covers a report whose Re-hashed SHA-256
// field is not 64 lowercase hex chars. The regex check on Validate()
// must fire.
func TestVerifyReport_BadSHA256(t *testing.T) {
	r, err := VerifyReport(fixturePath(t, "bad-sha.report.md"))
	if err == nil {
		if vErr := r.Validate(); vErr == nil {
			t.Fatal("Validate() = nil, want error naming SHA-256")
		}
	}
}

// TestVerifyReport_BadSignatureFormat covers a report whose
// operator signature fields are all blank. Validate() must reject
// OperatorSignature == "".
func TestVerifyReport_BadSignatureFormat(t *testing.T) {
	r, err := VerifyReport(fixturePath(t, "bad-sig.report.md"))
	if err == nil {
		if vErr := r.Validate(); vErr == nil {
			t.Fatal("Validate() = nil, want error naming operator_signature")
		}
	}
}

// writeFile is a tiny helper to seed TempDir fixtures without
// pulling in os.WriteFile at every call site.
func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

// catalogPlaceholderPath resolves the on-disk placeholder report
// the slice #2 seed ships. It lives under catalog/v1/verification/
// (not testdata/) so the parser test exercises a real path the
// shipped catalog will carry.
func catalogPlaceholderPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "catalog", "v1", "verification", "track-sample-001.md")
}

// TestVerifyReport_AcceptsPlaceholder is the regression guard for
// the placeholder contract from spec #287: a track whose audio
// bytes have not yet been curated MUST still carry a parseable
// verification report so the catalog loader can hand a stable
// shape to future tooling. The 64-zero SHA-256 is intentionally
// accepted because the regex `^[0-9a-f]{64}$` matches it — it
// is the SHA of the empty input, which the loader already rejects
// at checksum-verification time (PR-D #3.2). The report parser
// stays orthogonal to the audio checksum so each layer can fail
// independently with its own error class.
func TestVerifyReport_AcceptsPlaceholder(t *testing.T) {
	got, err := VerifyReport(catalogPlaceholderPath(t))
	if err != nil {
		t.Fatalf("VerifyReport(placeholder) = %v, want nil", err)
	}
	if got.TrackID != "track-sample-001" {
		t.Errorf("TrackID = %q, want %q", got.TrackID, "track-sample-001")
	}
	if got.LicenseURL != "https://creativecommons.org/licenses/by/4.0/" {
		t.Errorf("LicenseURL = %q", got.LicenseURL)
	}
	if got.RehashedSHA256 != "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Errorf("RehashedSHA256 = %q, want 64 zeros (placeholder contract from spec #287)", got.RehashedSHA256)
	}
}