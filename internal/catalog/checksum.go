package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrChecksumMismatch is wrapped around every Verify failure whose
// root cause is a SHA-256 digest mismatch. Callers match with
// errors.Is to surface one actionable error class to the user.
var ErrChecksumMismatch = errors.New("catalog: checksum mismatch")

// Verify streams path through crypto/sha256.New() via io.Copy
// (memory bounded by the hashing buffer, not file size — required
// for tens-of-MB MP3s) and compares the lowercase hex digest
// against expectedSHA256 case-insensitively. Returns nil on match,
// an error wrapping ErrChecksumMismatch on mismatch (message
// includes the path and both hex values), or the underlying
// os/io error otherwise.
func Verify(path string, expectedSHA256 string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("catalog: open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("catalog: read %s: %w", path, err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if strings.EqualFold(actual, expectedSHA256) {
		return nil
	}
	return fmt.Errorf("%w: path=%s expected=%s actual=%s",
		ErrChecksumMismatch, path, strings.ToLower(expectedSHA256), actual)
}