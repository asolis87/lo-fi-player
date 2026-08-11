package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChecksum_VerifiesStreamedFile: a 1 MiB+1 payload (size large
// enough to defeat a naive "buffer the whole file" shortcut, with a
// trailing sentinel byte so the hash differs from all-zero) MUST
// verify when the expected hex matches.
func TestChecksum_VerifiesStreamedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	payload := make([]byte, 1024*1024+1)
	payload[len(payload)-1] = 0x42
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("seed payload: %v", err)
	}
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])

	if err := Verify(path, want); err != nil {
		t.Fatalf("Verify(correct hash) = %v, want nil", err)
	}
}

// TestChecksum_MismatchReturnsError: a wrong hex MUST produce an
// error wrapping ErrChecksumMismatch with the path in the message.
func TestChecksum_MismatchReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("seed payload: %v", err)
	}
	const bogus = "0000000000000000000000000000000000000000000000000000000000000000"

	err := Verify(path, bogus)
	if err == nil {
		t.Fatal("Verify(wrong hash) = nil, want error")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want wrapped ErrChecksumMismatch", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error %q lacks path %s", err.Error(), path)
	}
}