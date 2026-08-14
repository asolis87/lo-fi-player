package main

import (
	"fmt"
	"github.com/asolis87/lo-fi-player/internal/catalog"
	"os"
	"os/user"
	"path/filepath"
)

// catalogCacheDir resolves the lo-fi-player catalog cache root,
// honoring XDG_CACHE_HOME and falling back to ~/.cache on Linux
// or the OS-reported UserCacheDir on macOS. Used by runList and
// runSync; credits.go keeps its own copy until PR #9 lands the
// refactor (decision #290 already documents the contract).
func catalogCacheDir() (string, error) {
	cacheRoot := os.Getenv("XDG_CACHE_HOME")
	if cacheRoot == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			cacheRoot = dir
		} else if current, err := user.Current(); err == nil {
			cacheRoot = filepath.Join(current.HomeDir, ".cache")
		} else {
			return "", err
		}
	}
	return filepath.Join(cacheRoot, "lofi-player", "catalog", "v1"), nil
}

// consumerLockGuard: code 1 + "sync in progress" si locked; nil cc. Best-effort.
func consumerLockGuard(cmd string) error {
	dir, err := catalogCacheDir()
	if err != nil {
		return nil
	}
	if locked, _ := catalog.IsLocked(filepath.Dir(dir)); locked {
		fmt.Fprintf(os.Stderr, "lofi %s: sync in progress\n", cmd)
		return &commandError{code: 1}
	}
	return nil
}
