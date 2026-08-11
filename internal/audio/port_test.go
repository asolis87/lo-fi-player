package audio

import "testing"

// Compile-time assertion: MockBackend MUST satisfy the AudioBackend
// port.
var _ AudioBackend = (*MockBackend)(nil)

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
