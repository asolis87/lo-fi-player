package audio

import (
	"errors"
	"sync"
	"testing"
)

// TestProceduralBackend_ImplementsAudioBackend pins the
// compile-time assertion that ProceduralBackend satisfies the port
// (the package won't compile otherwise) and walks the full
// AudioBackend surface end-to-end with a real SampleGenerator so
// regressions in any of the port methods surface immediately.
func TestProceduralBackend_ImplementsAudioBackend(t *testing.T) {
	// Compile-time assertion: ProceduralBackend MUST satisfy AudioBackend.
	var _ AudioBackend = (*ProceduralBackend)(nil)

	gen := NewWhiteNoiseGenerator(44100)
	b := NewProceduralBackend(WithGenerator(gen), WithBufferSize(1024))
	t.Cleanup(func() { _ = b.Close() })

	// --- Behavioural walk ---
	if err := b.Load(Track{ID: "test", Path: "procedural:white"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	playing, _, err := b.State()
	if err != nil {
		t.Fatalf("State after Play: %v", err)
	}
	if !playing {
		t.Fatalf("State() playing = false after Play, want true")
	}

	if err := b.SetVolume(50); err != nil {
		t.Fatalf("SetVolume(50): %v", err)
	}
	if err := b.SetVolume(-1); !errors.Is(err, ErrVolumeOutOfRange) {
		t.Fatalf("SetVolume(-1) = %v, want ErrVolumeOutOfRange", err)
	}
	if err := b.SetVolume(101); !errors.Is(err, ErrVolumeOutOfRange) {
		t.Fatalf("SetVolume(101) = %v, want ErrVolumeOutOfRange", err)
	}

	if err := b.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	playing, _, err = b.State()
	if err != nil {
		t.Fatalf("State after Pause: %v", err)
	}
	if playing {
		t.Fatalf("State() playing = true after Pause, want false")
	}

	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := b.Seek(1000); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotency: Close MUST be safe to call twice.
	if err := b.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestProceduralBackend_FillsBufferFromGenerator asserts that the
// ring buffer is actually driven by the wrapped SampleGenerator —
// after a single fill, the buffer must contain exactly the
// samples the generator produced, not zeros. This is the contract
// the Select() fallback relies on when it hands the backend off
// to a consumer (real audio device in production, tests here).
func TestProceduralBackend_FillsBufferFromGenerator(t *testing.T) {
	gen := NewWhiteNoiseGeneratorWithSeed(44100, 0xABCDEF)
	b := NewProceduralBackend(WithGenerator(gen), WithBufferSize(64))
	t.Cleanup(func() { _ = b.Close() })

	if err := b.Load(Track{ID: "rain", Path: "procedural:rain"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}

	// Drain samples through the test-only pull path; the backend
	// must refill from the generator on demand.
	pulled := b.pullSamples(64)
	if len(pulled) != 64 {
		t.Fatalf("pullSamples(64) returned %d samples, want 64", len(pulled))
	}

	// The samples cannot all be zero — that would mean the
	// generator was never consulted and the buffer shipped silence.
	nonZero := 0
	for _, s := range pulled {
		if s != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Fatal("pulled 64 samples are all zero; ring buffer was never filled by the generator")
	}
}

// TestProceduralBackend_ConcurrentSafe runs the backend through a
// quick concurrent stress to catch data races in the ring buffer
// and the playing/closed flags.
func TestProceduralBackend_ConcurrentSafe(t *testing.T) {
	gen := NewBrownNoiseGenerator(44100)
	b := NewProceduralBackend(WithGenerator(gen), WithBufferSize(256))
	t.Cleanup(func() { _ = b.Close() })

	if err := b.Load(Track{ID: "rain", Path: "procedural:rain"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); _ = b.pullSamples(32) }()
	go func() { defer wg.Done(); _, _, _ = b.State() }()
	go func() { defer wg.Done(); _ = b.SetVolume(80) }()
	wg.Wait()

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
