// Lock exclusivo del cache root (PR-5 tareas 5.2/5.4). Vive
// como <cacheRoot>/.sync.lock (sibling de v1). PIDs vivos o no
// verificables permanecen activos — evita robo por PID reuse.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const maxLockMetadataBytes = 4 * 1024

var ErrSyncInProgress = errors.New("catalog: sync in progress")

// ProcessInspector: realInspector usa signal 0; tests inyectan stubs.
type ProcessInspector interface {
	OwnerAlive(pid int) (alive bool, err error)
}

type Lock struct {
	path  string
	nonce string
}

type lockMetadata struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
	Nonce     string `json:"nonce"`
}

// Acquire crea <cacheRoot>/.sync.lock con O_CREATE|O_EXCL|O_WRONLY
// en modo 0600. Archivo preexistente retorna ErrSyncInProgress.
func Acquire(cacheRoot, nonce string) (*Lock, error) {
	if cacheRoot == "" || nonce == "" {
		return nil, fmt.Errorf("catalog: Acquire: cacheRoot=%q nonce=%q", cacheRoot, nonce)
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return nil, fmt.Errorf("catalog: mkdir %s: %w", cacheRoot, err)
	}
	path := filepath.Join(cacheRoot, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrSyncInProgress
		}
		return nil, fmt.Errorf("catalog: Acquire %s: %w", path, err)
	}
	meta := lockMetadata{PID: os.Getpid(), StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Nonce: nonce}
	if err := json.NewEncoder(f).Encode(meta); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("catalog: write lock %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("catalog: close lock %s: %w", path, err)
	}
	return &Lock{path: path, nonce: nonce}, nil
}

// Release elimina el lock solo si el nonce on-disk coincide.
// Lock ausente o Lock(nil) = no-op; defer Release es seguro.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("catalog: read lock %s: %w", l.path, err)
	}
	var meta lockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("catalog: parse lock %s: %w", l.path, err)
	}
	if meta.Nonce != l.nonce {
		return fmt.Errorf("catalog: lock nonce mismatch have=%q want=%q (not owner)", meta.Nonce, l.nonce)
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("catalog: remove lock %s: %w", l.path, err)
	}
	return nil
}

// IsLocked retorna true cuando .sync.lock existe. Consumers
// (list/credits/play <catalog-track>) lo usan para fallar con
// ErrSyncInProgress; sync decide si el lock es stale via
// ReclaimStale. No sigue symlinks.
func IsLocked(cacheRoot string) (bool, error) {
	if cacheRoot == "" {
		return false, fmt.Errorf("catalog: IsLocked: cacheRoot is empty")
	}
	_, err := os.Lstat(filepath.Join(cacheRoot, lockFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("catalog: lstat lock: %w", err)
	}
	return true, nil
}

// ReclaimStale mueve el lock a .sync-quarantine si el PID dueno
// esta verablemente muerto. (false, nil) = sin lock o PID vivo;
// (false, err) = no verificable; (true, nil) = PID muerto, en cuarentena.
func ReclaimStale(cacheRoot string, inspector ProcessInspector) (bool, error) {
	if cacheRoot == "" {
		return false, fmt.Errorf("catalog: ReclaimStale: cacheRoot is empty")
	}
	if inspector == nil {
		return false, fmt.Errorf("catalog: ReclaimStale: inspector is nil")
	}
	path := filepath.Join(cacheRoot, lockFileName)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("catalog: lstat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("catalog: lock %s is a symlink (refusing to follow)", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("catalog: lock %s is not a regular file", path)
	}
	if info.Size() > maxLockMetadataBytes {
		return false, fmt.Errorf("catalog: lock %s size=%d > max=%d (action: remove %s if safe)", path, info.Size(), maxLockMetadataBytes, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("catalog: read %s: %w", path, err)
	}
	var meta lockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return false, fmt.Errorf("catalog: lock metadata malformed: %w (action: remove %s if safe)", err, path)
	}
	if meta.PID <= 0 {
		return false, fmt.Errorf("catalog: lock metadata has invalid pid=%d (action: remove %s if safe)", meta.PID, path)
	}
	if meta.Nonce == "" {
		return false, fmt.Errorf("catalog: lock metadata has empty nonce (action: remove %s if safe)", path)
	}
	alive, err := inspector.OwnerAlive(meta.PID)
	if err != nil {
		return false, fmt.Errorf("catalog: PID inspection for %d failed (cannot reclaim, action: remove %s/%s if safe): %w", meta.PID, cacheRoot, lockFileName, err)
	}
	if alive {
		return false, nil
	}
	qdir := filepath.Join(cacheRoot, quarantineDir)
	if err := os.MkdirAll(qdir, 0o700); err != nil {
		return false, fmt.Errorf("catalog: mkdir quarantine %s: %w", qdir, err)
	}
	dst := filepath.Join(qdir, fmt.Sprintf("lock-dead-%d-%d", meta.PID, time.Now().UnixNano()))
	if err := os.Rename(path, dst); err != nil {
		return false, fmt.Errorf("catalog: quarantine lock: %w", err)
	}
	return true, nil
}

// realInspector implementa ProcessInspector con os.FindProcess
// + signal 0. ESRCH/ErrProcessDone = muerto; otros errores
// (típicamente EPERM por PID ajeno) se propagan al caller para
// que no afirme certeza sobre un proceso no propio.
func newRealInspector() ProcessInspector { return realInspector{} }

type realInspector struct{}

func (realInspector) OwnerAlive(pid int) (bool, error) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
