package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// withTempConfigHome points XDG_CONFIG_HOME and HOME at a fresh
// temp directory and returns the resulting config path. Tests use
// it to keep the user's real config isolated.
func withTempConfigHome(t *testing.T) (configPath string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	d, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", d, err)
	}
	return filepath.Join(d, fileName)
}

// TestDir_XDGOverride guards REQ-CFG-1: $XDG_CONFIG_HOME wins.
func TestDir_XDGOverride(t *testing.T) {
	xdg, home := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", home)
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	want := filepath.Join(xdg, "lofi-player")
	if got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

// TestDir_DefaultHome guards REQ-CFG-1: empty XDG falls back to HOME.
func TestDir_DefaultHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	want := filepath.Join(home, ".config", "lofi-player")
	if got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

// TestLoad_MissingFile_ReturnsDefaults: first run returns Default().
func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	withTempConfigHome(t)
	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil on missing file", err)
	}
	if !reflect.DeepEqual(*got, Default()) {
		t.Fatalf("Load() = %+v, want %+v", *got, Default())
	}
}

// TestSaveLoad_RoundTrip guards REQ-CFG-2: every persisted field
// round-trips byte-identically (including the queue order).
func TestSaveLoad_RoundTrip(t *testing.T) {
	withTempConfigHome(t)
	want := &Config{
		LastQueue:      []string{"track-drizzle", "track-rain", "track-lofi-001"},
		LastTrackIndex: 1,
		Volume:         65,
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(*got, *want) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", *got, *want)
	}
}

// TestLoad_CorruptedFile_BackupAndDefaults guards S-CFG-2: a
// corrupted file is renamed to .bak and the loader returns defaults
// without panicking.
func TestLoad_CorruptedFile_BackupAndDefaults(t *testing.T) {
	target := withTempConfigHome(t)
	corrupt := "@@@ not valid toml @@@\n=garbage=\n[[["
	if err := os.WriteFile(target, []byte(corrupt), 0o600); err != nil {
		t.Fatalf("seed corrupted config: %v", err)
	}

	got := LoadOrDefault()
	if !reflect.DeepEqual(*got, Default()) {
		t.Fatalf("LoadOrDefault() = %+v, want defaults %+v", *got, Default())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("corrupted config still at %s after LoadOrDefault (err=%v)", target, err)
	}
	bak := target + ".bak"
	info, err := os.Stat(bak)
	if err != nil {
		t.Fatalf("backup %s missing: %v", bak, err)
	}
	if info.IsDir() {
		t.Fatalf("backup %s is a directory", bak)
	}
	data, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(data) != corrupt {
		t.Fatalf("backup contents = %q, want %q", string(data), corrupt)
	}
}

// TestSave_AtomicWrite verifies Save replaces the target in one
// rename and leaves no staging files behind. tempPrefix is the
// staging-file prefix declared in store.go.
func TestSave_AtomicWrite(t *testing.T) {
	target := withTempConfigHome(t)
	parent := filepath.Dir(target)
	prior := "# prior contents that MUST be gone after Save\nvolume = 99\n"
	if err := os.WriteFile(target, []byte(prior), 0o600); err != nil {
		t.Fatalf("seed prior config: %v", err)
	}

	cfg := &Config{LastQueue: []string{"track-1"}, LastTrackIndex: 0, Volume: 42}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read final config: %v", err)
	}
	contents := string(data)
	if !strings.Contains(contents, "track-1") {
		t.Fatalf("final config missing new payload: %q", contents)
	}
	if strings.Contains(contents, "prior contents") {
		t.Fatalf("final config still carries prior payload: %q", contents)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", parent, err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == filepath.Base(target) {
			continue
		}
		if strings.HasPrefix(name, tempPrefix) {
			t.Fatalf("leftover staging file in dir: %s", name)
		}
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Volume != 42 || len(got.LastQueue) != 1 || got.LastQueue[0] != "track-1" {
		t.Fatalf("Load() after Save = %+v, want Volume=42 LastQueue=[track-1]", *got)
	}
}
