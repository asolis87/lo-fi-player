package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// validCatalog returns a minimal Catalog that parses cleanly so
// each Fetch test only mutates the byte stream it cares about.
func validCatalog() *Catalog {
	t := validTrack()
	return &Catalog{
		SchemaVersion: CurrentSchemaVersion,
		Version:       "1",
		Tracks:        []Track{t},
	}
}

// TestFetch_ValidManifest_ReturnsCatalog guards REQ-FCH-1: a
// well-formed manifest served over HTTPS must parse to a
// non-nil Catalog with the right SchemaVersion.
func TestFetch_ValidManifest_ReturnsCatalog(t *testing.T) {
	body, err := json.Marshal(validCatalog())
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	url := pinnedSHAURL(t, srv.URL)
	syn := NewSyncer(t.TempDir())
	syn.HTTPClient = &http.Client{Transport: urlRewriteTransport{target: srv.URL}}

	cat, err := syn.Fetch(context.Background(), url)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if cat == nil {
		t.Fatal("Fetch returned nil catalog")
	}
	if cat.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", cat.SchemaVersion, CurrentSchemaVersion)
	}
	if len(cat.Tracks) != 1 || cat.Tracks[0].ID != validTrack().ID {
		t.Fatalf("Tracks = %+v, want single valid track", cat.Tracks)
	}
}

// TestFetch_NetworkError_ReturnsErrOffline guards S-CAT-2: a
// network failure MUST surface as ErrOffline so the coordinator
// can render a recoverable message and bail without mutating
// cache.
func TestFetch_NetworkError_ReturnsErrOffline(t *testing.T) {
	syn := NewSyncer(t.TempDir())
	syn.HTTPClient = &http.Client{Transport: failingTransport{}}
	url := pinnedSHAURL(t, "https://raw.githubusercontent.com/asolis87/lo-fi-player/"+pinnedSHA+"/catalog/v1/manifest.json")

	_, err := syn.Fetch(context.Background(), url)
	if err == nil {
		t.Fatal("Fetch = nil, want error")
	}
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("Fetch error = %v, want wrapped ErrOffline", err)
	}
	if !strings.Contains(err.Error(), pinnedSHA) {
		t.Fatalf("error %q lacks the manifest URL", err.Error())
	}
}

// TestFetch_BadJSON_ReturnsError guards REQ-CAT-1: a malformed
// manifest MUST NOT parse; the error must wrap the URL so the
// user knows which fetch failed.
func TestFetch_BadJSON_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{this is not json"))
	}))
	t.Cleanup(srv.Close)

	url := pinnedSHAURL(t, srv.URL)
	syn := NewSyncer(t.TempDir())
	syn.HTTPClient = &http.Client{Transport: urlRewriteTransport{target: srv.URL}}

	_, err := syn.Fetch(context.Background(), url)
	if err == nil {
		t.Fatal("Fetch(bad json) = nil, want error")
	}
	if !strings.Contains(err.Error(), pinnedSHA) {
		t.Fatalf("error %q lacks the manifest URL", err.Error())
	}
}

// TestFetch_RejectsFloatingRef guards REQ-CAT-3: passing a URL
// with a non-pinned SHA must fail ValidateURL BEFORE the network
// is touched. Use a never-resolved host so any HTTP attempt
// surfaces as a clear test failure.
func TestFetch_RejectsFloatingRef(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called.Add(1)
	}))
	t.Cleanup(srv.Close)

	syn := NewSyncer(t.TempDir())
	syn.HTTPClient = &http.Client{Transport: urlRewriteTransport{target: srv.URL}}
	url := "https://raw.githubusercontent.com/asolis87/lo-fi-player/latest/catalog/v1/manifest.json"

	_, err := syn.Fetch(context.Background(), url)
	if err == nil {
		t.Fatal("Fetch(floating ref) = nil, want error")
	}
	if !errors.Is(err, ErrFloatRef) {
		t.Fatalf("Fetch error = %v, want wrapped ErrFloatRef", err)
	}
	if called.Load() != 0 {
		t.Fatalf("HTTP server hit %d times; ValidateURL must reject before the network is touched", called.Load())
	}
}

// TestApply_AtomicSwap guards the transactional sync contract:
// Apply writes a manifest that subsequent reads see in full, with
// no leftover staging file under the cache dir.
func TestApply_AtomicSwap(t *testing.T) {
	dir := t.TempDir()
	syn := NewSyncer(dir)
	cat := validCatalog()
	if err := syn.Apply(cat); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	target := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}
	var got Catalog
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("manifest.json is not parseable JSON: %v", err)
	}
	if got.SchemaVersion != CurrentSchemaVersion || len(got.Tracks) != 1 {
		t.Fatalf("round-tripped catalog = %+v, want single valid track", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("leftover staging file: %s", e.Name())
		}
	}
}

