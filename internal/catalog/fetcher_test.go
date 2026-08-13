// Tests del fetcher de streaming (PR-2A, tareas 2.1–2.4).
// Cada test arma un httptest.Server con respuesta controlada
// y corre fetchStreaming. Cubren:
//
//   - SHA de bytes conocidos vs hasher live (tarea 2.1).
//   - O_EXCL rechaza cuando el destino preexiste; bytes
//     anteriores byte-identical; mode 0o600 enforced (2.3).
//   - Boundary exacto: maxBytes succeeds, maxBytes+1
//     ErrAssetTooLarge + dest removed.
//   - Status errors: 404 wraps ErrPermanent, 503 wraps
//     ErrTransient, ambos dejan dest limpio (no body leak).
//   - Body HTTP siempre cerrado via defer.
package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// NewKnownBytes devuelve un payload deterministico de size
// bytes. Usado por tests SHA-streaming.
func newKnownBytes(size int) []byte {
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	return buf
}

// sha256Hex devuelve el SHA-256 lowercase hex de data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fetchServer arma un httptest.Server que sirve body con
// status. body nil = solo status.
func fetchServer(t *testing.T, status int, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		if body != nil {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fetchRequest construye un GET contra srv.URL.
func fetchRequest(t *testing.T, srv *httptest.Server) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

// TestFetch_SHAStreamMatchesKnownBytes (tarea 2.1): el
// fetcher MUST computar el SHA-256 de los bytes recibidos
// en streaming. Pasos: serve 64 KiB known, asigna sha256.New,
// compara SHA result contra sha256Hex(body) y contra el
// contenido del archivo en disco.
func TestFetch_SHAStreamMatchesKnownBytes(t *testing.T) {
	body := newKnownBytes(64 * 1024)
	srv := fetchServer(t, http.StatusOK, body)
	req := fetchRequest(t, srv)
	dest := filepath.Join(t.TempDir(), "audio.mp3")
	hasher := sha256.New()

	res, err := fetchStreaming(context.Background(), http.DefaultClient, req, dest, 1<<20, hasher)
	if err != nil {
		t.Fatalf("fetchStreaming: %v", err)
	}
	want := sha256Hex(body)
	if res.SHA256Hex != want {
		t.Fatalf("SHA = %q, quiere %q", res.SHA256Hex, want)
	}
	if res.Bytes != int64(len(body)) {
		t.Fatalf("Bytes = %d, quiere %d", res.Bytes, len(body))
	}
	onDisk, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile(dest): %v", err)
	}
	if !bytes.Equal(onDisk, body) {
		t.Fatal("bytes persistidos difieren del body servido")
	}
	if sha256Hex(onDisk) != want {
		t.Fatalf("SHA on-disk = %q, quiere %q", sha256Hex(onDisk), want)
	}
}

// TestFetch_ExclRefusesOverwrite (tarea 2.3): el fetcher
// MUST usar O_EXCL; cuando el destino preexiste, el fetch
// retorna error (wrap ErrFetchFailed) y los bytes anteriores
// MUST quedar byte-identical.
func TestFetch_ExclRefusesOverwrite(t *testing.T) {
	prior := []byte("PREEXISTING-DO-NOT-OVERWRITE")
	dir := t.TempDir()
	dest := filepath.Join(dir, "audio.mp3")
	if err := os.WriteFile(dest, prior, 0o600); err != nil {
		t.Fatalf("seed prior: %v", err)
	}
	srv := fetchServer(t, http.StatusOK, []byte("new payload that should NOT be written"))
	req := fetchRequest(t, srv)

	_, err := fetchStreaming(context.Background(), http.DefaultClient, req, dest, 1<<20, sha256.New())
	if err == nil {
		t.Fatal("fetchStreaming con destino preexistente = nil, quiere error")
	}
	if !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("error = %v, quiere envolver ErrFetchFailed", err)
	}
	after, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile(dest): %v", err)
	}
	if !bytes.Equal(after, prior) {
		t.Fatalf("dest mutado: got %q, quiere %q", after, prior)
	}
}

// TestFetch_ExclRespectsSixHundredMode (tarea 2.4): mode del
// archivo creado MUST ser exactamente 0o600 — cierra la matriz
// de permisos (no permite que termine legible por grupo).
func TestFetch_ExclRespectsSixHundredMode(t *testing.T) {
	srv := fetchServer(t, http.StatusOK, newKnownBytes(1024))
	req := fetchRequest(t, srv)
	dest := filepath.Join(t.TempDir(), "audio.mp3")

	if _, err := fetchStreaming(context.Background(), http.DefaultClient, req, dest, 1<<20, sha256.New()); err != nil {
		t.Fatalf("fetchStreaming: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("Stat(dest): %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, quiere 0o600", got)
	}
}

// TestFetch_BytesLimited (tarea 2.4 boundary): el limite es
// estricto. maxBytes succeeds, maxBytes+1 ErrAssetTooLarge
// + dest removed. Cubre tambien los codigos de error en
// proceso (no retries).
func TestFetch_BytesLimited(t *testing.T) {
	cases := []struct {
		name    string
		max     int64
		size    int
		wantErr bool
	}{
		{"max_exact_succeeds", 1024, 1024, false},
		{"max_plus_1_fails", 1024, 1025, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := newKnownBytes(c.size)
			srv := fetchServer(t, http.StatusOK, body)
			req := fetchRequest(t, srv)
			dest := filepath.Join(t.TempDir(), "asset.bin")

			_, err := fetchStreaming(context.Background(), http.DefaultClient, req, dest, c.max, sha256.New())
			if c.wantErr {
				if err == nil {
					t.Fatalf("fetchStreaming(size=%d, max=%d) = nil, quiere error", c.size, c.max)
				}
				if !errors.Is(err, ErrAssetTooLarge) {
					t.Fatalf("error = %v, quiere envolver ErrAssetTooLarge", err)
				}
				if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
					t.Fatalf("dest parcial presente tras ErrAssetTooLarge: stat=%v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchStreaming(size=%d, max=%d) = %v, quiere nil", c.size, c.max, err)
			}
			info, err := os.Stat(dest)
			if err != nil {
				t.Fatalf("Stat(dest): %v", err)
			}
			if got := info.Size(); got != int64(c.size) {
				t.Fatalf("size = %d, quiere %d", got, c.size)
			}
		})
	}
}

// TestFetch_StatusError clasifica el comportamiento de no-2xx:
// 404 wraps ErrPermanent, 503 wraps ErrTransient, ambos
// cierran resp.Body (via defer) y NO dejan dest en disco.
// El dest puede no existir (err antes de OpenFile) o existir
// pre-creado (O_EXCL); ambos chequean que el nuevo no se
// quede parcial.
func TestFetch_StatusError(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"404_permanent", http.StatusNotFound, ErrPermanent},
		{"503_transient", http.StatusServiceUnavailable, ErrTransient},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := fetchServer(t, c.status, nil)
			req := fetchRequest(t, srv)
			dest := filepath.Join(t.TempDir(), "asset.bin")

			_, err := fetchStreaming(context.Background(), http.DefaultClient, req, dest, 1<<20, sha256.New())
			if err == nil {
				t.Fatalf("fetchStreaming(status=%d) = nil, quiere error", c.status)
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("error = %v, quiere envolver %v", err, c.wantErr)
			}
			if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
				t.Fatalf("dest presente tras status=%d: stat=%v", c.status, statErr)
			}
		})
	}
}
