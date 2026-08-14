// The Syncer is the catalog package's transactional fetch +
// apply engine. PR-R1a iterates the manifest to download every
// per-track asset into a staging directory with streaming SHA-256
// and promotes staging → v1 only when all verifications pass.
// Promotion atomicity / failure isolation is PR-R1b; per-asset
// progress is PR-R2.
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	// manifestFileName is the cache-resident file Apply produces.
	// Atomic staging writes a sibling named ".tmp-<pid>-*" and
	// renames it over this target in one os.Rename call.
	manifestFileName = "manifest.json"
)

// maxManifestBytes lives in limits.go (PR-2B) — unica fuente
// de verdad para el limite de manifest.

// ErrOffline is wrapped around every Fetch / Sync failure whose
// root cause is the network being unreachable, the remote
// returning a transport-level error, or the server returning a
// non-2xx status. Callers match with errors.Is so they can render
// a recoverable "no network" message and bail without touching
// the cache (S-CAT-2).
var ErrOffline = errors.New("catalog: network offline")

// HTTPStatusError is the typed error Fetch returns when the
// server replies with a non-2xx status. Wraps ErrOffline so
// errors.Is(err, ErrOffline) keeps working; Transient lets the
// retry predicate distinguish 404 (permanent) from 503 (transient).
type HTTPStatusError struct {
	StatusCode int
	URL        string
	Transient  bool
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("GET %s: status %d", e.URL, e.StatusCode)
}

func (e *HTTPStatusError) Unwrap() error { return ErrOffline }

// HTTPClient is the minimal subset of *http.Client the Syncer
// needs; injecting an interface lets tests swap in
// httptest.Server's client or a failing transport without DNS
// or filesystem effects.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Syncer fetches the SHA-pinned manifest over HTTPS and applies
// it atomically to the local cache. HTTPClient is injectable so
// tests can simulate offline behaviour; CacheDir is the directory
// Apply will populate with the target manifest file. Sleeper is
// optional (PR-6): nil falls back to RealSleeper. Progress is
// optional (PR-6B): nil falls back to NullProgressReporter.
type Syncer struct {
	HTTPClient HTTPClient
	CacheDir   string
	Sleeper    Sleeper
	Progress   ProgressReporter
}

// NewSyncer returns a Syncer backed by http.DefaultClient and the
// provided cache directory. cmd/lofi resolves the cache path from
// $XDG_CACHE_HOME so this constructor stays free of env access.
func NewSyncer(cacheDir string) *Syncer {
	return &Syncer{
		HTTPClient: http.DefaultClient,
		CacheDir:   cacheDir,
	}
}

// Fetch downloads manifestURL via the injected HTTPClient. The
// caller's ctx controls timeout (http.NewRequestWithContext
// cancels the in-flight request). URL MUST pass ValidateURL or
// Fetch fails fast with ErrFloatRef before the network is
// touched; transport failures wrap ErrOffline with the URL in the
// message; parse failures return an error carrying the URL too.
// Content-Length, when reported, is checked up front against
// maxManifestBytes so a runaway server is rejected before the
// body is buffered.
func (s *Syncer) Fetch(ctx context.Context, manifestURL string) (*Catalog, error) {
	if err := ValidateURL(manifestURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request for %s: %v", ErrOffline, manifestURL, err)
	}
	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: GET %s: %v", ErrOffline, manifestURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, URL: manifestURL, Transient: isTransient(resp.StatusCode)}
	}
	if resp.ContentLength > maxManifestBytes {
		return nil, fmt.Errorf("catalog: manifest %s reports Content-Length=%d, max %d", manifestURL, resp.ContentLength, maxManifestBytes)
	}
	body := http.MaxBytesReader(nil, resp.Body, maxManifestBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrOffline, manifestURL, err)
	}
	var cat Catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return nil, fmt.Errorf("catalog: parse manifest %s: %w", manifestURL, err)
	}
	return &cat, nil
}

