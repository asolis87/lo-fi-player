package audio

import (
	"testing"
	"time"
)

// TestProcEventSource asserts ProceduralBackend implements
// EventSource and that Close closes the stream when no event was
// ever emitted. Rain is infinite by design so the only observable
// side-effect of Close is the closed channel.
func TestProcEventSource(t *testing.T) {
	b := NewProceduralBackend(WithGenerator(NewRainGenerator(44100)))
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case ev, ok := <-b.Events():
		if ok {
			t.Fatalf("post-Close event = %+v, want zero value", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("post-Close read blocked: channel was not closed")
	}
}
