package audio

import "sync"

// MockBackend is a deterministic, goroutine-safe test double for the
// AudioBackend port. It is exported so other packages can reuse it
// in tests; real adapters (mpv, oto) land in later PRs.
type MockBackend struct {
	mu      sync.Mutex
	loaded  []Track
	played  bool
	paused  bool
	stopped bool
	seeked  []int
	volume  int
	closed  int
	events  chan Event
}

// mockEventBufferSize bounds the MockBackend's event channel; the
// PR-1A burst contract (>4 events without loss) drives the size.
const mockEventBufferSize = 64

// NewMockBackend returns a MockBackend with default volume 0, an
// empty call log, and a buffered event channel.
func NewMockBackend() *MockBackend {
	return &MockBackend{events: make(chan Event, mockEventBufferSize)}
}

// Events returns the read-only event stream. Closed by Close.
func (m *MockBackend) Events() <-chan Event { return m.events }

// EmitEvent pushes ev onto the FIFO stream. No-op when closed.
// Generation correlation is not the test double's job — mpv owns
// that contract (PR-1B).
func (m *MockBackend) EmitEvent(ev Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed > 0 {
		return
	}
	m.events <- ev
}

// EmitEnd is a convenience wrapper for EventEnd events.
func (m *MockBackend) EmitEnd() {
	m.EmitEvent(Event{Type: EventEnd})
}

func (m *MockBackend) Load(t Track) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loaded = append(m.loaded, t)
	return nil
}

func (m *MockBackend) Play() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.played, m.paused, m.stopped = true, false, false
	return nil
}

func (m *MockBackend) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused = true
	return nil
}

func (m *MockBackend) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	return nil
}

func (m *MockBackend) SetVolume(v int) error {
	if v < 0 || v > 100 {
		return ErrVolumeOutOfRange
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.volume = v
	return nil
}

func (m *MockBackend) Seek(ms int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seeked = append(m.seeked, ms)
	return nil
}

func (m *MockBackend) State() (bool, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.played && !m.paused && !m.stopped, 0, nil
}

// Close releases every resource and closes the event stream once.
// Idempotent: subsequent calls return nil and do not double-close
// the events channel.
func (m *MockBackend) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed++
	if m.closed == 1 {
		close(m.events)
	}
	return nil
}

// Loaded returns the tracks passed to Load, in invocation order.
func (m *MockBackend) Loaded() []Track {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Track, len(m.loaded))
	copy(out, m.loaded)
	return out
}

// Played reports whether Play has been called since construction.
func (m *MockBackend) Played() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.played
}

// Paused reports whether Pause has been called since construction.
func (m *MockBackend) Paused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused
}

// Stopped reports whether Stop has been called since construction.
func (m *MockBackend) Stopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// Seeked returns the positions Seek has been asked to jump to.
func (m *MockBackend) Seeked() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]int, len(m.seeked))
	copy(out, m.seeked)
	return out
}

// Volume returns the most recent value passed to SetVolume after
// range validation.
func (m *MockBackend) Volume() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.volume
}

// Closed reports whether Close has been called at least once.
func (m *MockBackend) Closed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed > 0
}
