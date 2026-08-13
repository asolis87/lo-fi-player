package catalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// audioFixtureBytes is the deterministic payload writeTrackDir
// writes into every seeded audio.mp3. SHA-256 of these bytes is
// hardcoded in validTrack() so LoadFromDir's checksum gate (PR-D
// #3.2) can verify the fixture without re-hashing on every test.
const audioFixtureBytes = "dummy bytes"

// writeTrackDir stages a single <id>/{track.json,audio.mp3} under
// root from a mutated validTrack(). Tests use it to keep each
// fixture one line. The audio bytes are the deterministic
// payload above so the checksum in validTrack() matches.
func writeTrackDir(t *testing.T, root string, id string, mutate func(*Track)) {
	t.Helper()
	tr := validTrack()
	tr.ID = id
	if mutate != nil {
		mutate(&tr)
	}
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", id, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "track.json"), data, 0o644); err != nil {
		t.Fatalf("write track.json for %s: %v", id, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio.mp3"), []byte(audioFixtureBytes), 0o644); err != nil {
		t.Fatalf("write audio.mp3 for %s: %v", id, err)
	}
}

// TestLoadFromDir_ValidCatalog: three valid tracks + a stray
// LICENSE.txt MUST load to a 3-track catalog in sorted order.
func TestLoadFromDir_ValidCatalog(t *testing.T) {
	root := t.TempDir()
	writeTrackDir(t, root, "track-drizzle", nil)
	writeTrackDir(t, root, "track-rain", nil)
	writeTrackDir(t, root, "track-lofi-001", nil)
	if err := os.WriteFile(filepath.Join(root, "LICENSE.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatalf("seed LICENSE.txt: %v", err)
	}

	cat, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if got, want := len(cat.Tracks), 3; got != want {
		t.Fatalf("len(Tracks) = %d, want %d", got, want)
	}
	for i, want := range []string{"track-drizzle", "track-lofi-001", "track-rain"} {
		if got := cat.Tracks[i].ID; got != want {
			t.Fatalf("Tracks[%d].ID = %q, want %q", i, got, want)
		}
	}
}

// TestLoadFromDir_ReportsOffendingTrackID: a bad track aborts
// the load with an error that names its id.
func TestLoadFromDir_ReportsOffendingTrackID(t *testing.T) {
	root := t.TempDir()
	writeTrackDir(t, root, "track-drizzle", nil)
	writeTrackDir(t, root, "track-bad", func(tr *Track) { tr.License = "CC-BY-NC" })
	writeTrackDir(t, root, "track-rain", nil)

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir: expected error for bad track, got nil")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !strings.Contains(err.Error(), "track-bad") {
		t.Fatalf("error %q lacks offending track id", err.Error())
	}
}

// TestLoadFromDir_RejectsSubdirWithoutAudioMp3 is the PR-D #3.1
// gate: a subdir that ships track.json but is missing its audio
// bytes MUST abort the load (no silent skip) and the error MUST
// name the offending subdir so an operator can locate the gap.
func TestLoadFromDir_RejectsSubdirWithoutAudioMp3(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "track-bad")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.MarshalIndent(validTrack(), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "track.json"), data, 0o644); err != nil {
		t.Fatalf("seed track.json: %v", err)
	}

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir(missing audio.mp3) = nil, want error")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !strings.Contains(err.Error(), "track-bad") {
		t.Fatalf("error %q lacks offending subdir name", err.Error())
	}
}

// TestLoadFromDir_RejectsSubdirWithoutTrackJson is the PR-D #3.1
// twin for the other half of the subdir contract: a directory
// that has nothing but a stray asset (e.g. just LICENSE.txt) MUST
// abort the load rather than be silently skipped — slice #1's
// skip-on-missing-track.json silently produced empty catalogs
// when the cache dir was half-populated.
func TestLoadFromDir_RejectsSubdirWithoutTrackJson(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "track-orphan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "LICENSE.txt"), []byte("no track.json here"), 0o644); err != nil {
		t.Fatalf("seed LICENSE.txt: %v", err)
	}

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir(missing track.json) = nil, want error")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !strings.Contains(err.Error(), "track-orphan") {
		t.Fatalf("error %q lacks offending subdir name", err.Error())
	}
}

// TestLoadFromDir_RejectsChecksumMismatch is the PR-D #3.2 gate:
// when a subdir ships audio.mp3 + track.json but the embedded
// checksum_sha256 does not match the actual bytes, LoadFromDir
// MUST abort with an error wrapping ErrChecksumMismatch and
// naming the offending track id so the operator can re-hash the
// file or fix the manifest. We do not assert on the hex strings
// themselves (the digest is deterministic but verbose) — the
// wrapper-class check is what callers actually match on.
func TestLoadFromDir_RejectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "track-bad-checksum")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	tr := validTrack()
	tr.ID = "track-bad-checksum"
	tr.ChecksumSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	data, err := json.MarshalIndent(tr, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "track.json"), data, 0o644); err != nil {
		t.Fatalf("write track.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audio.mp3"), []byte(audioFixtureBytes), 0o644); err != nil {
		t.Fatalf("write audio.mp3: %v", err)
	}

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir(checksum mismatch) = nil, want error")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want wrapped ErrChecksumMismatch", err)
	}
	if !strings.Contains(err.Error(), "track-bad-checksum") {
		t.Fatalf("error %q lacks offending track id", err.Error())
	}
}

