package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// syncOwner, syncRepo, syncPlaceholderRepo are the production
// coordinates runSync feeds into catalog.ResolveManifestURL. The
// owner+repo stay stable across releases; the SHA comes from
// catalog.FirstRunCommitSHA so a release only has to swap one
// symbol.
const (
	syncOwner = "asolis87"
	syncRepo  = "lo-fi-player"
)

// syncerFactory builds the catalog.Syncer runSync uses. Tests
// swap it for an offline-transport variant; production always
// keeps the default value that delegates to catalog.NewSyncer.
var syncerFactory = func(cacheDir string) *catalog.Syncer {
	return catalog.NewSyncer(cacheDir)
}

// runSyncContextFactory returns the ctx + cancel func. Tests inject plain context.
var runSyncContextFactory = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// lastSyncErr captures the most recent error runSync returned so
// tests can assert on the wrapped sentinel without re-running the
// CLI. Production callers should rely on the returned error
// directly; the package-level mirror exists only to keep tests
// free of goroutines or temp files.
var lastSyncErr error

// runSync implements `lofi sync`. It resolves the cache dir via
// catalogCacheDir(), constructs a Syncer (injected through
// syncerFactory for tests), and calls Sync against the SHA-pinned
// manifest URL produced by catalog.ResolveManifestURL with the
// hardcoded owner/repo and catalog.FirstRunCommitSHA.
//
// Errors fall into three classes:
//
//   - URL validation (catalog.ErrFloatRef): wrong SHA shape, wrong
//     host, or floating ref. The placeholder SHA in slice #1
//     intentionally fails this gate until PR #10 ships a real SHA.
//   - Network failure (catalog.ErrOffline): recoverable; the user
//     is told the cache is unchanged and can retry later.
//   - Anything else: the error message is surfaced verbatim.
//
// Adquiere el cache-root lock antes de tocar el cache (PR-5).
func runSync(args []string) error {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "lofi sync: unexpected argument %q\n\n%s", args[0], usage)
		lastSyncErr = &commandError{code: 2}
		return lastSyncErr
	}

	ctx, stop := runSyncContextFactory()
	defer stop()

	cacheRoot, err := catalogCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lofi sync: cannot locate cache dir: %v\n", err)
		lastSyncErr = &commandError{code: 1}
		return lastSyncErr
	}
	lock, err := catalog.Acquire(filepath.Dir(cacheRoot), "sync")
	if err != nil {
		if errors.Is(err, catalog.ErrSyncInProgress) {
			fmt.Fprintln(os.Stderr, "lofi sync: sync in progress")
		} else {
			fmt.Fprintf(os.Stderr, "lofi sync: acquire lock: %v\n", err)
		}
		lastSyncErr = &commandError{code: 1}
		return lastSyncErr
	}
	defer lock.Release()

	url := catalog.ResolveManifestURL(syncRepo, syncOwner, catalog.FirstRunCommitSHA)
	syn := syncerFactory(cacheRoot)

	if err := syn.Sync(ctx, url); err != nil {
		lastSyncErr = err
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "lofi sync: cancelled")
			return &commandError{code: 130}
		}
		switch {
		case errors.Is(err, catalog.ErrOffline):
			fmt.Fprintln(os.Stderr, "lofi sync: network unavailable; cache left untouched, retry when online")
		case errors.Is(err, catalog.ErrFloatRef):
			fmt.Fprintf(os.Stderr, "lofi sync: manifest URL rejected: %v\n", err)
		default:
			fmt.Fprintf(os.Stderr, "lofi sync: %v\n", err)
		}
		return &commandError{code: 1}
	}

	lastSyncErr = nil
	fmt.Fprintf(os.Stderr, "lofi sync: cached %s\n", url)
	return nil
}
