// Package catalog: promocion transaccional v1.
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrPromotionRecovery: step2+restore fallan; backup retenido para recovery.
var ErrPromotionRecovery = errors.New("catalog: promotion recovery failed")

// FileOps es el subset minimo que Promote necesita.
type FileOps interface {
	Rename(oldPath, newPath string) error
	RemoveAll(path string) error
}

type realFileOps struct{}

func (realFileOps) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }
func (realFileOps) RemoveAll(path string) error          { return os.RemoveAll(path) }

// Promote ejecuta el protocolo causal. Contrato (3.3): step1 fail=v1
// intacto; step2 fail+restore ok=v1 restaurado; step2+restore fail=
// ErrPromotionRecovery con backup retenido; happy=backup eliminado.
// On a first sync v1 does not exist yet — step 1 MUST be skipped
// (otherwise os.Rename returns ENOENT) without weakening the
// cache-preservation contract: there is no prior v1 to corrupt
// and the staging tree is the source of truth. This was added
// in PR-R1a because the Sync orchestration now composes
// Stage+downloads+Promote end-to-end and the existing Promote
// only handled the "v1 already exists" branch.
func Promote(cacheRoot, stagingPath string, fo FileOps) error {
	if cacheRoot == "" {
		return fmt.Errorf("catalog: promote: cacheRoot is empty")
	}
	if stagingPath == "" {
		return fmt.Errorf("catalog: promote: stagingPath is empty")
	}
	if fo == nil {
		fo = realFileOps{}
	}
	stagingName := filepath.Base(stagingPath)
	if !stagingNameRe.MatchString(stagingName) {
		return fmt.Errorf("%w: staging name=%q", ErrInvalidStagingDir, stagingName)
	}
	v1 := filepath.Join(cacheRoot, catalogDirName)
	backup := filepath.Join(cacheRoot, backupNameFromStaging(stagingName))

	// Step 1: v1 → backup. Skip when v1 does not exist (first sync
	// on an empty cache) — there is nothing to back up.
	v1Exists := false
	if _, err := os.Lstat(v1); err == nil {
		v1Exists = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("catalog: lstat %s: %w", v1, err)
	}
	if v1Exists {
		if err := fo.Rename(v1, backup); err != nil {
			return fmt.Errorf("catalog: promote step 1 (v1->backup): %w", err)
		}
	}
	if promoteErr := fo.Rename(stagingPath, v1); promoteErr != nil {
		if !v1Exists {
			return fmt.Errorf("catalog: promote step 2 (staging->v1, no prior v1): %w", promoteErr)
		}
		if restoreErr := fo.Rename(backup, v1); restoreErr != nil {
			return fmt.Errorf("%w: promote=%v restore=%v",
				ErrPromotionRecovery, promoteErr, restoreErr)
		}
		return fmt.Errorf("catalog: promote step 2 (staging->v1, restored from backup): %w", promoteErr)
	}
	if v1Exists {
		if err := fo.RemoveAll(backup); err != nil {
			return fmt.Errorf("catalog: promote step 3 (remove backup): %w", err)
		}
	}
	return nil
}
