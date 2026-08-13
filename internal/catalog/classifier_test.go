// Tests del clasificador HTTP usado por el sync (PR-1, tareas
// 1.3–1.5). Cubre la politica de reintentos: 408, 429 y 5xx
// son transitorios (ErrTransient); el resto de 4xx son
// permanentes (ErrPermanent); 2xx no produce error. El
// mensaje del error envuelto MUST preservar el codigo de
// status para que PR-2 / PR-6 puedan registrar y ramificar
// sin re-derivar la respuesta HTTP.
package catalog

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// TestClassifyStatus es la unica funcion de test del
// clasificador (el regex focal '^TestClassifyStatus$' exige
// coincidencia exacta). Cubre todos los codigos que PR-6
// ramifica explicitamente segun la matriz de amenazas del
// diseno (R3 S9–S11).
func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		wantErr error
	}{
		{"200_ok", 200, nil},
		{"204_no_content", 204, nil},
		{"400_bad_request", 400, ErrPermanent},
		{"401_unauthorized", 401, ErrPermanent},
		{"403_forbidden", 403, ErrPermanent},
		{"404_not_found", 404, ErrPermanent},
		{"410_gone", 410, ErrPermanent},
		{"408_request_timeout", 408, ErrTransient},
		{"429_too_many_requests", 429, ErrTransient},
		{"500_internal_server_error", 500, ErrTransient},
		{"502_bad_gateway", 502, ErrTransient},
		{"503_service_unavailable", 503, ErrTransient},
		{"504_gateway_timeout", 504, ErrTransient},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ClassifyStatus(c.code)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("ClassifyStatus(%d) = %v, quiere nil para 2xx", c.code, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ClassifyStatus(%d) = nil, quiere envolver %v", c.code, c.wantErr)
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("ClassifyStatus(%d) = %v, quiere envolver %v", c.code, err, c.wantErr)
			}
			// Preservar la evidencia del codigo en el mensaje
			// para que PR-2 / PR-6 puedan registrar y ramificar
			// sin re-derivar de la respuesta HTTP.
			if !strings.Contains(err.Error(), strconv.Itoa(c.code)) {
				t.Fatalf("ClassifyStatus(%d) mensaje %q no incluye el codigo de status", c.code, err.Error())
			}
		})
	}
}
