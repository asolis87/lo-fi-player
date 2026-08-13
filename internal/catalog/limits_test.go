// Tests del lookup de limites por asset class (PR-2B, tareas
// 2.5–2.7). Cubre tres contratos:
//
//   - Per-asset-class: track.json=1 MiB, LICENSE.txt=1 MiB,
//     audio.mp3=32 MiB. Verifica tamano exacto en bytes (no
//     solo magnitud) para impedir que un refactor accidental
//     cambie los topes.
//   - Manifest constant: 8 MiB tanto via maxBytesFor como
//     via la constante module-level maxManifestBytes.
//   - Unknown rejected: cualquier name fuera del conjunto
//     MUST envolver errUnknownAssetName.
package catalog

import (
	"errors"
	"testing"
)

// TestLimits_PerAssetClass (tarea 2.5): para cada asset
// sub-archivado (track.json, audio.mp3, LICENSE.txt) del
// catalogo v1, maxBytesFor retorna el max bytes exacto
// declarado en el spec. Tabla drives asserts exactos (no
// aproximaciones) para impedir que un refactor accidental
// cambie los topes.
func TestLimits_PerAssetClass(t *testing.T) {
	cases := []struct {
		name string
		want int64
	}{
		{"track.json", 1 * 1024 * 1024},
		{"LICENSE.txt", 1 * 1024 * 1024},
		{"audio.mp3", 32 * 1024 * 1024},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := maxBytesFor(c.name)
			if err != nil {
				t.Fatalf("maxBytesFor(%q) err = %v, quiere nil", c.name, err)
			}
			if got != c.want {
				t.Fatalf("maxBytesFor(%q) = %d, quiere %d", c.name, got, c.want)
			}
		})
	}
}

// TestLimits_ManifestConstant (tarea 2.5): la constante
// module-level maxManifestBytes MUST ser 8 MiB y maxBytesFor
// MUST retornar el mismo valor para "manifest.json". Esto
// cierra el invariante de "una sola fuente de verdad" para
// el limite de manifest.
func TestLimits_ManifestConstant(t *testing.T) {
	const want int64 = 8 * 1024 * 1024
	if maxManifestBytes != want {
		t.Fatalf("maxManifestBytes = %d, quiere %d", maxManifestBytes, want)
	}
	got, err := maxBytesFor("manifest.json")
	if err != nil {
		t.Fatalf("maxBytesFor(manifest.json) err = %v, quiere nil", err)
	}
	if got != maxManifestBytes {
		t.Fatalf("maxBytesFor(manifest.json) = %d, quiere maxManifestBytes (%d)", got, maxManifestBytes)
	}
}

// TestLimits_UnknownAssetRejected (tarea 2.5): maxBytesFor
// MUST envolver errUnknownAssetName para cualquier name fuera
// del conjunto permitido. Cierra la regla "no bajamos bytes
// no catalogados".
func TestLimits_UnknownAssetRejected(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"path_traversal":  "../track.json",
		"wrong_extension": "track.yaml",
		"audio_wav":       "audio.wav",
		"manifest_upper":  "Manifest.json",
		"license_lower":   "license.txt",
		"audio_upper":     "audio.MP3",
		"nested":          "nested/track.json",
		"random":          "secret.pdf",
	}
	for name, assetName := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := maxBytesFor(assetName)
			if err == nil {
				t.Fatalf("maxBytesFor(%q) = nil, quiere error", assetName)
			}
			if !errors.Is(err, errUnknownAssetName) {
				t.Fatalf("maxBytesFor(%q) error = %v, quiere envolver errUnknownAssetName", assetName, err)
			}
		})
	}
}