// Apply writes c to <CacheDir>/manifest.json atomically: stage
// into a temp file in the same directory, fsync, then os.Rename
// over the target. On any error before the rename succeeds the
// staging file is removed so a partial manifest can never poison
// the cache (S-CAT-2). Target and staging both use 0o600 perms so
// the rename does not relax file mode.
func (s *Syncer) Apply(c *Catalog) error {
	if c == nil {
		return errors.New("catalog: Apply called with nil catalog")
	}
	if err := os.MkdirAll(s.CacheDir, 0o700); err != nil {
		return fmt.Errorf("catalog: mkdir %s: %w", s.CacheDir, err)
	}
	stage, err := os.CreateTemp(s.CacheDir, fmt.Sprintf(".tmp-%d-*", os.Getpid()))
	if err != nil {
		return fmt.Errorf("catalog: create staging file in %s: %w", s.CacheDir, err)
	}
	stagePath := stage.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(stagePath)
		}
	}()
	if err := json.NewEncoder(stage).Encode(c); err != nil {
		return fmt.Errorf("catalog: encode manifest: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("catalog: fsync staging: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("catalog: close staging: %w", err)
	}
	target := filepath.Join(s.CacheDir, manifestFileName)
	if err := os.Rename(stagePath, target); err != nil {
		return fmt.Errorf("catalog: rename staging to %s: %w", target, err)
	}
	renamed = true
	return nil
}

// Sync is the high-level entrypoint (R-R1a). It fetches the
// pinned manifest, creates a sibling staging directory, writes
// the parsed manifest there, iterates the declared tracks and
// downloads track.json/audio.mp3/LICENSE.txt per track into
// staging/<id>/<filename> with streaming SHA-256, then promotes
// staging → v1 atomically via Promote (PR-3). PR-6 wraps Fetch
// in 3-attempt retry; PR-R1a wraps each per-asset download in
// the same primitive. PR-6B wires ProgressFetch + ProgressApply
// (no per-asset events; PR-R2 will revisit). Network failures
// leave the cache untouched (S-CAT-2); callers match ErrOffline
// with errors.Is.
func (s *Syncer) Sync(ctx context.Context, manifestURL string) error {
	prog := s.progressReporter()

	prog.Report(ProgressEvent{Phase: ProgressFetch})
	var cat *Catalog
	err := Retry(ctx, s.Sleeper, IsTransientHTTP, func(ctx context.Context) error {
		c, ferr := s.Fetch(ctx, manifestURL)
		if ferr != nil {
			return ferr
		}
		cat = c
		return nil
	})
	if err != nil {
		prog.Report(ProgressEvent{Phase: ProgressFetch, Done: true, Err: err})
		return err
	}
	prog.Report(ProgressEvent{Phase: ProgressFetch, Done: true})

	parsed, perr := url.Parse(manifestURL)
	if perr != nil {
		return fmt.Errorf("catalog: parse %s: %w", manifestURL, perr)
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) < 3 {
		return fmt.Errorf("catalog: url path=%q has %d segments, want >=3", parsed.Path, len(parts))
	}
	base := fmt.Sprintf("%s://%s/%s/%s", parsed.Scheme, parsed.Host, parts[0], parts[1])
	commitSHA := parts[2]
	if !pinnedSHARe.MatchString(commitSHA) {
		return fmt.Errorf("%w: sha=%q is not 40 lowercase hex chars", ErrFloatRef, commitSHA)
	}
	cacheRoot := filepath.Dir(s.CacheDir)
	stagingDir, err := Stage(cacheRoot, fmt.Sprintf("r1a-%d-%d", os.Getpid(), syncNonceCounter))
	if err != nil {
		return fmt.Errorf("catalog: stage %s: %w", cacheRoot, err)
	}
	syncNonceCounter++
	committed := false
	defer func() {
		if !committed {
			_ = Cleanup(stagingDir)
		}
	}()

	target := filepath.Join(stagingDir, manifestFileName)
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return fmt.Errorf("catalog: marshal manifest: %w", err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("catalog: write %s: %w", target, err)
	}

	// Each per-asset download gets its own 3-attempt retry budget so
	// a transient 5xx on track-002 does not poison track-003.
	type asset struct {
		name string
		max  int64
		sha  string
	}
	audioMax, _ := maxBytesFor("audio.mp3")
	trackMax, _ := maxBytesFor("track.json")
	licenseMax, _ := maxBytesFor("LICENSE.txt")
	for i := range cat.Tracks {
		tr := &cat.Tracks[i]
		for _, a := range []asset{
			{"track.json", trackMax, ""},
			{"audio.mp3", audioMax, tr.ChecksumSHA256},
			{"LICENSE.txt", licenseMax, ""},
		} {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := Retry(ctx, s.Sleeper, IsTransientHTTP, func(ctx context.Context) error {
				_, derr := s.downloadAsset(ctx, base, commitSHA, tr.ID, a.name, a.max, a.sha, stagingDir)
				return derr
			}); err != nil {
				return fmt.Errorf("catalog: download %s/%s: %w", tr.ID, a.name, err)
			}
		}
	}

	prog.Report(ProgressEvent{Phase: ProgressApply})
	if perr := Promote(cacheRoot, stagingDir, nil); perr != nil {
		prog.Report(ProgressEvent{Phase: ProgressApply, Done: true, Err: perr})
		return perr
	}
	committed = true
	prog.Report(ProgressEvent{Phase: ProgressApply, Done: true})
	return nil
}

