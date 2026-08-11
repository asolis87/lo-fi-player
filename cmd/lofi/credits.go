package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/credits"
)

type commandError struct {
	code int
}

func (e *commandError) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }

func codeFor(err error) int {
	if err == nil {
		return 0
	}
	var commandErr *commandError
	if errors.As(err, &commandErr) {
		return commandErr.code
	}
	return 1
}

func runCredits(args []string) error {
	jsonMode := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonMode = true
		default:
			fmt.Fprintf(os.Stderr, "lofi credits: unexpected argument %q\n\n%s", arg, usage)
			return &commandError{code: 2}
		}
	}

	catalogRoot, err := creditsCatalogRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lofi credits: cannot locate catalog: %v\n", err)
		return &commandError{code: 1}
	}
	cat, err := catalog.LoadFromDir(catalogRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "lofi credits: no catalog available; run `lofi sync` first")
		} else {
			fmt.Fprintf(os.Stderr, "lofi credits: load catalog: %v\n", err)
		}
		return &commandError{code: 1}
	}

	if jsonMode {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(cat); err != nil {
			fmt.Fprintf(os.Stderr, "lofi credits: encode catalog: %v\n", err)
			return &commandError{code: 1}
		}
		return nil
	}
	fmt.Fprint(os.Stdout, credits.FormatNotice(cat))
	return nil
}

func creditsCatalogRoot() (string, error) {
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
