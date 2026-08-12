package config

import (
	"bytes"
	"errors"
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

// TestParseV2_StrictRouting cubre los 4 escenarios de SCHEMA-1 en una
// sola tabla: v2 exacto, v3 futuro, unknown key y malformado.
// Cualquier ruta no v2 retorna error (quarantine lo maneja el caller).
func TestParseV2_StrictRouting(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantVol int
		wantHis []string
		wantErr bool
	}{
		{"v2_round_trip", "schema_version = 2\nvolume = 75\nhistory = [\"A\", \"B\"]\n", 75, []string{"A", "B"}, false},
		{"future_v3_quarantines", "schema_version = 3\nvolume = 50\nhistory = [\"A\"]\n", 0, nil, true},
		{"unknown_key_quarantines", "schema_version = 2\nvolume = 75\nhistory = [\"A\"]\nunknown_field = 1\n", 0, nil, true},
		{"malformed_quarantines", "@@@ not valid toml @@@\n=garbage=\n[[[", 0, nil, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotHis, gotVol, err := parseV2([]byte(tc.in))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if gotVol != tc.wantVol {
				t.Fatalf("volume = %d, want %d", gotVol, tc.wantVol)
			}
			if !reflect.DeepEqual(gotHis, tc.wantHis) {
				t.Fatalf("history = %v, want %v", gotHis, tc.wantHis)
			}
		})
	}
}

// TestMigrateLegacyToV2_SpecScenarios cubre los 3 escenarios de
// HIST-3 en una sola tabla: reverse, dedupe-by-first-in-legacy, cap
// 25 sin evictar.
func TestMigrateLegacyToV2_SpecScenarios(t *testing.T) {
	legacy25 := make([]string, 25)
	for i := range legacy25 {
		legacy25[i] = string(rune('A' + i))
	}
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"reverse_simple", []string{"A", "B", "C"}, []string{"C", "B", "A"}},
		{"dedupe_by_first_in_legacy", []string{"A", "B", "A", "C"}, []string{"C", "B", "A"}},
		{"cap25_no_evict", legacy25, reverseSlice(legacy25)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := migrateLegacyToV2(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("migrated = %v, want %v", got, tc.want)
			}
		})
	}
}

// reverseSlice devuelve una copia del slice en orden inverso; se
// usa como helper para construir el "want" esperado por
// migrateLegacyToV2 sobre entradas en orden de insercion.
func reverseSlice(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

// TestMigrateLegacyToV2_EdgeCases es la cobertura A3 sobre
// migrateLegacyToV2: nil, vacio, un solo elemento, duplicados
// consecutivos, y cap estricto en 26 entradas. No introduce
// requisitos nuevos; endurece los escenarios ya verdes de A1.
func TestMigrateLegacyToV2_EdgeCases(t *testing.T) {
	legacy26 := make([]string, 26)
	for i := range legacy26 {
		legacy26[i] = string(rune('a' + i))
	}
	legacy25 := make([]string, 25)
	for i := range legacy25 {
		legacy25[i] = string(rune('a' + i))
	}
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil_input", nil, []string{}},
		{"empty_input", []string{}, []string{}},
		{"single_element", []string{"track-1"}, []string{"track-1"}},
		{"all_duplicates", []string{"A", "A", "A"}, []string{"A"}},
		{"consecutive_duplicates", []string{"A", "A", "B", "B", "C"}, []string{"C", "B", "A"}},
		{"two_distinct_reversed", []string{"A", "B"}, []string{"B", "A"}},
		{"cap_at_25", legacy25, reverseSlice(legacy25)},
		{"cap_evicts_in_26", legacy26, reverseSlice(legacy25[:25])},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := migrateLegacyToV2(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("migrateLegacyToV2(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestWriteV2_NeverWritesLastQueue garantiza que el escritor v2 no
// emite la clave last_queue: emision de history reemplaza el campo
// legacy.
func TestWriteV2_NeverWritesLastQueue(t *testing.T) {
	var buf strings.Builder
	if err := writeV2(&buf, []string{"A", "B"}, 60); err != nil {
		t.Fatalf("writeV2 error = %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "last_queue") {
		t.Fatalf("writeV2 emitted last_queue: %q", out)
	}
	if !strings.Contains(out, "schema_version = 2") {
		t.Fatalf("writeV2 missing schema_version line: %q", out)
	}
	if !strings.Contains(out, "volume = 60") {
		t.Fatalf("writeV2 missing volume line: %q", out)
	}
	if !strings.Contains(out, `"A"`) || !strings.Contains(out, `"B"`) {
		t.Fatalf("writeV2 missing history entries: %q", out)
	}
}

// TestQuarantineInvalidConfig_MovesFileAside verifica que el helper
// de quarantine mueve el archivo a <path>.bak y deja al path
// original ausente (politica existente extendida para v3+ y
// unknown-key).
func TestQuarantineInvalidConfig_MovesFileAside(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, fileName)
	corrupt := "schema_version = 3\nvolume = 50\nhistory = [\"A\"]\n"
	if err := os.WriteFile(target, []byte(corrupt), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	if err := quarantineInvalidConfig(target); err != nil {
		t.Fatalf("quarantineInvalidConfig error = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target %s still exists after quarantine", target)
	}
	bak := target + backupSuffix
	if _, err := os.Stat(bak); err != nil {
		t.Fatalf("backup %s missing: %v", bak, err)
	}
}

// errRenameForced es un sentinel de falla inyectable en el seam
// renameFile (PERSIST-2).
var errRenameForced = errors.New("config: rename forced failure")

// TestAtomicFailure_PreRename_PreservesPrior valida PERSIST-2:
// falla controlada antes del rename preserva archivo previo,
// elimina staging, devuelve error inyectado verbatim.
func TestAtomicFailure_PreRename_PreservesPrior(t *testing.T) {
	target := withTempConfigHome(t)
	parent := filepath.Dir(target)
	prior := "# prior contents that MUST survive the forced rename failure\nlast_queue = [\"a\", \"b\"]\nlast_track_index = 2\nvolume = 99\n"
	if err := os.WriteFile(target, []byte(prior), 0o600); err != nil {
		t.Fatalf("seed prior config: %v", err)
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read prior config: %v", err)
	}
	original := renameFile
	renameFile = func(string, string) error { return errRenameForced }
	t.Cleanup(func() { renameFile = original })
	err = Save(&Config{LastQueue: []string{"NEW"}, LastTrackIndex: 0, Volume: 1})
	if !errors.Is(err, errRenameForced) {
		t.Fatalf("Save() error = %v, want errRenameForced", err)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target after forced failure: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("prior config mutated byte-for-byte under forced rename failure:\nbefore=%q\nafter=%q", before, after)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("ReadDir(%s) after forced failure: %v", parent, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tempPrefix) {
			t.Fatalf("staging file leaked after forced rename failure: %s", e.Name())
		}
	}
}