// writeManifestWith serializes a manifest.json declaring the given
// tracks under root. Each Track element is written verbatim — the
// caller chooses the SHA, license, and id so the test can drive
// both the count gate and the per-track validation path. The
// schema_version + version fields are set to current values so
// LoadFromFile's structural validation passes.
func writeManifestWith(t *testing.T, root string, declared []Track) {
	t.Helper()
	cat := Catalog{
		SchemaVersion: CurrentSchemaVersion,
		Version:       "1",
		Tracks:        declared,
	}
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest.json: %v", err)
	}
}

// tracksFromIDs builds N validTracks with the supplied ids, used
// by the manifest-backed tests to keep fixtures one line each.
func tracksFromIDs(ids ...string) []Track {
	out := make([]Track, len(ids))
	for i, id := range ids {
		tr := validTrack()
		tr.ID = id
		out[i] = tr
	}
	return out
}

// TestLoadFromDir_ManifestBacked_CountMismatch3 is the PR-4 task
// 4.1/4.2 RED gate for the lower bound on a manifest-backed
// cache: a manifest declaring exactly 3 otherwise-valid tracks
// MUST abort with an error that wraps ErrUnexpectedTrackCount
// and surfaces both expected (4) and observed (3) values.
func TestLoadFromDir_ManifestBacked_CountMismatch3(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"track-drizzle", "track-lofi-001", "track-rain"} {
		writeTrackDir(t, root, id, nil)
	}
	writeManifestWith(t, root, tracksFromIDs("track-drizzle", "track-lofi-001", "track-rain"))

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir(manifest with 3 tracks) = nil, want ErrUnexpectedTrackCount")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !errors.Is(err, ErrUnexpectedTrackCount) {
		t.Fatalf("error = %v, want wrapped ErrUnexpectedTrackCount", err)
	}
	if !strings.Contains(err.Error(), "expected 4") {
		t.Fatalf("error %q lacks expected count", err.Error())
	}
	if !strings.Contains(err.Error(), "got 3") {
		t.Fatalf("error %q lacks observed count", err.Error())
	}
}

// TestLoadFromDir_ManifestBacked_CountMismatch5 is the PR-4 task
// 4.1/4.2 RED gate for the upper bound: a manifest declaring 5
// otherwise-valid tracks MUST abort with the same ErrUnexpectedTrackCount
// sentinel so the gate is not merely a "minimum" check that lets
// excess slip through as a happy path.
func TestLoadFromDir_ManifestBacked_CountMismatch5(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"track-drizzle", "track-lofi-001", "track-rain", "track-zephyr", "track-cumulus"} {
		writeTrackDir(t, root, id, nil)
	}
	writeManifestWith(t, root, tracksFromIDs("track-drizzle", "track-lofi-001", "track-rain", "track-zephyr", "track-cumulus"))

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir(manifest with 5 tracks) = nil, want ErrUnexpectedTrackCount")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !errors.Is(err, ErrUnexpectedTrackCount) {
		t.Fatalf("error = %v, want wrapped ErrUnexpectedTrackCount", err)
	}
	if !strings.Contains(err.Error(), "expected 4") {
		t.Fatalf("error %q lacks expected count", err.Error())
	}
	if !strings.Contains(err.Error(), "got 5") {
		t.Fatalf("error %q lacks observed count", err.Error())
	}
}

// TestLoadFromDir_ManifestBacked_ExactlyFour is the PR-4 positive
// regression: a synced cache (manifest.json + one subdir per
// declared track) MUST load to a 4-track catalog sorted by id.
// This is the only test that proves the MVP gate accepts the
// contractually-correct cache; without it a regression that
// loosens the count check would still pass a 3-track fixture
// but ship a non-MVP catalog.
func TestLoadFromDir_ManifestBacked_ExactlyFour(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"track-drizzle", "track-lofi-001", "track-rain", "track-zephyr"} {
		writeTrackDir(t, root, id, nil)
	}
	writeManifestWith(t, root, tracksFromIDs("track-drizzle", "track-lofi-001", "track-rain", "track-zephyr"))

	cat, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir(manifest with 4 tracks): %v", err)
	}
	if got, want := len(cat.Tracks), 4; got != want {
		t.Fatalf("len(Tracks) = %d, want %d", got, want)
	}
	for i, want := range []string{"track-drizzle", "track-lofi-001", "track-rain", "track-zephyr"} {
		if got := cat.Tracks[i].ID; got != want {
			t.Fatalf("Tracks[%d].ID = %q, want %q", i, got, want)
		}
	}
}