// TestApply_FailureLeavesOriginalIntact guards S-CAT-2: when
// the staging write fails, the previously cached manifest MUST
// survive untouched. We seed a manifest, lock the cache dir to
// read+execute only so os.CreateTemp cannot stage, and assert the
// prior bytes are still on disk after Apply returns an error.
func TestApply_FailureLeavesOriginalIntact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "manifest.json")
	prior := []byte(`{"schema_version":1,"version":"prior","tracks":[]}`)
	if err := os.WriteFile(target, prior, 0o600); err != nil {
		t.Fatalf("seed prior manifest: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	syn := NewSyncer(dir)
	if err := syn.Apply(validCatalog()); err == nil {
		t.Fatal("Apply(ro dir) = nil, want error")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read manifest after failed Apply: %v", err)
	}
	if string(got) != string(prior) {
		t.Fatalf("Apply mutated cache: got %q, want prior %q", got, prior)
	}
}

// TestSync_OfflineDoesNotMutateCache guards S-CAT-2: when the
// network is down, Sync MUST return ErrOffline and MUST leave any
// pre-existing manifest untouched.
func TestSync_OfflineDoesNotMutateCache(t *testing.T) {
	dir := t.TempDir()
	prior := []byte(`{"schema_version":1,"version":"prior","tracks":[]}`)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), prior, 0o600); err != nil {
		t.Fatalf("seed prior manifest: %v", err)
	}
	syn := NewSyncer(dir)
	syn.HTTPClient = &http.Client{Transport: failingTransport{}}
	url := pinnedSHAURL(t, "https://raw.githubusercontent.com/asolis87/lo-fi-player/"+pinnedSHA+"/catalog/v1/manifest.json")

	err := syn.Sync(context.Background(), url)
	if err == nil {
		t.Fatal("Sync = nil, want error")
	}
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("Sync error = %v, want wrapped ErrOffline", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest after Sync: %v", err)
	}
	if string(got) != string(prior) {
		t.Fatalf("Sync mutated cache: got %q, want prior %q", got, prior)
	}
}

// TestSync_HappyPath_RoundTrip is PR-E's integration gate (task
// 4.5): an end-to-end Sync against an httptest.Server that serves
// a valid catalog MUST write the manifest to the cache dir
// atomically, leave no stale staging files, and make the manifest
// visible to a follow-up LoadFromFile. The transport that bridges
// the pinned-SHA URL onto the test server (urlRewriteTransport
// below) is the same one the offline-test paths use; this test
// proves the same plumbing lights up cleanly when the server
// returns a well-formed body.
func TestSync_HappyPath_RoundTrip(t *testing.T) {
	want := validCatalog()
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	syn := NewSyncer(dir)
	syn.HTTPClient = &http.Client{Transport: urlRewriteTransport{target: srv.URL}}
	url := pinnedSHAURL(t, srv.URL)

	if err := syn.Sync(context.Background(), url); err != nil {
		t.Fatalf("Sync(happy path) = %v, want nil", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server hit %d times, want exactly 1 (Fetch+Apply, no retries)", got)
	}

	target := filepath.Join(dir, "manifest.json")
	gotBytes, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read cache manifest: %v", err)
	}
	var got Catalog
	if err := json.Unmarshal(gotBytes, &got); err != nil {
		t.Fatalf("cache manifest is not parseable JSON: %v\n%s", err, gotBytes)
	}
	if got.SchemaVersion != want.SchemaVersion || got.Version != want.Version {
		t.Fatalf("round-tripped catalog = %+v, want %+v", got, want)
	}
	if len(got.Tracks) != 1 || got.Tracks[0].ID != want.Tracks[0].ID {
		t.Fatalf("round-tripped tracks = %+v, want single track id=%q", got.Tracks, want.Tracks[0].ID)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("leftover staging file after happy path: %s", e.Name())
		}
	}
}

// TestSync_AppliesRealManifest proves that after a successful
// end-to-end Sync, the file LoadFromFile reads back is the SAME
// manifest body the server returned (byte-for-byte), so the
// runtime contract "the cache IS the pinned manifest" holds.
func TestSync_AppliesRealManifest(t *testing.T) {
	want := validCatalog()
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	syn := NewSyncer(dir)
	syn.HTTPClient = &http.Client{Transport: urlRewriteTransport{target: srv.URL}}
	url := pinnedSHAURL(t, srv.URL)

	if err := syn.Sync(context.Background(), url); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	loaded, err := LoadFromFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("LoadFromFile on synced cache: %v", err)
	}
	if loaded.Version != want.Version {
		t.Fatalf("loaded.Version = %q, want %q", loaded.Version, want.Version)
	}
	if len(loaded.Tracks) != 1 || loaded.Tracks[0].ID != want.Tracks[0].ID {
		t.Fatalf("loaded.Tracks = %+v, want single track id=%q", loaded.Tracks, want.Tracks[0].ID)
	}
}

// pinnedSHAURL builds the immutable manifest URL ValidateURL
// requires, pointed at the httptest server. The URL the test
// server actually answers lives at srv.URL (a 127.0.0.1:port
// http URL); the Syncer uses urlRewriteTransport below to map
// the raw.githubusercontent.com URL back onto the test server.
func pinnedSHAURL(t *testing.T, srvURL string) string {
	t.Helper()
	return "https://raw.githubusercontent.com/lofi-player/localhost/" + pinnedSHA + "/catalog/v1/manifest.json?srv=" + strings.TrimPrefix(srvURL, "http://")
}

// urlRewriteTransport forwards every request to a single fixed
// http endpoint regardless of the URL the Syncer hands it. We
// carry the real server address in a query parameter so the
// pinned-SHA URL the test builds can stay shaped like a real
// GitHub URL while the bytes actually come from httptest.
type urlRewriteTransport struct {
	target string
}

func (t urlRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	raw := req.URL.Query().Get("srv")
	if raw == "" {
		raw = t.target
	}
	cloned := req.Clone(req.Context())
	cloned.URL = &url.URL{
		Scheme: "http",
		Host:   strings.TrimPrefix(raw, "http://"),
		Path:   "/",
	}
	cloned.RequestURI = ""
	return http.DefaultTransport.RoundTrip(cloned)
}

// failingTransport is an http.RoundTripper that always returns an
// error; used to simulate offline network without DNS lookups.
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.EOF
}