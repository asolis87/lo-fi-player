package main

import (
	"github.com/asolis87/lo-fi-player/internal/catalog"
)

// filterHistoryAgainstCatalog descarta IDs del historial MRU que
// no existen en el catalogo cargado (FILTER-1 + FILTER-2). Es
// una funcion pura: opera sobre slices y el catalog; no toca I/O,
// no muta el catalogo y no lee stdin. La idea es que pueda ser
// reutilizada por el TUI en futuras slices sin arrastrar la
// dependencia de cmd/lofi. El volumen es independiente del
// filtro: el caller sigue hidratando effective volume por su
// cuenta. Cat nil => historial vacio para mantener startup
// idempotente cuando el loader fallo (RESUME-2 + CATALOG-1).
func filterHistoryAgainstCatalog(history []string, cat *catalog.Catalog) []string {
	if cat == nil || len(cat.Tracks) == 0 {
		return []string{}
	}
	index := make(map[string]struct{}, len(cat.Tracks))
	for _, tr := range cat.Tracks {
		index[tr.ID] = struct{}{}
	}
	out := make([]string, 0, len(history))
	for _, id := range history {
		if _, ok := index[id]; !ok {
			continue
		}
		out = append(out, id)
	}
	return out
}
