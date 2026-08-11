package catalog

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// FirstRunFetchURLTemplate is the printf-style template used by
// the binary on first launch to assemble the immutable manifest
// URL. The three slots are owner, repo, and the 40-hex commit SHA
// baked at build time (decision #289). NEVER substitute a branch,
// tag, or "latest" ref here — REQ-CAT-3 forbids floating refs.
const FirstRunFetchURLTemplate = "https://raw.githubusercontent.com/%s/%s/%s/catalog/v1/manifest.json"

// FirstRunCommitSHA is the commit SHA baked into the binary that
// pins the first published manifest. It MUST stay a 40-char
// lowercase hex string; if it ever points at a wrong commit, the
// only remediation is a new release publishing the corrected SHA
// (decision #289). The placeholder below is replaced by the
// release pipeline when the catalog/v1/ seed lands in PR #10.
//
// Declared as a var (not const) so PR #9's runSync tests can
// override the value with a valid SHA without spinning up an
// httptest server just to satisfy URL validation. Production code
// never reassigns this; the release pipeline writes the real SHA
// into this same symbol.
var FirstRunCommitSHA = "<PLACEHOLDER_SHA>"

// ErrFloatRef is wrapped around every ValidateURL failure whose
// root cause is a non-immutable ref (branch, tag, "latest") or a
// SHA that is not 40 lowercase hex chars. Callers match with
// errors.Is so "this URL would silently change" surfaces as one
// actionable class.
var ErrFloatRef = errors.New("catalog: floating ref or malformed SHA")

// pinnedHost is the only host REQ-CAT-3 permits: raw.githubusercontent.com
// serves immutable blob bytes keyed by commit SHA. UI hosts and
// the API host can rewrite content and are rejected.
const pinnedHost = "raw.githubusercontent.com"

// pinnedSHARe enforces "exactly 40 lowercase hex chars". Go's
// regexp is heavy for the catalog hot path but this function runs
// at most once per fetch, so clarity beats micro-optimisation.
var pinnedSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ResolveManifestURL renders FirstRunFetchURLTemplate with the
// provided owner, repo, and commit SHA. It performs no validation
// — callers needing the URL accepted by ValidateURL must pass a
// properly shaped SHA (see pinnedSHARe). Format is fixed so byte
// identity is reproducible.
func ResolveManifestURL(repo, owner, commitSHA string) string {
	return fmt.Sprintf(FirstRunFetchURLTemplate, owner, repo, commitSHA)
}

// ValidateURL enforces REQ-CAT-3 / REQ-FCH-1 / decision #289: the
// URL MUST
//   - parse,
//   - use https scheme,
//   - point at raw.githubusercontent.com,
//   - carry /<owner>/<repo>/<40-hex-sha>/...,
//   - reference a non-empty catalog path.
//
// Anything else (floating ref, malformed SHA, UI host, http) is
// wrapped in ErrFloatRef so callers can match on one sentinel.
func ValidateURL(u string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("%w: parse %s: %v", ErrFloatRef, u, err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%w: scheme=%q, want https", ErrFloatRef, parsed.Scheme)
	}
	if !strings.EqualFold(parsed.Host, pinnedHost) {
		return fmt.Errorf("%w: host=%q, want %q", ErrFloatRef, parsed.Host, pinnedHost)
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) < 4 {
		return fmt.Errorf("%w: path=%q has %d segments, want >=4", ErrFloatRef, parsed.Path, len(parts))
	}
	sha := parts[2]
	if !pinnedSHARe.MatchString(sha) {
		return fmt.Errorf("%w: sha=%q is not 40 lowercase hex chars", ErrFloatRef, sha)
	}
	return nil
}