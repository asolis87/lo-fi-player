package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	catalogDirName   = "v1"
	stagingDirPrefix = "v1.staging-"
	backupDirSuffix  = ".backup-"
)

var ErrInvalidStagingDir = errors.New("catalog: invalid staging directory")

type OwnerCheck func(stagingPath string) (alive bool, err error)

var stagingNameRe = regexp.MustCompile(`^v1\.staging-([0-9]+)-([A-Za-z0-9_-]+)$`)

// Stage crea <cacheRoot>/v1.staging-<pid>-<nonce> en mode 0o700.
func Stage(cacheRoot, nonce string) (string, error) {
	if cacheRoot == "" {
		return "", fmt.Errorf("%w: cacheRoot is empty", ErrInvalidStagingDir)
	}
	if nonce == "" {
		return "", fmt.Errorf("%w: nonce is empty", ErrInvalidStagingDir)
	}
	staging := filepath.Join(cacheRoot, fmt.Sprintf("%s%d-%s", stagingDirPrefix, os.Getpid(), nonce))
	if err := os.Mkdir(staging, 0o700); err != nil {
		return "", fmt.Errorf("catalog: stage %s: %w", staging, err)
	}
	return staging, nil
}

func Cleanup(stagingPath string) error { return removeStaging(stagingPath) }

// CleanupStaleStaging elimina staging dirs cuyo OwnerCheck reporta muerto.
func CleanupStaleStaging(cacheRoot string, ownerCheck OwnerCheck) error {
	if cacheRoot == "" {
		return fmt.Errorf("%w: cacheRoot is empty", ErrInvalidStagingDir)
	}
	if ownerCheck == nil {
		return fmt.Errorf("catalog: CleanupStaleStaging requires non-nil ownerCheck")
	}
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("catalog: read %s: %w", cacheRoot, err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == catalogDirName || strings.HasPrefix(name, catalogDirName+backupDirSuffix) {
			continue
		}
		if !stagingNameRe.MatchString(name) {
			continue
		}
		path := filepath.Join(cacheRoot, name)
		alive, err := ownerCheck(path)
		if err != nil {
			return fmt.Errorf("catalog: ownerCheck %s: %w", path, err)
		}
		if alive {
			continue
		}
		if err := removeStaging(path); err != nil {
			return err
		}
	}
	return nil
}

func removeStaging(p string) error {
	if !stagingNameRe.MatchString(filepath.Base(p)) {
		return fmt.Errorf("%w: name=%q", ErrInvalidStagingDir, filepath.Base(p))
	}
	info, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("catalog: lstat %s: %w", p, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symlink", ErrInvalidStagingDir, p)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrInvalidStagingDir, p)
	}
	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("catalog: remove %s: %w", p, err)
	}
	return nil
}

func backupNameFromStaging(stagingName string) string {
	return strings.Replace(stagingName, stagingDirPrefix, catalogDirName+backupDirSuffix, 1)
}
