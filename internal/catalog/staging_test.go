// Tests staging layer y promocion transaccional (PR-3).
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func liveStagingName(nonce string) string {
	return fmt.Sprintf("v1.staging-%d-%s", os.Getpid(), nonce)
}

// fakeFileOps envuelve realFileOps; permite forzar errores
// de rename por oldPath para tests deterministas.
type fakeFileOps struct {
	inner       FileOps
	failRenames map[string]error
}

func (f *fakeFileOps) Rename(old, new string) error {
	if err, ok := f.failRenames[old]; ok {
		return err
	}
	return f.inner.Rename(old, new)
}

func (f *fakeFileOps) RemoveAll(p string) error { return f.inner.RemoveAll(p) }

func TestStage_HappyAndNegative(t *testing.T) {
	root := t.TempDir()
	staging, err := Stage(root, "abc")
	if err != nil || staging != filepath.Join(root, liveStagingName("abc")) {
		t.Fatalf("Stage: path=%q err=%v", staging, err)
	}
	info, err := os.Stat(staging)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("stat: info=%v err=%v", info, err)
	}
	for _, c := range []struct{ root, nonce string }{{"", "x"}, {root, ""}} {
		if _, err := Stage(c.root, c.nonce); err == nil || !errors.Is(err, ErrInvalidStagingDir) {
			t.Fatalf("Stage(%q,%q) = %v", c.root, c.nonce, err)
		}
	}
}

func TestCleanup_TableDriven(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name       string
		setup      func() string
		wantExists bool
		wantErr    bool
	}{
		{"valid", func() string {
			p := filepath.Join(root, liveStagingName("ok"))
			_ = os.MkdirAll(p, 0o700)
			return p
		}, false, false},
		{"v1_preserved", func() string {
			p := filepath.Join(root, "v1")
			_ = os.MkdirAll(p, 0o700)
			return p
		}, true, true},
		{"backup_preserved", func() string {
			p := filepath.Join(root, "v1.backup-12345-x")
			_ = os.MkdirAll(p, 0o700)
			return p
		}, true, true},
		{"bad_patterns", func() string {
			p := filepath.Join(root, "v1.staging-1-..")
			_ = os.MkdirAll(p, 0o700)
			return p
		}, true, true},
		{"symlink_rejected", func() string {
			target := t.TempDir()
			p := filepath.Join(root, liveStagingName("link"))
			if err := os.Symlink(target, p); err != nil {
				t.Skipf("symlink no soportado: %v", err)
			}
			return p
		}, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.setup()
			err := Cleanup(p)
			if (err != nil) != c.wantErr {
				t.Fatalf("Cleanup(%s) err = %v, wantErr = %v", c.name, err, c.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidStagingDir) {
				t.Fatalf("Cleanup(%s) = %v, quiere envolver ErrInvalidStagingDir", c.name, err)
			}
			_, statErr := os.Stat(p)
			if (statErr == nil) != c.wantExists {
				t.Fatalf("Cleanup(%s) exists=%v want=%v", c.name, statErr == nil, c.wantExists)
			}
		})
	}
}

