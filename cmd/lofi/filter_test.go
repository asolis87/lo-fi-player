package main

import (
	"reflect"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// TestFilterHistoryAgainstCatalog_AllPresent verifica el camino
// feliz: si todos los IDs existen en el catalogo, el filtro los
// preserva en el orden MRU (FILTER-1 escenario parcial).
func TestFilterHistoryAgainstCatalog_AllPresent(t *testing.T) {
	cat := catalogForIDs(t, "A", "B", "C")
	in := []string{"A", "B", "C"}
	got := filterHistoryAgainstCatalog(in, cat)
	if !reflect.DeepEqual(got, []string{"A", "B", "C"}) {
		t.Fatalf("got %v, want %v", got, []string{"A", "B", "C"})
	}
}

// TestFilterHistoryAgainstCatalog_PartialAndUnknown verifica
// FILTER-1 escenario parcial + FILTER-2: IDs desconocidos y
// parcialmente faltantes se descartan sin tocar el resto.
func TestFilterHistoryAgainstCatalog_PartialAndUnknown(t *testing.T) {
	cat := catalogForIDs(t, "A", "C")
	// B esta en el catalogo (no queremos confundir con "unknown"),
	// D no esta.
	in := []string{"A", "B", "X", "C", "D"}
	got := filterHistoryAgainstCatalog(in, cat)
	want := []string{"A", "C"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestFilterHistoryAgainstCatalog_AllMissing verifica FILTER-1
// segundo escenario: historial completo ausente => historial
// vacio, sin error y volumen independiente.
func TestFilterHistoryAgainstCatalog_AllMissing(t *testing.T) {
	cat := catalogForIDs(t, "Z")
	in := []string{"X", "Y", "W"}
	got := filterHistoryAgainstCatalog(in, cat)
	if len(got) != 0 {
		t.Fatalf("got %v, want empty slice", got)
	}
}

// TestFilterHistoryAgainstCatalog_NilCatalogSafe: con catalog
// nil el filtro degrada a slice vacio sin panic, alineado con
// RESUME-2 (no-TTY) que nunca debe fallar por catalog ausente.
func TestFilterHistoryAgainstCatalog_NilCatalogSafe(t *testing.T) {
	got := filterHistoryAgainstCatalog([]string{"A", "B"}, nil)
	if len(got) != 0 {
		t.Fatalf("got %v, want empty slice", got)
	}
}

// catalogForIDs construye un catalog minimo con los IDs dados.
// Sirve como fixture local sin tocar disco.
func catalogForIDs(t *testing.T, ids ...string) *catalog.Catalog {
	t.Helper()
	tracks := make([]catalog.Track, 0, len(ids))
	for _, id := range ids {
		tracks = append(tracks, catalog.Track{ID: id})
	}
	return &catalog.Catalog{Tracks: tracks}
}