// TestLoadFromDir_ManifestBacked_MalformedTrack_RealCauseWins
// guards the boundary decision: the count check MUST run only
// AFTER LoadFromFile validates each track's metadata. A manifest
// with 3 entries where one track has an invalid license MUST
// report ErrInvalidTrack (the real cause), NOT ErrUnexpectedTrackCount
// — otherwise the operator would see "expected 4, got 3" when
// the actual fault is a single malformed track.
func TestLoadFromDir_ManifestBacked_MalformedTrack_RealCauseWins(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"track-drizzle", "track-lofi-001", "track-rain"} {
		writeTrackDir(t, root, id, nil)
	}
	declared := tracksFromIDs("track-drizzle", "track-lofi-001", "track-rain")
	declared[1].License = "CC-BY-NC" // invalid: not in {CC0, CC-BY, CC-BY-SA}
	writeManifestWith(t, root, declared)

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir(malformed manifest) = nil, want ErrInvalidTrack")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if errors.Is(err, ErrUnexpectedTrackCount) {
		t.Fatalf("count check ran before track validation: %v", err)
	}
	if !errors.Is(err, ErrInvalidTrack) {
		t.Fatalf("error = %v, want wrapped ErrInvalidTrack", err)
	}
}

// TestLoadFromDir_ManifestBacked_MissingSubdir proves the
// subdir-consistency gate: a manifest that declares 4 tracks but
// the cache is missing one of those subdirs MUST abort with a
// message naming the manifest-declared id. This protects against
// a sync that wrote the manifest before all per-track files
// landed on disk — the operator needs to know which id is short.
func TestLoadFromDir_ManifestBacked_MissingSubdir(t *testing.T) {
	root := t.TempDir()
	// Three of the four subdirs exist; track-zephyr is missing.
	for _, id := range []string{"track-drizzle", "track-lofi-001", "track-rain"} {
		writeTrackDir(t, root, id, nil)
	}
	writeManifestWith(t, root, tracksFromIDs("track-drizzle", "track-lofi-001", "track-rain", "track-zephyr"))

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir(manifest vs cache mismatch) = nil, want error")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !strings.Contains(err.Error(), "track-zephyr") {
		t.Fatalf("error %q lacks the missing id", err.Error())
	}
	if errors.Is(err, ErrUnexpectedTrackCount) {
		t.Fatalf("missing-subdir reported as count mismatch: %v", err)
	}
}

// TestLoadFromDir_ManifestBacked_BadChecksum is the SHA-authority
// test for the manifest-backed path: the manifest IS the source
// of truth for ChecksumSHA256, so a manifest declaring a wrong
// SHA MUST abort with ErrChecksumMismatch naming the offending
// id. This is the same PR-D #3.2 gate the legacy path enforces,
// expressed at the manifest boundary.
func TestLoadFromDir_ManifestBacked_BadChecksum(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"track-drizzle", "track-lofi-001", "track-rain", "track-zephyr"} {
		writeTrackDir(t, root, id, nil)
	}
	declared := tracksFromIDs("track-drizzle", "track-lofi-001", "track-rain", "track-zephyr")
	declared[2].ChecksumSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	writeManifestWith(t, root, declared)

	cat, err := LoadFromDir(root)
	if err == nil {
		t.Fatal("LoadFromDir(manifest with wrong SHA) = nil, want ErrChecksumMismatch")
	}
	if cat != nil {
		t.Fatalf("non-nil catalog on error path: %+v", cat)
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want wrapped ErrChecksumMismatch", err)
	}
	if !strings.Contains(err.Error(), "track-rain") {
		t.Fatalf("error %q lacks the offending track id", err.Error())
	}
}

// TestLoadFromDir_NoManifest_LegacyAcceptsArbitraryCount is the
// gatekeeper guard against the rejected broad LoadFromDir count
// gate: a directory with N (here 2) valid subdirs and NO
// manifest.json MUST load to an N-track catalog, because the
// pre-MVP / fixture convention predates the manifest-backed
// contract. This proves the legacy path is unaffected and full
// CLI tests (which stage 1/2-track fixtures without a manifest)
// keep passing.
func TestLoadFromDir_NoManifest_LegacyAcceptsArbitraryCount(t *testing.T) {
	root := t.TempDir()
	writeTrackDir(t, root, "track-drizzle", nil)
	writeTrackDir(t, root, "track-rain", nil)
	// No manifest.json is written — LoadFromDir MUST take the
	// legacy path and return the 2 tracks it finds.

	cat, err := LoadFromDir(root)
	if err != nil {
		t.Fatalf("LoadFromDir(legacy 2-track) = %v, want nil", err)
	}
	if got, want := len(cat.Tracks), 2; got != want {
		t.Fatalf("len(Tracks) = %d, want %d (legacy path should not enforce the MVP count)", got, want)
	}
	for i, want := range []string{"track-drizzle", "track-rain"} {
		if got := cat.Tracks[i].ID; got != want {
			t.Fatalf("Tracks[%d].ID = %q, want %q", i, got, want)
		}
	}
}
