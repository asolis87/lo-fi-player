// Package audio defines the audio port and the test doubles that
// downstream callers use to drive the player without a real backend.
//
// Architectural rule: this package is the ONLY place in the codebase
// allowed to mention a concrete audio adapter (mpv, oto, etc.). The
// CLI and TUI layers communicate with audio exclusively through the
// AudioBackend interface defined here. Any code outside this package
// that imports an adapter is a layering violation and must be moved
// behind this port.
package audio

import "errors"

// ErrVolumeOutOfRange is returned by backends when SetVolume receives
// a value outside the documented [0, 100] range. Concrete adapters
// MUST return the same sentinel so callers can match on errors.Is.
var ErrVolumeOutOfRange = errors.New("audio: volume out of range")

// Track is the minimal descriptor the audio backend needs to load a
// playable item. Adapters translate catalog.Track into audio.Track at
// the boundary; the port never imports the catalog package.
type Track struct {
	ID   string
	Path string
}

// AudioBackend is the only audio port the CLI and TUI are allowed to
// depend on. Concrete adapters (mpv, oto, ...) live inside this
// package and satisfy this interface.
type AudioBackend interface {
	Load(Track) error   // prepare the track; MUST NOT start playback
	Play() error        // start or resume playback of the loaded track
	Pause() error       // suspend playback without unloading
	Stop() error        // halt playback and rewind to the start
	SetVolume(int) error // values outside [0, 100] return ErrVolumeOutOfRange
	Seek(int) error     // jump to position in milliseconds
	State() (playing bool, positionMS int, err error)
	Close() error       // release every resource; MUST be idempotent
}
