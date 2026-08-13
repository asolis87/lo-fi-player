// Constructor de URLs inmutables para los asset names del
// catalogo v1. Vive antes del fetcher (PR-2) para que el
// codigo de descarga reuse el mismo validador de id y no
// arme paths por concatenacion directa. AssetURL nunca usa
// source_url, ramas o query values; solo base cruda +
// SHA pinned + id slug + asset name fijo.
package catalog

import (
	"errors"
	"fmt"
	"regexp"
)

// ErrInvalidAssetID se envuelve sobre cada fallo de
// AssetURL / ValidateAssetID cuya causa raiz es: SHA que no
// son 40 hex lowercase, id de pista mal formado (vacio,
// contiene .., espacios, acentos, mayusculas, guiones al
// inicio/final/duplicados), o asset name fuera del conjunto
// permitido. Los callers matchean con errors.Is para que la
// superficie sea una sola clase accionable.
var ErrInvalidAssetID = errors.New("catalog: invalid asset id or name")

// assetIDRe enforce la regla de slug: secuencia de [a-z0-9]
// separada por un solo guion, sin guiones al inicio, final
// ni consecutivos. Se compila una vez al cargar el paquete
// para que el hot path de ValidateAssetID no pague el costo
// de compilacion por llamada.
var assetIDRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// allowedAssetNames enumera los tres nombres de archivo
// exactos que el catalogo MVP v1 publica por pista. Cualquier
// otro nombre MUST rechazarse para impedir que el caller baje
// bytes fuera del arbol catalog/v1/<id>/.
var allowedAssetNames = map[string]struct{}{
	"track.json":  {},
	"audio.mp3":   {},
	"LICENSE.txt": {},
}

// ValidateAssetID reporta nil cuando id cumple el patron de
// slug ^[a-z0-9]+(?:-[a-z0-9]+)*$. Cualquier otra entrada
// MUST envolver ErrInvalidAssetID con el valor recibido para
// que el caller pueda registrar el id sin re-derivar.
func ValidateAssetID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: id is empty", ErrInvalidAssetID)
	}
	if !assetIDRe.MatchString(id) {
		return fmt.Errorf("%w: id=%q", ErrInvalidAssetID, id)
	}
	return nil
}

// AssetURL ensambla la URL inmutable por pista a partir del
// prefijo fijo del host raw.githubusercontent.com/<owner>/<repo>,
// el SHA de 40 hex lowercase pinneado en build time, el id
// de pista validado por ValidateAssetID y uno de los tres
// asset names permitidos. Cualquier desviacion MUST envolver
// ErrInvalidAssetID sin retornar URL parcial.
func AssetURL(base, commitSHA, id, name string) (string, error) {
	if !pinnedSHARe.MatchString(commitSHA) {
		return "", fmt.Errorf("%w: sha=%q is not 40 lowercase hex chars", ErrInvalidAssetID, commitSHA)
	}
	if err := ValidateAssetID(id); err != nil {
		return "", err
	}
	if _, ok := allowedAssetNames[name]; !ok {
		return "", fmt.Errorf("%w: name=%q not in {track.json, audio.mp3, LICENSE.txt}", ErrInvalidAssetID, name)
	}
	return fmt.Sprintf("%s/%s/catalog/v1/%s/%s", base, commitSHA, id, name), nil
}
