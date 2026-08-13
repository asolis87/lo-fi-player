// Tests para los primitives de URL inmutable del catalogo (PR-1,
// tareas 1.1–1.5). Cubren AssetURL y ValidateAssetID con
// tres grupos: ids invalidos, asset names invalidos, y SHA
// malformado. El caso feliz vive en un cuarto test que itera
// los tres asset names permitidos para garantizar que la union
// de base+sha+id+name nunca escapa el prefijo catalog/v1/.
package catalog

import (
	"errors"
	"strings"
	"testing"
)

// pinnedBase es el prefijo fijo del host raw.githubusercontent.com
// que AssetURL consume. Tests que ejercitan AssetURL no
// invocan ValidateURL — el contrato de AssetURL es estricto
// respecto al SHA, id y asset name; la base se asume honesta
// porque la fija el binario al compilar tiempo.
const pinnedBase = "https://raw.githubusercontent.com/asolis87/lo-fi-player"

// assertInvalidAssetURL corre AssetURL con los argumentos
// dados y afirma el contrato de fallo: error no nulo, error
// envolviendo ErrInvalidAssetID y URL devuelta vacia. La
// URL vacia es parte del contrato porque AssetURL nunca
// debe regresar un prefijo parcial cuando falla.
func assertInvalidAssetURL(t *testing.T, base, sha, id, name, msg string) {
	t.Helper()
	url, err := AssetURL(base, sha, id, name)
	if err == nil {
		t.Fatalf("%s: AssetURL devolvio (%q, nil), quiere error", msg, url)
	}
	if !errors.Is(err, ErrInvalidAssetID) {
		t.Fatalf("%s: error = %v, quiere envolver ErrInvalidAssetID", msg, err)
	}
	if url != "" {
		t.Fatalf("%s: AssetURL devolvio url=%q, quiere cadena vacia", msg, url)
	}
}

// TestAssetURL_RejectsInvalidID cubre la regla de slug: el id
// de pista debe ser secuencia de [a-z0-9] separada por un solo
// guion. Cualquier desviacion (.., espacios, acentos, mayusculas,
// guiones al inicio/final/duplicados, barras, dos puntos, signos)
// o cadena vacia MUST envolver ErrInvalidAssetID antes de
// construir cualquier URL.
func TestAssetURL_RejectsInvalidID(t *testing.T) {
	cases := map[string]string{
		"empty":                 "",
		"double_dot":            "..",
		"id_with_dot":           "track.001",
		"id_with_space":         "foo bar",
		"id_with_accent":        "café",
		"id_with_uppercase":     "Track-001",
		"id_with_slash":         "foo/bar",
		"leading_hyphen":        "-foo",
		"trailing_hyphen":       "foo-",
		"double_hyphen":         "foo--bar",
		"colon":                 "foo:bar",
		"question_mark":         "foo?bar",
		"hash":                  "foo#bar",
		"percent_encoded_space": "foo%20bar",
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			assertInvalidAssetURL(t, pinnedBase, pinnedSHA, id, "track.json", "id invalido="+id)
		})
	}
}

// TestAssetURL_RejectsInvalidName cubre el conjunto fijo de
// asset names (track.json, audio.mp3, LICENSE.txt). Cualquier
// otro nombre, vacio, mayusculas distintas, prefijos
// traversal o extensiones distintas MUST envolver
// ErrInvalidAssetID para impedir que la cache local baje
// bytes fuera del catalogo.
func TestAssetURL_RejectsInvalidName(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"wrong_extension": "track.yaml",
		"audio_upper":     "audio.MP3",
		"license_lower":   "license.txt",
		"path_traversal":  "../track.json",
		"nested":          "nested/track.json",
		"audio_wav":       "audio.wav",
		"track_extra_dot": "track..json",
	}
	for name, assetName := range cases {
		t.Run(name, func(t *testing.T) {
			assertInvalidAssetURL(t, pinnedBase, pinnedSHA, "track-001", assetName, "name invalido="+assetName)
		})
	}
}

// TestAssetURL_RejectsInvalidSHA cubre la regla de pinning:
// el SHA MUST ser exactamente 40 chars hex lowercase (misma
// regexp que pinning.pinnedSHARe). AssetURL MUST rechazar
// cualquier desviacion con ErrInvalidAssetID para que el
// caller no termine apuntando a un ref flotante.
func TestAssetURL_RejectsInvalidSHA(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"too_short":   "abcdef",
		"uppercase":   "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
		"non_hex":     "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
		"too_long":    "abcdef0123456789abcdef0123456789abcdef0100",
		"placeholder": "<PLACEHOLDER_SHA>",
	}
	for name, sha := range cases {
		t.Run(name, func(t *testing.T) {
			assertInvalidAssetURL(t, pinnedBase, sha, "track-001", "track.json", "sha invalido="+sha)
		})
	}
}

// TestAssetURL_HappyPath_AllAssets cubre el caso feliz: para
// cada asset name permitido, AssetURL construye la URL exacta
// bajo catalog/v1/ con el id validado. La comparacion byte a
// byte garantiza que el orden de campos y los separadores son
// reproducibles (decision #289 sobre identidad inmutable).
func TestAssetURL_HappyPath_AllAssets(t *testing.T) {
	const id = "lofi-lion-tame-the-beast"
	cases := []struct {
		name string
		want string
	}{
		{"track.json", pinnedBase + "/" + pinnedSHA + "/catalog/v1/" + id + "/track.json"},
		{"audio.mp3", pinnedBase + "/" + pinnedSHA + "/catalog/v1/" + id + "/audio.mp3"},
		{"LICENSE.txt", pinnedBase + "/" + pinnedSHA + "/catalog/v1/" + id + "/LICENSE.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := AssetURL(pinnedBase, pinnedSHA, id, c.name)
			if err != nil {
				t.Fatalf("AssetURL(caso feliz): %v", err)
			}
			if got != c.want {
				t.Fatalf("AssetURL(caso feliz) = %q, quiere %q", got, c.want)
			}
			if !strings.HasPrefix(got, pinnedBase+"/"+pinnedSHA+"/catalog/v1/") {
				t.Fatalf("AssetURL(caso feliz) = %q, quiere prefijo catalog/v1/", got)
			}
		})
	}
}
