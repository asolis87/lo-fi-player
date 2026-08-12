// Package license guards the repository-level LICENSE file and
// parses per-track LICENSE.txt / verification reports that the
// catalog seed audit (slice #2 / spec #287 / PR-B) relies on.
//
// ParseLicenseFile classifies a per-track LICENSE.txt into one of
// the closed enum {CC0, CC-BY, CC-BY-SA} (REQ-CAT-2). VerifyReport
// reads a verification report Markdown at
// catalog/v1/verification/<id>.md and validates its shape.
//
// Per spec #287, the parser does NOT verify signatures
// cryptographically; the report is a human-checked contract and
// license_status is the loader's signal of trust.
package license

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// ErrLicenseNotFound wraps os.ReadFile ENOENT so callers match the
// class with errors.Is without leaking os.PathError details.
var ErrLicenseNotFound = errors.New("license: file not found")

// ErrLicenseUnrecognized wraps classification failures. The body
// was read but no CC marker matched.
var ErrLicenseUnrecognized = errors.New("license: unrecognized type")

// ErrInvalidReport wraps every Validate failure so callers match
// the class with errors.Is while the message keeps the offending
// field name.
var ErrInvalidReport = errors.New("license: invalid report")

// sha256Hex matches exactly 64 lowercase hex chars. Per spec the
// re-hashed SHA-256 is canonical lowercase only — uppercase is
// rejected so audit tooling can normalize consistently.
var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// License is the structured result of ParseLicenseFile. Type
// matches the closed enum in internal/catalog (REQ-CAT-2) so a
// downstream loader can compare string-for-string.
type License struct {
	Type          string
	CanonicalName string
	SourcePath    string
	Body          string
}

// Report mirrors the fields the spec #287 / catalog-seed Verification
// Report Format mandates. SignedAt is parsed from the operator's
// Date line; OperatorSignature is the concatenated "Name <email>
// Date" shape audit tooling expects.
type Report struct {
	TrackID           string
	LicenseURL        string
	HTMLSnapshotPath  string
	RehashedSHA256    string
	OperatorSignature string
	SignedAt          time.Time
}

// ParseLicenseFile reads path and classifies the LICENSE.txt body
// into the closed {CC0, CC-BY, CC-BY-SA} enum. Returns
// ErrLicenseNotFound if the file is missing, ErrLicenseUnrecognized
// if no CC marker matched.
func ParseLicenseFile(path string) (*License, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrLicenseNotFound, path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	body := string(data)
	typ, canonical, ok := detectLicenseType(body)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrLicenseUnrecognized, path)
	}
	return &License{
		Type:          typ,
		CanonicalName: canonical,
		SourcePath:    path,
		Body:          body,
	}, nil
}

// VerifyReport reads the verification report Markdown at path,
// parses each section, runs Validate, and returns the populated
// Report. A parse error or validation failure is returned wrapped
// in ErrInvalidReport so callers match the class via errors.Is.
func VerifyReport(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrInvalidReport, path, err)
	}
	r, err := parseReportMarkdown(string(data))
	if err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// Validate enforces the field-shape rules. The slice #2 catalog
// loader does NOT crypto-verify the signature; this validator only
// checks that the fields exist and have the documented shape so a
// downstream loader can rely on the contract.
func (r *Report) Validate() error {
	if r == nil {
		return fmt.Errorf("%w: report is nil", ErrInvalidReport)
	}
	if strings.TrimSpace(r.TrackID) == "" {
		return fmt.Errorf("%w: track_id is empty", ErrInvalidReport)
	}
	if strings.TrimSpace(r.LicenseURL) == "" {
		return fmt.Errorf("%w: license_url is empty", ErrInvalidReport)
	}
	if u, err := url.Parse(r.LicenseURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%w: license_url=%q is not a valid URL", ErrInvalidReport, r.LicenseURL)
	}
	if strings.TrimSpace(r.HTMLSnapshotPath) == "" {
		return fmt.Errorf("%w: html_snapshot_path is empty", ErrInvalidReport)
	}
	if !sha256Hex.MatchString(r.RehashedSHA256) {
		return fmt.Errorf("%w: rehashed_sha256=%q does not match ^[0-9a-f]{64}$", ErrInvalidReport, r.RehashedSHA256)
	}
	if strings.TrimSpace(r.OperatorSignature) == "" {
		return fmt.Errorf("%w: operator_signature is empty", ErrInvalidReport)
	}
	return nil
}

// detectLicenseType returns (type, canonicalName, matched) using a
// priority-ordered marker scan. Order matters: CC-BY-SA's
// "Attribution-ShareAlike" contains "Attribution" as a substring,
// so we must check ShareAlike first to avoid misclassifying
// CC-BY-SA as CC-BY.
func detectLicenseType(body string) (string, string, bool) {
	switch {
	case strings.Contains(body, "CC0 1.0 Universal"):
		return "CC0", "Creative Commons CC0 1.0 Universal", true
	case strings.Contains(body, "Attribution-ShareAlike"):
		return "CC-BY-SA", "Creative Commons Attribution-ShareAlike 4.0", true
	case strings.Contains(body, "Creative Commons Attribution"):
		return "CC-BY", "Creative Commons Attribution 4.0", true
	}
	return "", "", false
}

// parseReportMarkdown scans the report line-by-line. Each section
// is keyed by its Markdown "## Heading"; field values come from the
// bullet lines "- Key: Value" beneath. The parser is intentionally
// permissive about whitespace so a hand-edited report still parses.
func parseReportMarkdown(body string) (*Report, error) {
	r := &Report{}
	var sigName, sigEmail, sigDate string
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "# "):
			r.TrackID = strings.TrimSpace(strings.TrimPrefix(line, "# Verification Report:"))
		case strings.HasPrefix(line, "## "):
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		case strings.HasPrefix(line, "- "):
			kv := strings.TrimPrefix(line, "- ")
			key, val, ok := splitKeyValue(kv)
			if !ok {
				continue
			}
			switch section {
			case "License Claim":
				switch key {
				case "License URL":
					r.LicenseURL = val
				case "HTML snapshot":
					r.HTMLSnapshotPath = val
				}
			case "Audio Integrity":
				if key == "Re-hashed SHA-256" {
					r.RehashedSHA256 = val
				}
			case "Operator Signature":
				switch key {
				case "Name":
					sigName = val
				case "Email":
					sigEmail = val
				case "Date":
					sigDate = val
					if t, ok := parseReportDate(val); ok {
						r.SignedAt = t
					}
				}
				r.OperatorSignature = formatSignature(sigName, sigEmail, sigDate)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: scan report: %v", ErrInvalidReport, err)
	}
	return r, nil
}

// splitKeyValue splits the first ": " separator. Returns ok=false
// for lines without a separator so unknown bullets are ignored.
func splitKeyValue(s string) (string, string, bool) {
	idx := strings.Index(s, ": ")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+2:]), true
}

// formatSignature assembles the canonical "Name <email> Date"
// shape. Empty parts are preserved so a fully blank signature
// fails Validate with "operator_signature is empty" rather than
// silently passing.
func formatSignature(name, email, date string) string {
	if name == "" && email == "" && date == "" {
		return ""
	}
	return fmt.Sprintf("%s <%s> %s", name, email, date)
}

// parseReportDate accepts the ISO 8601 date form the spec uses
// ("2026-08-11"). Other forms return ok=false so the test for
// invalid signatures can still distinguish missing fields from
// malformed ones.
func parseReportDate(s string) (time.Time, bool) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}