// syncNonceCounter tags each Sync call's staging directory with a
// monotonic counter so concurrent calls inside one test do not
// collide.
var syncNonceCounter uint64

// downloadAsset streams one immutable asset to
// <stagingDir>/<id>/<filename>, hashing bytes on the same
// io.Copy (PR-2A streaming). When expectedSHA is non-empty the
// digest MUST match or downloadAsset removes the staged file and
// returns ErrChecksumMismatch.
func (s *Syncer) downloadAsset(
	ctx context.Context,
	base, commitSHA, id, filename string,
	maxBytes int64,
	expectedSHA string,
	stagingDir string,
) (FetchResult, error) {
	assetURL, err := AssetURL(base, commitSHA, id, filename)
	if err != nil {
		return FetchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("%w: build request for %s: %v", ErrOffline, assetURL, err)
	}
	trackDir := filepath.Join(stagingDir, id)
	if err := os.MkdirAll(trackDir, 0o700); err != nil {
		return FetchResult{}, fmt.Errorf("catalog: mkdir %s: %w", trackDir, err)
	}
	dest := filepath.Join(trackDir, filename)
	hasher := sha256.New()
	client := http.DefaultClient
	if c, ok := s.HTTPClient.(*http.Client); ok {
		client = c
	}
	result, err := fetchStreaming(ctx, client, req, dest, maxBytes, hasher)
	if err != nil {
		return FetchResult{}, err
	}
	if expectedSHA != "" && !strings.EqualFold(result.SHA256Hex, expectedSHA) {
		_ = os.Remove(dest)
		return FetchResult{}, fmt.Errorf("%w: id=%s name=%s expected=%s actual=%s",
			ErrChecksumMismatch, id, filename, strings.ToLower(expectedSHA), result.SHA256Hex)
	}
	return result, nil
}

// progressReporter returns s.Progress or NullProgressReporter when
// the field is nil. Centralising the fallback here keeps the Sync
// method free of nil checks at every emit site.
func (s *Syncer) progressReporter() ProgressReporter {
	if s.Progress == nil {
		return NullProgressReporter{}
	}
	return s.Progress
}

// IsTransientHTTP is the default transient predicate Sync uses
// to gate retries: 408/429/5xx → transient; 4xx other → permanent.
// Transport failures (no status code) → transient via ErrOffline.
func IsTransientHTTP(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.Transient
	}
	return errors.Is(err, ErrOffline)
}