func TestCleanupStaleStaging_TableDriven(t *testing.T) {
	root := t.TempDir()
	v1 := filepath.Join(root, "v1")
	backup := filepath.Join(root, "v1.backup-12345-nonce")
	stale := filepath.Join(root, "v1.staging-9999999-stale")
	live := filepath.Join(root, liveStagingName("live"))
	_ = os.MkdirAll(v1, 0o700)
	_ = os.WriteFile(filepath.Join(v1, "manifest.json"), []byte("keep"), 0o600)
	_ = os.MkdirAll(backup, 0o700)
	_ = os.MkdirAll(stale, 0o700)
	_ = os.MkdirAll(live, 0o700)

	check := func(p string) (bool, error) {
		if strings.Contains(p, "9999999") {
			return false, nil
		}
		return true, nil
	}
	if err := CleanupStaleStaging(root, nil); err == nil {
		t.Fatal("nil ownerCheck debio fallar")
	}
	want := errors.New("check boom")
	_ = os.MkdirAll(filepath.Join(root, "v1.staging-9999999-err"), 0o700)
	if err := CleanupStaleStaging(root, func(string) (bool, error) { return false, want }); !errors.Is(err, want) {
		t.Fatalf("err = %v, quiere envolver %v", err, want)
	}
	if err := CleanupStaleStaging(root, check); err != nil {
		t.Fatalf("CleanupStaleStaging: %v", err)
	}
	if _, err := os.Stat(v1); err != nil {
		t.Fatalf("v1 eliminado: %v", err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup eliminado: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(v1, "manifest.json")); string(got) != "keep" {
		t.Fatalf("v1 manifest mutado: %q", got)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale no eliminado: %v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("live eliminado: %v", err)
	}
}

func seedPromote(t *testing.T, nonce string) (root, v1, staging, backup string) {
	t.Helper()
	root = t.TempDir()
	v1 = filepath.Join(root, "v1")
	staging = filepath.Join(root, liveStagingName(nonce))
	backup = filepath.Join(root, "v1.backup-"+filepath.Base(staging)[len(stagingDirPrefix):])
	_ = os.MkdirAll(v1, 0o700)
	_ = os.WriteFile(filepath.Join(v1, "manifest.json"), []byte("old"), 0o600)
	_ = os.MkdirAll(staging, 0o700)
	_ = os.WriteFile(filepath.Join(staging, "manifest.json"), []byte("new"), 0o600)
	return root, v1, staging, backup
}

func TestPromote_HappyPath(t *testing.T) {
	root, v1, staging, _ := seedPromote(t, "happy")
	if err := Promote(root, staging, nil); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(v1, "manifest.json")); string(got) != "new" {
		t.Fatalf("v1 = %q, quiere %q", got, "new")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging presente: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(root, "v1.backup-*")); len(matches) != 0 {
		t.Fatalf("backup no eliminado: %v", matches)
	}
}

func TestPromote_Step1Fails_NoRestore(t *testing.T) {
	root, v1, staging, _ := seedPromote(t, "step1")
	fo := &fakeFileOps{inner: realFileOps{}, failRenames: map[string]error{
		v1: errors.New("disk full"),
	}}
	err := Promote(root, staging, fo)
	if err == nil || errors.Is(err, ErrPromotionRecovery) {
		t.Fatalf("err = %v, quiere fallo no-recuperable", err)
	}
	if got, _ := os.ReadFile(filepath.Join(v1, "manifest.json")); string(got) != "old" {
		t.Fatalf("v1 mutado: %q", got)
	}
	if matches, _ := filepath.Glob(filepath.Join(root, "v1.backup-*")); len(matches) != 0 {
		t.Fatalf("backup no debio existir: %v", matches)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging ausente: %v", err)
	}
}

func TestPromote_Step2Fails_Restored(t *testing.T) {
	root, v1, staging, backup := seedPromote(t, "step2")
	fo := &fakeFileOps{inner: realFileOps{}, failRenames: map[string]error{
		staging: errors.New("step 2 fail"),
	}}
	err := Promote(root, staging, fo)
	if err == nil || errors.Is(err, ErrPromotionRecovery) {
		t.Fatalf("err = %v, quiere fallo step 2 sin recovery", err)
	}
	if got, _ := os.ReadFile(filepath.Join(v1, "manifest.json")); string(got) != "old" {
		t.Fatalf("v1 no restaurado: %q", got)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup retenido tras restore OK: %v", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("staging ausente: %v", err)
	}
}

func TestPromote_RestoreFails_RecoveryError(t *testing.T) {
	root, _, staging, backup := seedPromote(t, "restorefail")
	fo := &fakeFileOps{inner: realFileOps{}, failRenames: map[string]error{
		staging: errors.New("step 2"),
		backup:  errors.New("restore"),
	}}
	err := Promote(root, staging, fo)
	if err == nil || !errors.Is(err, ErrPromotionRecovery) {
		t.Fatalf("err = %v, quiere envolver ErrPromotionRecovery", err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup no retenido: %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "promote=") || !strings.Contains(msg, "restore=") {
		t.Fatalf("err message %q no expone ambas causas", msg)
	}
}
