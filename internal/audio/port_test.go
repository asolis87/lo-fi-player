package audio

import (
	"testing"
	"time"
)

// Compile-time assertion: MockBackend MUST satisfy the AudioBackend
// port.
var _ AudioBackend = (*MockBackend)(nil)

// TestPortContract: Track.Generation, EventSource contract, and
// S-EVT-3-drain FIFO behaviour. mpv's generation correlation on
// Event is part of PR-1B and is NOT covered here.
func TestPortContract(t *testing.T) {
	t.Run("Track/Generation", func(t *testing.T) {
		tr := Track{ID: "x", Path: "/tmp/x.mp3", Generation: 42}
		if tr.Generation != 42 {
			t.Fatalf("Track.Generation = %d, want 42", tr.Generation)
		}
	})

	t.Run("EventSource/Backends", func(t *testing.T) {
		// Every backend MUST satisfy EventSource; the TUI only
		// consumes events from types that pass the assertion.
		var _ EventSource = (*MockBackend)(nil)
		var _ EventSource = (*ProceduralBackend)(nil)
		var _ EventSource = (*MpvBackend)(nil)
	})

	t.Run("S-EVT-3-drain", func(t *testing.T) {
		// Three back-to-back events surface in FIFO order; the
		// channel is closed when the backend shuts down. Generation
		// correlation is not asserted here — that contract lives in
		// the mpv adapter and lands in PR-1B.
		m := NewMockBackend()
		defer func() { _ = m.Close() }()
		for i := 0; i < 3; i++ {
			m.EmitEnd()
		}
		for i := 0; i < 3; i++ {
			ev := mustEvent(t, m, time.Second)
			if ev.Type != EventEnd {
				t.Fatalf("event %d type = %v, want %v", i, ev.Type, EventEnd)
			}
		}
	})
}

func mustEvent(t *testing.T, src EventSource, d time.Duration) Event {
	t.Helper()
	select {
	case ev, ok := <-src.Events():
		if !ok {
			t.Fatalf("event channel closed before receiving expected event")
		}
		return ev
	case <-time.After(d):
		t.Fatalf("timed out after %v waiting for event", d)
	}
	return Event{}
}

func TestMockBackend_RecordsCalls(t *testing.T) {
	m := NewMockBackend()

	if err := m.Load(Track{ID: "track-1", Path: "/tmp/track-1.mp3"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := m.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := m.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := m.Seek(1500); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	if got := m.Loaded(); len(got) != 1 || got[0].ID != "track-1" {
		t.Fatalf("Loaded() = %+v, want one track with id track-1", got)
	}
	if !m.Played() || !m.Paused() || !m.Stopped() {
		t.Fatalf("call flags = played=%v paused=%v stopped=%v, want all true",
			m.Played(), m.Paused(), m.Stopped())
	}
	if got := m.Seeked(); len(got) != 1 || got[0] != 1500 {
		t.Fatalf("Seeked() = %+v, want one entry of 1500", got)
	}
}

func TestMockBackend_SetVolumeClampsRange(t *testing.T) {
	m := NewMockBackend()

	if err := m.SetVolume(50); err != nil {
		t.Fatalf("SetVolume(50): %v", err)
	}
	if got := m.Volume(); got != 50 {
		t.Fatalf("Volume() = %d, want 50", got)
	}
	if err := m.SetVolume(-1); err == nil {
		t.Fatal("SetVolume(-1) returned nil error, want range error")
	}
	if err := m.SetVolume(101); err == nil {
		t.Fatal("SetVolume(101) returned nil error, want range error")
	}
}

func TestMockBackend_StateReflectsPlayback(t *testing.T) {
	m := NewMockBackend()

	playing, _, err := m.State()
	if err != nil || playing {
		t.Fatalf("idle State() = (%v, _, %v), want (false, _, nil)", playing, err)
	}

	if err := m.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	playing, _, err = m.State()
	if err != nil || !playing {
		t.Fatalf("post-Play State() = (%v, _, %v), want (true, _, nil)", playing, err)
	}
}

func TestMockBackend_CloseIsIdempotent(t *testing.T) {
	m := NewMockBackend()
	if err := m.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if !m.Closed() {
		t.Fatal("Closed() = false after Close")
	}
}
