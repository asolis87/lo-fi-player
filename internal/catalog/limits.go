// Tabla compacta de limites por asset class para la descarga
// del catalogo v1 (PR-2B, tareas 2.5–2.6). La API es
// deliberadamente pequena: cada uno de los cuatro asset names
// del catalogo v1 — manifest.json, track.json, audio.mp3,
// LICENSE.txt — mapea a un max bytes con techo distante del
// tamano real para que un servidor hostil o buggy no pueda
// agotar memoria. Nombres fuera del conjunto MUST rechazar
// con errUnknownAssetName para impedir que un caller baje
// bytes no catalogados.
//
// maxManifestBytes vivia antes como constante privada en
// syncer.go; ahora reside aqui — unica fuente de verdad
// para el limite de manifest.
package catalog

import (
	"errors"
	"fmt"
)

// maxManifestBytes tope para manifest.json. 8 MiB esta muy
// por encima del manifest shipped y muy por debajo de
// cualquier cosa que importe para memoria de proceso.
const maxManifestBytes int64 = 8 * 1024 * 1024

// maxTrackBytes y maxLicenseBytes comparten el mismo techo
// (1 MiB): track.json y LICENSE.txt son pequenos en la
// practica. Se nombran como dos constantes para que un
// ajuste futuro no se haga por confusion.
const (
	maxTrackBytes   int64 = 1 * 1024 * 1024
	maxLicenseBytes int64 = 1 * 1024 * 1024
)

// maxAudioBytes tope por pista. 32 MiB cubre holgadamente
// cualquier MP3 razonable del catalogo.
const maxAudioBytes int64 = 32 * 1024 * 1024

// errUnknownAssetName se envuelve sobre cada maxBytesFor
// cuyo name no esta en el conjunto permitido. Distinto de
// ErrInvalidAssetID: este error es especifico al lookup de
// limites, no a la validacion de URLs.
var errUnknownAssetName = errors.New("catalog: unknown asset name for limits")

// maxBytesFor retorna el max bytes permitido para un asset
// name del catalogo v1. Nombres fuera del conjunto
// (manifest.json, track.json, audio.mp3, LICENSE.txt) MUST
// envolver errUnknownAssetName sin retornar 0 ni un valor
// por defecto.
func maxBytesFor(name string) (int64, error) {
	switch name {
	case "manifest.json":
		return maxManifestBytes, nil
	case "track.json":
		return maxTrackBytes, nil
	case "audio.mp3":
		return maxAudioBytes, nil
	case "LICENSE.txt":
		return maxLicenseBytes, nil
	}
	return 0, fmt.Errorf("%w: name=%q", errUnknownAssetName, name)
}
