// Package license guards the repository-level LICENSE file. The
// repo license is a single binary decision (MIT per decision #326)
// that subsequent dependency audits and redistribution will rely
// on; this test exists so any accidental edit to the LICENSE
// file (or a missing file at all) breaks the build immediately
// rather than slipping past review.
package license

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot resolves the repository root from the test's runtime
// directory. license_test.go lives under internal/license/, so the
// repo root is two levels up. The path is computed via filepath.Abs
// so the test works regardless of the working directory the
// `go test` invocation was invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	return abs
}

// TestLicenseFile_ContainsMITText is the Phase 6.1 acceptance gate
// for the MIT license decision (#326). The repo LICENSE MUST
// contain the canonical MIT text so downstream tooling and
// redistribution can recognize the license without a side-channel
// manifest. We assert the four canonical phrases that uniquely
// identify the MIT license text rather than just any license.
func TestLicenseFile_ContainsMITText(t *testing.T) {
	path := filepath.Join(repoRoot(t), "LICENSE")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read LICENSE at %s: %v", path, err)
	}
	text := string(data)

	// Canonical MIT phrases. These four strings are mandated by the
	// MIT license template (spdx.org/licenses/MIT.html) and are
	// enough to distinguish MIT from Apache-2.0, BSD-2-Clause,
	// ISC, and the other short permissive licenses the repo might
	// plausibly be confused with.
	required := []string{
		"MIT License",
		"Copyright (c) 2026",
		"Permission is hereby granted, free of charge",
		`THE SOFTWARE IS PROVIDED "AS IS"`,
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Errorf("LICENSE missing canonical MIT phrase %q", phrase)
		}
	}
}
