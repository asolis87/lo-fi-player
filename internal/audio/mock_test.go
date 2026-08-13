package audio

import (
	"testing"
	"time"
)

// TestMockFIFOAndEnd exercises the PR-1A MockBackend EventSource:
// a burst of six events arrives in FIFO order without loss, and
// Close closes the stream so reads after Close return the zero
// value immediately (channel closed). Emit* after Close is a no-op.
//
// This is a test-double assertion. The mpv adapter's lossless FIFO
// for arbitrary end-file bursts is part of PR-1B and is NOT
// asserted here.
func TestMockFIFOAndEnd(t *testing.T) {
	m := NewMockBackend()
	const burst = 6
	for i := 0; i < burst; i++ {
		// Alternate EventEnd and EventError to verify FIFO over
		// mixed types, not just repeated EventEnd.
		if i%2 == 0 {
			m.EmitEvent(Event{Type: EventEnd, Message: "eof"})
		} else {
			m.EmitEvent(Event{Type: EventError, Message: "io"})
		}
	}
	want := []EventType{EventEnd, EventError, EventEnd, EventError, EventEnd, EventError}
	for i, w := range want {
		ev := mustEvent(t, m, time.Second)
		if ev.Type != w {
			t.Fatalf("event %d type = %v, want %v", i, ev.Type, w)
		}
	}

	// Close closes the stream: subsequent reads return the zero
	// value immediately.
	if err := m.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	select {
	case ev, ok := <-m.Events():
		if ok {
			t.Fatalf("post-Close event = %+v, want zero value", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("post-Close read blocked: channel was not closed")
	}

	// EmitEnd after Close is a no-op.
	m.EmitEnd()
	select {
	case ev, ok := <-m.Events():
		if ok {
			t.Fatalf("post-Close EmitEnd delivered %+v", ev)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("post-Close EmitEnd kept channel open")
	}
}
