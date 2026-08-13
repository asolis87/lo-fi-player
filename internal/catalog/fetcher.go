// Streaming fetcher + SHA-256 sobre la marcha (PR-2A,
// tareas 2.1–2.4). fetchStreaming conecta la respuesta
// HTTP, escribe el body a dest (O_CREATE|O_EXCL|O_WRONLY,
// 0o600) y hashea cada byte en la misma pasada io.Copy. El
// limite de bytes se aplica via io.LimitReader(_, maxBytes+1)
// para detectar overflow sin un segundo reader. Cualquier
// error MUST cerrar y remover el destino parcial; un destino
// preexistente MUST quedar byte-identical porque O_EXCL
// rechaza la apertura. El clasificador de status existente
// (ErrTransient / ErrPermanent) se envuelve con %w; este
// primitive NO reintenta (PR-6 owns retry).
package catalog

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
)

// ErrAssetTooLarge se envuelve sobre cada fetchStreaming cuyo
// body excede maxBytes. Distinto de ErrTransient porque un
// cuerpo sobredimensionado es control del servidor, no blip.
var ErrAssetTooLarge = errors.New("catalog: asset exceeds max bytes")

// ErrFetchFailed es el umbrella para fallos no-status (red,
// filesystem, nil param, hash, copy). Los errores de status
// se propagan via %w sobre ErrTransient / ErrPermanent.
var ErrFetchFailed = errors.New("catalog: fetch failed")

// FetchResult holds the observed hash and total bytes copied
// after a successful fetchStreaming. SHA256Hex is lowercase
// hex; Bytes equals the wire payload length.
type FetchResult struct {
	SHA256Hex string
	Bytes     int64
}

// fetchStreaming streams the body of req to destPath (created
// with O_CREATE|O_EXCL|O_WRONLY, mode 0o600). Every byte is
// hashed through hasher on the same io.Copy. Status codes are
// classified via ClassifyStatus before the destination is
// opened so a non-2xx response never leaves a partial file.
// maxBytes MUST be > 0; the caller sources it from a per-asset
// table (PR-2B). Limit is strict: exactly maxBytes succeeds,
// maxBytes+1 triggers ErrAssetTooLarge.
func fetchStreaming(ctx context.Context, client *http.Client, req *http.Request, destPath string, maxBytes int64, hasher hash.Hash) (FetchResult, error) {
	_ = ctx
	if req == nil || client == nil || hasher == nil {
		return FetchResult{}, fmt.Errorf("%w: nil request/client/hasher", ErrFetchFailed)
	}
	if maxBytes <= 0 {
		return FetchResult{}, fmt.Errorf("%w: maxBytes=%d", ErrFetchFailed, maxBytes)
	}
	resp, err := client.Do(req)
	if err != nil {
		return FetchResult{}, fmt.Errorf("%w: GET %s: %v", ErrFetchFailed, req.URL, err)
	}
	defer resp.Body.Close()
	if clsErr := ClassifyStatus(resp.StatusCode); clsErr != nil {
		return FetchResult{}, fmt.Errorf("%w: GET %s: %w", ErrFetchFailed, req.URL, clsErr)
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return FetchResult{}, fmt.Errorf("%w: open %s: %v", ErrFetchFailed, destPath, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = f.Close()
			_ = os.Remove(destPath)
		}
	}()
	n, copyErr := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(resp.Body, maxBytes+1))
	if copyErr != nil {
		return FetchResult{}, fmt.Errorf("%w: copy: %v", ErrFetchFailed, copyErr)
	}
	if n > maxBytes {
		return FetchResult{}, fmt.Errorf("%w: limit=%d observed=%d", ErrAssetTooLarge, maxBytes, n)
	}
	if err := f.Sync(); err != nil {
		return FetchResult{}, fmt.Errorf("%w: fsync: %v", ErrFetchFailed, err)
	}
	if err := f.Close(); err != nil {
		return FetchResult{}, fmt.Errorf("%w: close: %v", ErrFetchFailed, err)
	}
	committed = true
	return FetchResult{SHA256Hex: hex.EncodeToString(hasher.Sum(nil)), Bytes: n}, nil
}
