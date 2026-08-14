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
	"time"
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
	syn.Sleeper = noOpSleeper{}
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
// pinnedSHAURL builds the immutable manifest URL ValidateURL
// requires, pointed at the httptest server via the ?srv= query
// parameter that urlRewriteTransport below maps onto the test
// server. The Syncer sees a real GitHub URL while the bytes come
// from httptest.
func pinnedSHAURL(t *testing.T, srvURL string) string {
	t.Helper()
	return "https://raw.githubusercontent.com/lofi-player/localhost/" + pinnedSHA + "/catalog/v1/manifest.json?srv=" + strings.TrimPrefix(srvURL, "http://")
}

// TestSync_FullAssets_12GETs (PR-R1a A1/A2): a 4-track manifest
// served over httptest MUST drive 12 asset GETs (4 tracks × 3)
// on top of the manifest GET, populate v1 with manifest.json +
// four subdirs each carrying track.json/audio.mp3/LICENSE.txt,
// and verify audio.mp3 SHA matches Track.ChecksumSHA256 (PR-2A).
func TestSync_FullAssets_12GETs(t *testing.T) {
	wantIDs := []string{
		"bigger-questions",
		"going-in-circles",
		"it-was-like-that-when-i-got-here",
		"lofi-lion-tame-the-beast",
	}
	tracks := make([]Track, 0, len(wantIDs))
	for _, id := range wantIDs {
		tr := validTrack()
		tr.ID = id
		tracks = append(tracks, tr)
	}
	want := &Catalog{
		SchemaVersion: CurrentSchemaVersion,
		Version:       "v1.0",
		Tracks:        tracks,
	}
	manifestBytes, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	var hits, audioHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/manifest.json"):
			_, _ = w.Write(manifestBytes)
		case strings.HasSuffix(r.URL.Path, "/audio.mp3"):
			audioHits.Add(1)
			_, _ = w.Write([]byte(audioFixtureBytes))
		case strings.HasSuffix(r.URL.Path, "/track.json"):
			id := r.URL.Path[strings.LastIndex(strings.TrimSuffix(r.URL.Path, "/track.json"), "/")+1:]
			tr := validTrack()
			tr.ID = id
			data, _ := json.MarshalIndent(tr, "", "  ")
			_, _ = w.Write(data)
		case strings.HasSuffix(r.URL.Path, "/LICENSE.txt"):
			_, _ = w.Write([]byte("CC-BY-4.0\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	syn := NewSyncer(filepath.Join(dir, "v1"))
	syn.HTTPClient = &http.Client{Transport: urlRewriteTransport{target: srv.URL}}
	syn.Sleeper = noOpSleeper{}
	url := pinnedSHAURL(t, srv.URL)

	if err := syn.Sync(context.Background(), url); err != nil {
		t.Fatalf("Sync(12 GETs) = %v, want nil", err)
	}

	if got := audioHits.Load(); got != 4 {
		t.Fatalf("audio.mp3 hits = %d, want 4", got)
	}
	if got := hits.Load(); got != 13 {
		t.Fatalf("server hits = %d, want 13 (1 manifest + 12 assets)", got)
	}

	v1 := filepath.Join(dir, "v1")
	if _, err := os.Stat(filepath.Join(v1, "manifest.json")); err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}
	for _, id := range wantIDs {
		trackDir := filepath.Join(v1, id)
		for _, name := range []string{"track.json", "audio.mp3", "LICENSE.txt"} {
			if _, err := os.Stat(filepath.Join(trackDir, name)); err != nil {
				t.Fatalf("missing %s/%s: %v", id, name, err)
			}
		}
		if err := Verify(filepath.Join(trackDir, "audio.mp3"), want.Tracks[0].ChecksumSHA256); err != nil {
			t.Fatalf("SHA gate failed for %s: %v", id, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) > 1 {
		for _, e := range entries {
			if strings.Contains(e.Name(), "staging-") {
				t.Fatalf("leftover staging dir: %s", e.Name())
			}
		}
	}

	cat, err := LoadFromDir(v1)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if got, want := len(cat.Tracks), 4; got != want {
		t.Fatalf("len(Tracks) = %d, want %d", got, want)
	}
	for i, id := range wantIDs {
		if cat.Tracks[i].ID != id {
			t.Fatalf("Tracks[%d].ID = %q, want %q", i, cat.Tracks[i].ID, id)
		}
	}
}

// urlRewriteTransport forwards every request to a fixed httptest
// endpoint. The real server address is carried in a query param
// so the pinned-SHA URL stays shaped like a real GitHub URL.
// The request path is preserved so the fixture's per-asset
// dispatch sees the original URL (PR-R1a).
type urlRewriteTransport struct {
	target string
}

func (t urlRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	raw := req.URL.Query().Get("srv")
	if raw == "" {
		raw = t.target
	}
	// raw is an httptest URL like "http://127.0.0.1:56530"; the
	// trimmed prefix is "127.0.0.1:56530" which Go's URL parser
	// would mis-classify as a path with an embedded colon. We
	// split on "/" instead so the host+port round-trips verbatim.
	hostPort := strings.TrimPrefix(raw, "http://")
	cloned := req.Clone(req.Context())
	cloned.URL = &url.URL{
		Scheme: "http",
		Host:   hostPort,
		Path:   req.URL.Path,
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

// noOpSleeper returns immediately without blocking. Used by
// tests that don't care about exact sleep timing but want to
// skip the 1s+2s wall-clock sleeps the retry primitive would
// otherwise impose.
type noOpSleeper struct{}

func (noOpSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}
