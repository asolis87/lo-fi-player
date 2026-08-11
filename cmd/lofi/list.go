package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// runList implements `lofi list [--json]`. It loads the catalog
// from the XDG cache dir (REQ-CLI-1), prints a tabular listing
// sorted by track id by default, and emits JSON when --json is
// supplied. A missing cache is non-fatal in the sense that we
// print a `lofi sync` hint and exit 1 instead of crashing.
func runList(args []string) error {
	jsonMode := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonMode = true
		default:
			fmt.Fprintf(os.Stderr, "lofi list: unexpected argument %q\n\n%s", arg, usage)
			return &commandError{code: 2}
		}
	}

	cacheRoot, err := catalogCacheDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lofi list: cannot locate cache dir: %v\n", err)
		return &commandError{code: 1}
	}

	cat, err := catalog.LoadFromDir(cacheRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "lofi list: no catalog available; run `lofi sync` first")
		} else {
			fmt.Fprintf(os.Stderr, "lofi list: load catalog: %v\n", err)
		}
		return &commandError{code: 1}
	}

	if jsonMode {
		return encodeListJSON(os.Stdout, cat)
	}
	return printListTable(os.Stdout, cat)
}

// encodeListJSON writes cat to w as indented JSON. Errors are
// surfaced to the caller so runList can wrap them with a stderr
// diagnostic and exit 1.
func encodeListJSON(w io.Writer, cat *catalog.Catalog) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cat); err != nil {
		return fmt.Errorf("lofi list: encode catalog: %w", err)
	}
	return nil
}

// printListTable renders cat as a fixed-column table. Tracks are
// already sorted by id (LoadFromDir enforces it); tabwriter handles
// padding so the output stays readable for the longest shipped id.
func printListTable(w io.Writer, cat *catalog.Catalog) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tTITLE\tARTIST\tLICENSE"); err != nil {
		return err
	}
	for _, tr := range cat.Tracks {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", tr.ID, tr.Title, tr.Artist, tr.License); err != nil {
			return err
		}
	}
	return tw.Flush()
}