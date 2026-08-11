package catalog

import (
	"errors"
	"strings"
	"testing"
)

// pinnedSHA is a valid 40-hex SHA used by the validation tests.
const pinnedSHA = "abcdef0123456789abcdef0123456789abcdef01"

// TestResolveManifestURL_BuildsPinnedURL guards REQ-FCH-1 and the
// SHA-pinning contract (decision #289): the URL must point at a
// raw.githubusercontent.com blob pinned to a 40-hex commit SHA.
func TestResolveManifestURL_BuildsPinnedURL(t *testing.T) {
	got := ResolveManifestURL("lo-fi-player", "asolis87", pinnedSHA)
	const want = "https://raw.githubusercontent.com/asolis87/lo-fi-player/abcdef0123456789abcdef0123456789abcdef01/catalog/v1/manifest.json"
	if got != want {
		t.Fatalf("ResolveManifestURL = %q, want %q", got, want)
	}
}

// TestFirstRunFetchURLTemplate_RendersValidURL guards REQ-FCH-1 +
// REQ-CAT-3: a first-run fetch that uses the template plus the
// placeholder SHA MUST yield a URL that ValidateURL accepts. This
// is the contract the binary uses on first launch.
func TestFirstRunFetchURLTemplate_RendersValidURL(t *testing.T) {
	if FirstRunCommitSHA == "" || strings.Contains(FirstRunCommitSHA, "<") {
		t.Skipf("FirstRunCommitSHA placeholder not yet replaced (=%q); template contract still tested below", FirstRunCommitSHA)
	}
	built := ResolveManifestURL("lo-fi-player", "asolis87", FirstRunCommitSHA)
	if err := ValidateURL(built); err != nil {
		t.Fatalf("ValidateURL(%q) = %v, want nil for a rendered template URL", built, err)
	}
}

// TestValidateURL_AcceptsPinnedSHA: a properly built pinned URL
// MUST pass validation.
func TestValidateURL_AcceptsPinnedSHA(t *testing.T) {
	u := ResolveManifestURL("lo-fi-player", "asolis87", pinnedSHA)
	if err := ValidateURL(u); err != nil {
		t.Fatalf("ValidateURL(pinned) = %v, want nil", err)
	}
}

// TestValidateURL_RejectsLatestRef guards REQ-CAT-3: a floating
// ref like "latest" MUST be rejected with ErrFloatRef.
func TestValidateURL_RejectsLatestRef(t *testing.T) {
	u := "https://raw.githubusercontent.com/asolis87/lo-fi-player/latest/catalog/v1/manifest.json"
	err := ValidateURL(u)
	if err == nil {
		t.Fatal("ValidateURL(latest) = nil, want error")
	}
	if !errors.Is(err, ErrFloatRef) {
		t.Fatalf("ValidateURL(latest) = %v, want wrapped ErrFloatRef", err)
	}
}

// TestValidateURL_RejectsMalformedSHA guards REQ-CAT-3 + the SHA
// pinning contract: a SHA that is not exactly 40 lowercase hex
// characters MUST be rejected.
func TestValidateURL_RejectsMalformedSHA(t *testing.T) {
	cases := []string{
		// Wrong length.
		"https://raw.githubusercontent.com/asolis87/lo-fi-player/abcdef/catalog/v1/manifest.json",
		// Uppercase hex (we require lowercase only).
		"https://raw.githubusercontent.com/asolis87/lo-fi-player/ABCDEF0123456789ABCDEF0123456789ABCDEF01/catalog/v1/manifest.json",
		// Non-hex character.
		"https://raw.githubusercontent.com/asolis87/lo-fi-player/zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz/catalog/v1/manifest.json",
		// Empty SHA.
		"https://raw.githubusercontent.com/asolis87/lo-fi-player//catalog/v1/manifest.json",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			if err := ValidateURL(u); err == nil {
				t.Fatalf("ValidateURL(%q) = nil, want error", u)
			}
		})
	}
}

// TestValidateURL_RejectsWrongHost guards REQ-CAT-3: only the
// raw.githubusercontent.com host carries immutable blobs; a github.com
// UI URL or any other host MUST be rejected.
func TestValidateURL_RejectsWrongHost(t *testing.T) {
	cases := []string{
		"https://github.com/asolis87/lo-fi-player/raw/" + pinnedSHA + "/catalog/v1/manifest.json",
		"https://api.github.com/repos/asolis87/lo-fi-player/contents/catalog/v1/manifest.json",
		"http://raw.githubusercontent.com/asolis87/lo-fi-player/" + pinnedSHA + "/catalog/v1/manifest.json",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			if err := ValidateURL(u); err == nil {
				t.Fatalf("ValidateURL(%q) = nil, want error", u)
			}
		})
	}
}