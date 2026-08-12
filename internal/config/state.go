// Package config — capa de dominio del estado de reproduccion v2.
// Dormant en PR-A2: sin consumidores en cmd/lofi o internal/tui.
// Contrato: volumen crudo preservado + historial MRU con cap 25
// + persistencia atomica via seam renameFile compartido con Save
// (PERSIST-1 + PERSIST-2).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"
)

// DefaultVolume es el volumen uniforme inicial (VOL-1). El raw de un Config fresco (Default().Volume) es 50.
const DefaultVolume = 50

// ErrInvalidVolume se devuelve cuando SetVolume recibe un valor
// fuera de [0, 100]; el volumen crudo queda intacto.
// 0 (mute) es valido y no produce error.
var ErrInvalidVolume = errors.New("config: volume out of range")

// PlaybackState es el estado vivo de volumen + historial MRU;
// seguro para uso concurrente. El raw persistido nunca se
// reescribe desde aqui: EffectiveVolume es una vista, no una
// correccion (VOL-2).
type PlaybackState struct {
	mu  sync.Mutex
	raw Config
}

// NewPlaybackState construye un PlaybackState a partir de un Config cargado del disco; volumen e historial se preservan tal cual.
func NewPlaybackState(raw Config) *PlaybackState {
	return &PlaybackState{raw: raw}
}

// EffectiveVolume devuelve el volumen crudo si esta dentro del
// rango estricto [0, 100]; en caso contrario devuelve 50 sin
// reescribir el raw persistido (VOL-1 + VOL-2).
func (s *PlaybackState) EffectiveVolume() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.raw.Volume
	if v < 0 || v > 100 {
		return DefaultVolume
	}
	return v
}

// History devuelve una copia defensiva del historial MRU
// (newest-first, cap 25); el caller puede mutar el slice sin
// afectar el estado interno (HIST-2).
func (s *PlaybackState) History() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.raw.History == nil {
		return []string{}
	}
	out := make([]string, len(s.raw.History))
	copy(out, s.raw.History)
	return out
}

// SetVolume actualiza el volumen crudo en memoria. No persiste;
// el caller debe invocar Persist() para durabilidad (PERSIST-1).
// Rechaza valores fuera de [0, 100] con ErrInvalidVolume. 0 es
// valido (mute).
func (s *PlaybackState) SetVolume(v int) error {
	if v < 0 || v > 100 {
		return ErrInvalidVolume
	}
	s.mu.Lock()
	s.raw.Volume = v
	s.mu.Unlock()
	return nil
}

// RecordPlayed aplica MRU-front con dedupe por primera ocurrencia
// y cap a 25 (HIST-1 + HIST-2). Replay del frente es no-op.
// Persist() es responsabilidad del caller para durabilidad.
func (s *PlaybackState) RecordPlayed(id string) error {
	if id == "" {
		return fmt.Errorf("config: RecordPlayed with empty id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.raw.History == nil {
		s.raw.History = []string{}
	}
	if len(s.raw.History) > 0 && s.raw.History[0] == id {
		return nil
	}
	filtered := make([]string, 0, len(s.raw.History))
	for _, existing := range s.raw.History {
		if existing == id {
			continue
		}
		filtered = append(filtered, existing)
	}
	updated := make([]string, 0, len(filtered)+1)
	updated = append(updated, id)
	updated = append(updated, filtered...)
	if len(updated) > historyCap {
		updated = updated[:historyCap]
	}
	s.raw.History = updated
	return nil
}

// Persist serializa el estado vivo al esquema v2 en disco via
// staging + fsync + renameFile; last_queue nunca se emite
// (SCHEMA-1). Stub minimo en PR-A2: PR-B (B5) lo invoca cuando
// aplique.
func (s *PlaybackState) Persist() error {
	s.mu.Lock()
	history := s.raw.History
	if history == nil {
		history = []string{}
	}
	volume := s.raw.Volume
	s.mu.Unlock()
	var buf bytes.Buffer
	if err := writeV2(&buf, history, volume); err != nil {
		return err
	}
	return persistBuffer(&buf)
}

// persistBuffer escribe el buffer v2 ya serializado en Path() de
// forma atomica via staging + fsync + renameFile (compartido con
// Save) para que la falla inyectada aplique a todos los caminos
// de escritura atomica del paquete.
func persistBuffer(buf *bytes.Buffer) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", dir, err)
	}
	p, err := Path()
	if err != nil {
		return err
	}
	stage, err := os.CreateTemp(dir, tempPrefix)
	if err != nil {
		return fmt.Errorf("config: create staging file: %w", err)
	}
	stagePath := stage.Name()
	var renamed bool
	defer func() {
		if !renamed {
			os.Remove(stagePath)
		}
	}()
	if err := os.Chmod(stagePath, filePerm); err != nil {
		return err
	}
	if _, err := stage.Write(buf.Bytes()); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := renameFile(stagePath, p); err != nil {
		return err
	}
	renamed = true
	return nil
}
