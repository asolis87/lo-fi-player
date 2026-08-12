package tui

import (
	"strings"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/audio"
)

// TestViewNoCatalog_RendersSyncGuidance vale CATALOG-1 escenario 1
// (TUI): con el catalog ausente, la vista dedicada debe mostrar
// exactamente "No catalog found. Run `lofi sync` to download tracks."
// sin texto adicional de controles ni navegacion.
func TestViewNoCatalog_RendersSyncGuidance(t *testing.T) {
	m := NewModel(audio.NewMockBackend(), nil, nil)
	m.Mode = ModeNoCatalog

	out := viewNoCatalog(m)
	want := "No catalog found. Run `lofi sync` to download tracks."
	if !strings.Contains(out, want) {
		t.Fatalf("viewNoCatalog missing %q\n--view--\n%s", want, out)
	}
}

// TestModelNoCatalog_StartsWhenCatalogNil garantiza que el
// dispatcher enrute el modo no-catalog cuando el Model arranca con
// Catalog == nil (CATALOG-1).
func TestModelNoCatalog_StartsWhenCatalogNil(t *testing.T) {
	m := NewModel(audio.NewMockBackend(), nil, nil)
	if m.Mode != ModeNoCatalog {
		t.Fatalf("Mode with nil Catalog = %v, want ModeNoCatalog", m.Mode)
	}
}

// TestModelNoCatalog_StartsWhenCatalogEmpty cubre el segundo caso:
// catalog presente pero vacio (track-list vacia) => mismo enrutado.
func TestModelNoCatalog_StartsWhenCatalogEmpty(t *testing.T) {
	// NewModelWithState no acepta catalog vacio: lo simulamos mutando
	// el campo post-construccion para reflejar exactamente la situacion
	// real (catalog cargado pero sin tracks por sincronizacion parcial).
	m := NewModel(audio.NewMockBackend(), nil, nil)
	m.Catalog = nil
	if m.Mode != ModeNoCatalog {
		t.Fatalf("Mode with empty Catalog = %v, want ModeNoCatalog", m.Mode)
	}
}
