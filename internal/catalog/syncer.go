// The Syncer is the catalog package's transactional fetch +
// apply engine. It owns the cache directory, validates the
// SHA-pinned manifest URL with ValidateURL (REQ-CAT-3 / decision
// #289), and writes the new manifest atomically so a partial
// fetch can never poison the local cache (S-CAT-2).
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	// manifestFileName is the cache-resident file Apply produces.
	// Atomic staging writes a sibling named ".tmp-<pid>-*" and
	// renames it over this target in one os.Rename call.
	manifestFileName = "manifest.json"

	// maxManifestBytes caps the body Fetch will read so a hostile
	// or buggy server cannot exhaust memory. 8 MiB is far above
	// the largest shipped manifest and far below anything that
	// matters for process memory.
	maxManifestBytes = 8 * 1024 * 1024
)

// ErrOffline is wrapped around every Fetch / Sync failure whose
// root cause is the network being unreachable, the remote
// returning a transport-level error, or the server returning a
// non-2xx status. Callers match with errors.Is so they can render
// a recoverable "no network" message and bail without touching
// the cache (S-CAT-2).
var ErrOffline = errors.New("catalog: network offline")

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
// Apply will populate with the target manifest file.
type Syncer struct {
	HTTPClient HTTPClient
	CacheDir   string
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
		return nil, fmt.Errorf("%w: GET %s: status %d", ErrOffline, manifestURL, resp.StatusCode)
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

// Sync is the high-level entrypoint: fetch manifestURL (which
// MUST pass ValidateURL) and apply atomically. Network failures
// leave the local cache untouched (S-CAT-2); callers can match
// ErrOffline with errors.Is to surface a recoverable message.
func (s *Syncer) Sync(ctx context.Context, manifestURL string) error {
	cat, err := s.Fetch(ctx, manifestURL)
	if err != nil {
		return err
	}
	return s.Apply(cat)
}