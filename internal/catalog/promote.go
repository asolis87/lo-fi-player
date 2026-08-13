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

	if err := fo.Rename(v1, backup); err != nil {
		return fmt.Errorf("catalog: promote step 1 (v1->backup): %w", err)
	}
	if promoteErr := fo.Rename(stagingPath, v1); promoteErr != nil {
		if restoreErr := fo.Rename(backup, v1); restoreErr != nil {
			return fmt.Errorf("%w: promote=%v restore=%v",
				ErrPromotionRecovery, promoteErr, restoreErr)
		}
		return fmt.Errorf("catalog: promote step 2 (staging->v1, restored from backup): %w", promoteErr)
	}
	if err := fo.RemoveAll(backup); err != nil {
		return fmt.Errorf("catalog: promote step 3 (remove backup): %w", err)
	}
	return nil
}
