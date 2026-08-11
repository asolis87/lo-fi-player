package audio

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeDevice is the test-only Device the procedural backend
// drives in unit tests. It records every Write so the pump's
// behaviour can be asserted without touching real audio hardware.
type fakeDevice struct {
	mu       sync.Mutex
	written  []int16
	closeCnt int
}

func newFakeDevice() *fakeDevice { return &fakeDevice{} }

func (f *fakeDevice) Write(samples []int16) error { f.mu.Lock(); defer f.mu.Unlock(); f.written = append(f.written, samples...); return nil }
func (f *fakeDevice) Close() error                { f.mu.Lock(); defer f.mu.Unlock(); f.closeCnt++; return nil }
func (f *fakeDevice) sampleCount() int           { f.mu.Lock(); defer f.mu.Unlock(); return len(f.written) }
func (f *fakeDevice) writeCount() int            { f.mu.Lock(); defer f.mu.Unlock(); return len(f.written) }
func (f *fakeDevice) closed() bool               { f.mu.Lock(); defer f.mu.Unlock(); return f.closeCnt > 0 }
func (f *fakeDevice) samples() []int16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int16, len(f.written))
	copy(out, f.written)
	return out
}

// TestProceduralBackend_ImplementsAudioBackend walks the full
// AudioBackend surface with a real SampleGenerator. A noopDevice
// is injected so the pump goroutine does not touch real audio
// hardware on a CI runner.
func TestProceduralBackend_ImplementsAudioBackend(t *testing.T) {
	var _ AudioBackend = (*ProceduralBackend)(nil)

	gen := NewWhiteNoiseGenerator(44100)
	b := NewProceduralBackend(WithGenerator(gen), WithBufferSize(1024), WithDevice(noopDevice{}))
	t.Cleanup(func() { _ = b.Close() })

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
	if err := b.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestProceduralBackend_FillsBufferFromGenerator asserts that the
// ring buffer is driven by the wrapped SampleGenerator — the pump
// goroutine forwards samples to the injected fake device and the
// device MUST receive non-zero bytes.
func TestProceduralBackend_FillsBufferFromGenerator(t *testing.T) {
	gen := NewWhiteNoiseGeneratorWithSeed(44100, 0xABCDEF)
	dev := newFakeDevice()
	b := NewProceduralBackend(WithGenerator(gen), WithBufferSize(64), WithDevice(dev))
	t.Cleanup(func() { _ = b.Close() })

	if err := b.Load(Track{ID: "rain", Path: "procedural:rain"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for dev.sampleCount() < 64 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := dev.sampleCount(); got < 64 {
		t.Fatalf("device received %d samples, want >= 64 (pump did not deliver)", got)
	}

	nonZero := 0
	for _, s := range dev.samples() {
		if s != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Fatal("device received all-zero samples; ring buffer was never filled by the generator")
	}
}

// TestProceduralBackend_ConcurrentSafe runs the backend through a
// quick concurrent stress to catch data races in the ring buffer
// and the playing/closed flags. The pump goroutine participates
// in the race; a noopDevice keeps the writes off real audio.
func TestProceduralBackend_ConcurrentSafe(t *testing.T) {
	gen := NewBrownNoiseGenerator(44100)
	b := NewProceduralBackend(WithGenerator(gen), WithBufferSize(256), WithDevice(noopDevice{}))
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

// TestProceduralBackend_PumpsSamplesToDevice is the canonical
// proof that the pump goroutine actually forwards samples to the
// configured Device. Asserts the device received bytes, that at
// least one sample is non-zero, and that volume is applied on
// the way out. This is the test that would have caught the
// PR-4 rescind bug — the previous backend pulled samples only
// through the test-only pullSamples hook, so production was
// silent regardless of what the device wanted.
func TestProceduralBackend_PumpsSamplesToDevice(t *testing.T) {
	dev := newFakeDevice()
	b := NewProceduralBackend(WithGenerator(NewRainGenerator(44100)), WithSampleRate(44100), WithDevice(dev))
	t.Cleanup(func() { _ = b.Close() })

	if err := b.Load(Track{ID: "procedural:rain", Path: "procedural:rain"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.SetVolume(50); err != nil {
		t.Fatalf("SetVolume(50): %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for dev.sampleCount() < pumpChunkSize && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := dev.sampleCount(); got < pumpChunkSize {
		t.Fatalf("device received %d samples, want >= %d (pump did not deliver)", got, pumpChunkSize)
	}

	// Volume = 50% must scale the peak to at most ~half of the
	// int16 envelope.
	peak := int16(0)
	for _, s := range dev.samples() {
		if s > peak {
			peak = s
		} else if -s > peak {
			peak = -s
		}
	}
	halfMax := int16(0x3FFF)
	if peak > halfMax {
		t.Fatalf("peak sample %d exceeds 50%% envelope (max %d); volume was not applied", peak, halfMax)
	}
	if peak == 0 {
		t.Fatalf("peak sample is zero; pump forwarded silence")
	}

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !dev.closed() {
		t.Fatal("device was not closed after backend Close")
	}
}
