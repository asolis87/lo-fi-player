package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeInspector struct {
	alive map[int]bool
	err   error
}

func (f *fakeInspector) OwnerAlive(pid int) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.alive[pid], nil
}

func seedLock(t *testing.T, root string, body []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, lockFileName), body, 0o600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
}

func TestLockAcquireConcurrent(t *testing.T) {
	root := t.TempDir()
	first, err := Acquire(root, "first")
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if _, err := Acquire(root, "second"); !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("second Acquire = %v, want ErrSyncInProgress", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := Acquire(root, "third"); err != nil {
		t.Fatalf("third Acquire post-Release: %v", err)
	}
}

func TestLockRelease_NonceMismatch(t *testing.T) {
	root := t.TempDir()
	l, err := Acquire(root, "mine")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = l.Release() }()
	seedLock(t, root, []byte(`{"pid":1,"started_at":"x","nonce":"other"}`))
	err = l.Release()
	if err == nil || !strings.Contains(err.Error(), "nonce mismatch") {
		t.Fatalf("Release mismatch = %v, want error", err)
	}
	if _, err := os.Stat(filepath.Join(root, lockFileName)); err != nil {
		t.Fatalf("lock removed despite mismatch: %v", err)
	}
}

func TestIsLocked(t *testing.T) {
	root := t.TempDir()
	if locked, _ := IsLocked(root); locked {
		t.Fatal("IsLocked(absent) = true")
	}
	seedLock(t, root, []byte(`{"pid":1,"started_at":"x","nonce":"n"}`))
	if locked, _ := IsLocked(root); !locked {
		t.Fatal("IsLocked(present) = false")
	}
}

func TestReclaimStale_TableDriven(t *testing.T) {
	t.Run("dead_quarantines", func(t *testing.T) {
		root := t.TempDir()
		seedLock(t, root, []byte(`{"pid":9999999,"started_at":"x","nonce":"dead"}`))
		reclaimed, err := ReclaimStale(root, &fakeInspector{alive: map[int]bool{9999999: false}})
		if err != nil || !reclaimed {
			t.Fatalf("ReclaimStale = (%v,%v), want (true,nil)", reclaimed, err)
		}
		if _, err := os.Stat(filepath.Join(root, lockFileName)); !os.IsNotExist(err) {
			t.Fatalf("lock still present: %v", err)
		}
		matches, _ := filepath.Glob(filepath.Join(root, quarantineDir, "lock-dead-9999999-*"))
		if len(matches) != 1 {
			t.Fatalf("quarantine files = %d, want 1 (%v)", len(matches), matches)
		}
	})
	t.Run("live_respected", func(t *testing.T) {
		root := t.TempDir()
		seedLock(t, root, []byte(`{"pid":12345,"started_at":"x","nonce":"live"}`))
		reclaimed, err := ReclaimStale(root, &fakeInspector{alive: map[int]bool{12345: true}})
		if err != nil || reclaimed {
			t.Fatalf("ReclaimStale(live) = (%v,%v), want (false,nil)", reclaimed, err)
		}
		if _, err := os.Stat(filepath.Join(root, lockFileName)); err != nil {
			t.Fatalf("lock removed despite live owner: %v", err)
		}
	})
	t.Run("unverifiable_no_reclaim", func(t *testing.T) {
		root := t.TempDir()
		seedLock(t, root, []byte(`{"pid":4242,"started_at":"x","nonce":"u"}`))
		want := errors.New("EPERM")
		_, err := ReclaimStale(root, &fakeInspector{err: want})
		if !errors.Is(err, want) {
			t.Fatalf("ReclaimStale(unverifiable) = %v, want wrap %v", err, want)
		}
		if _, err := os.Stat(filepath.Join(root, lockFileName)); err != nil {
			t.Fatalf("lock removed despite unverifiable PID: %v", err)
		}
	})
}
