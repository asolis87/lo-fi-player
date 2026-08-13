// Clasificador HTTP usado por el sync para decidir si un
// fallo de descarga amerita reintento. NO implementa la
// politica de reintentos en si (vive en retry.go, PR-6);
// solo mapea codigo de status a transitorio / permanente
// preservando el codigo en el mensaje para que las capas
// superiores puedan registrar y ramificar.
package catalog

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrTransient se envuelve sobre cada fallo de
// ClassifyStatus cuyo codigo (408, 429, 5xx) senala que el
// caller puede reintentar tras backoff acotado.
var ErrTransient = errors.New("catalog: transient HTTP status")

// ErrPermanent se envuelve sobre cada fallo de
// ClassifyStatus cuyo codigo (4xx distintos de 408 y 429)
// senala que el caller MUST NO reintentar.
var ErrPermanent = errors.New("catalog: permanent HTTP status")

// transientStatusCodes lista los unicos codigos 4xx que la
// politica de reintentos trata como transitorios. 5xx se
// cubre por rango en ClassifyStatus para no enumerar cada
// codigo individualmente.
var transientStatusCodes = map[int]struct{}{
	http.StatusRequestTimeout:  {}, // 408
	http.StatusTooManyRequests: {}, // 429
}

// isTransient reporta si code cae dentro de la politica
// transitoria: 408/429 puntuales o cualquier 5xx.
func isTransient(code int) bool {
	if _, ok := transientStatusCodes[code]; ok {
		return true
	}
	return code >= 500 && code < 600
}

// ClassifyStatus mapea un codigo HTTP a la clase de error
// que la politica de reintentos consume:
//
//   - 2xx → nil (la peticion tuvo exito, no hay error).
//   - 408, 429, 5xx → ErrTransient preservando code.
//   - resto de 4xx → ErrPermanent preservando code.
//   - 1xx / 3xx u otro codigo no clasificado →
//     ErrPermanent preservando code (caller no debe reintentar).
//
// El codigo MUST quedar embebido en el mensaje porque PR-2
// y PR-6 registran y ramifican a partir del error que el
// clasificador retorna.
func ClassifyStatus(code int) error {
	switch {
	case code >= 200 && code < 300:
		return nil
	case isTransient(code):
		return fmt.Errorf("%w: status=%d", ErrTransient, code)
	case code >= 400 && code < 500:
		return fmt.Errorf("%w: status=%d", ErrPermanent, code)
	}
	return fmt.Errorf("%w: status=%d (unclassified)", ErrPermanent, code)
}
