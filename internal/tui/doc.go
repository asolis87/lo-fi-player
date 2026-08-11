// Package tui hosts the Bubble Tea program that drives the lo-fi
// player's interactive mode. It owns the keymap, the four views
// (Now-Playing, Queue, Catalog, Attribution), and the non-fatal
// error banner triggered by audio backend events.
//
// The package imports only the AudioBackend port (internal/audio)
// and the catalog types (internal/catalog); it never reaches into
// concrete adapters. The composition root (cmd/lofi) wires the
// real backend and catalog before handing the Model to
// tea.NewProgram.
package tui
