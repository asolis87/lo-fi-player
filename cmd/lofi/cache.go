package main

import (
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