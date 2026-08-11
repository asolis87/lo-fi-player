package audio

import (
	"context"
	"sync"
	"time"
)

// ProceduralBackend is the AudioBackend adapter that drives a
// SampleGenerator through a fixed-size ring buffer and a pump
// goroutine that drains the buffer at wall-clock rate.
type ProceduralBackend struct {
	gen        SampleGenerator
	sampleRate int
	bufferSize int

	mu      sync.Mutex
	buf     []int16 // ring storage
	head    int     // read cursor
	tail    int     // write cursor
	size    int     // valid sample count
	loaded  bool
	playing bool
	volume  int
	closed  bool
	pos     int // samples drained since last Load/Seek

	device     Device // audio sink; nil → defaultDeviceFactory on Play
	pumpCtx    context.Context
	pumpCancel context.CancelFunc
	pumpDone   chan struct{}
}

// ProceduralOption configures a ProceduralBackend.
type ProceduralOption func(*ProceduralBackend)

// WithGenerator sets the SampleGenerator the backend wraps.
// Required: NewProceduralBackend returns nil if no generator is
// supplied (callers should always pass one explicitly).
func WithGenerator(g SampleGenerator) ProceduralOption {
	return func(b *ProceduralBackend) { b.gen = g }
}

// WithBufferSize overrides the default ring buffer size. Values
// <= 0 fall back to the default. The size is the maximum number
// of samples the buffer can hold; it is fixed for the backend's
// lifetime and never grows.
func WithBufferSize(n int) ProceduralOption {
	return func(b *ProceduralBackend) {
		if n > 0 {
			b.bufferSize = n
		}
	}
}

// WithSampleRate overrides the backend's tracked sample rate.
// Defaults to 44100 when zero or negative.
func WithSampleRate(sr int) ProceduralOption {
	return func(b *ProceduralBackend) {
		if sr > 0 {
			b.sampleRate = sr
		}
	}
}

// defaultProceduralBufferSize is the fixed ring buffer capacity
// used when no explicit size is supplied. ~185 ms at 44.1 kHz —
// small enough to keep latency bounded, large enough that the
// generator does not have to refill on every consumer pull.
const defaultProceduralBufferSize = 8192

// NewProceduralBackend constructs a backend wrapping gen. The
// generator MUST be non-nil; passing nil returns nil. Default
// volume is 100 so the procedural fallback plays audio out of
// the box.
func NewProceduralBackend(opts ...ProceduralOption) *ProceduralBackend {
	b := &ProceduralBackend{
		sampleRate: 44100,
		bufferSize: defaultProceduralBufferSize,
		volume:     100,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.gen == nil {
		return nil
	}
	b.buf = make([]int16, b.bufferSize)
	return b
}

// Generator returns the wrapped SampleGenerator. Used by Select()
// to detect the default generator type and by callers that need
// to reset/replace it.
func (b *ProceduralBackend) Generator() SampleGenerator { return b.gen }

// BufferSize returns the fixed ring buffer capacity.
func (b *ProceduralBackend) BufferSize() int { return b.bufferSize }

// SampleRate returns the backend's sample rate.
func (b *ProceduralBackend) SampleRate() int { return b.sampleRate }

// Volume returns the most recently set volume level after range
// validation. Default is 100 (full scale).
func (b *ProceduralBackend) Volume() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.volume
}

// Load prepares the backend to play the given Track. It resets
// the wrapped generator so playback starts from a deterministic
// state, clears the ring buffer, and marks the backend as loaded
// but not yet playing. For procedural tracks the Track.Path
// (e.g. "procedural:rain") is informational only; the generator
// itself drives the sample stream.
func (b *ProceduralBackend) Load(t Track) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBackendUnavailable
	}
	b.gen.Reset()
	b.head, b.tail, b.size, b.pos = 0, 0, 0, 0
	b.loaded = true
	b.playing = false
	return nil
}

// Play starts or resumes playback. The ring buffer is filled
// eagerly so the pump goroutine never blocks on the first read.
func (b *ProceduralBackend) Play() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrBackendUnavailable
	}
	if !b.loaded {
		b.mu.Unlock()
		return ErrBackendUnavailable
	}
	if b.device == nil {
		b.device = defaultDeviceFactory(b.sampleRate)
	}
	device := b.device
	b.fillLocked(b.bufferSize)
	b.stopPumpLocked()
	pumpCtx, pumpCancel := context.WithCancel(context.Background())
	b.pumpCtx = pumpCtx
	b.pumpCancel = pumpCancel
	b.pumpDone = make(chan struct{})
	b.playing = true
	go b.pump(pumpCtx, b.pumpDone, device)
	b.mu.Unlock()
	return nil
}

// Pause suspends playback. The pump goroutine is signalled to
// stop so the device stops receiving samples.
func (b *ProceduralBackend) Pause() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrBackendUnavailable
	}
	b.playing = false
	b.stopPumpLocked()
	b.mu.Unlock()
	return nil
}

// Stop halts playback and rewinds to the start.
func (b *ProceduralBackend) Stop() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrBackendUnavailable
	}
	b.playing = false
	b.head, b.tail, b.size, b.pos = 0, 0, 0, 0
	b.gen.Reset()
	b.stopPumpLocked()
	b.mu.Unlock()
	return nil
}

// SetVolume sets the playback level. The pump goroutine reads
// b.volume on every chunk so updates take effect next iteration.
func (b *ProceduralBackend) SetVolume(v int) error {
	if v < 0 || v > 100 {
		return ErrVolumeOutOfRange
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBackendUnavailable
	}
	b.volume = v
	return nil
}

// Seek jumps to an absolute position in milliseconds.
func (b *ProceduralBackend) Seek(ms int) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrBackendUnavailable
	}
	if ms < 0 {
		ms = 0
	}
	b.pos = ms * b.sampleRate / 1000
	b.stopPumpLocked()
	b.mu.Unlock()
	return nil
}

// stopPumpLocked cancels the pump goroutine and waits for it to
// exit. Caller MUST hold b.mu.
func (b *ProceduralBackend) stopPumpLocked() {
	pumpCancel := b.pumpCancel
	pumpDone := b.pumpDone
	b.pumpCancel = nil
	b.pumpCtx = nil
	b.pumpDone = nil
	if pumpCancel != nil {
		pumpCancel()
		<-pumpDone
	}
}

// State reports whether the backend is currently playing and the
// position in milliseconds since the last Load or Seek. The
// position is computed from the sample counter, not from the
// ring buffer's read cursor, so it stays meaningful even when
// the consumer is faster or slower than the generator.
func (b *ProceduralBackend) State() (bool, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false, 0, ErrBackendUnavailable
	}
	posMS := b.pos * 1000 / b.sampleRate
	return b.playing, posMS, nil
}

// Close permanently shuts the backend down. Idempotent: a second
// call is a no-op that returns nil so callers can defer it
// without worrying about double-close from cleanup paths.
func (b *ProceduralBackend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.playing = false
	b.head, b.tail, b.size = 0, 0, 0
	b.stopPumpLocked()
	device := b.device
	b.device = nil
	b.mu.Unlock()
	if device != nil {
		_ = device.Close()
	}
	return nil
}

// pullSamples drains up to n samples from the ring buffer,
// refilling from the generator as needed. It is the test-side
// surface that stands in for a real audio device's pull
// callback; it is not part of the AudioBackend port because
// production audio output is owned by the device driver.
func (b *ProceduralBackend) pullSamples(n int) []int16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pullSamplesLocked(n)
}

// pullSamplesLocked assumes b.mu is held.
func (b *ProceduralBackend) pullSamplesLocked(n int) []int16 {
	if b.closed || n <= 0 {
		return nil
	}
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		if b.size == 0 {
			b.fillLocked(b.bufferSize)
			if b.size == 0 {
				out[i] = 0
				continue
			}
		}
		out[i] = b.buf[b.head]
		b.head = (b.head + 1) % len(b.buf)
		b.size--
		b.pos++
	}
	return out
}

// pump drives the device. Pulls chunks from the ring buffer,
// applies the current volume, forwards to the device, and paces
// itself to wall-clock via a timer-aware select against the
// cancel context.
func (b *ProceduralBackend) pump(ctx context.Context, done chan struct{}, device Device) {
	defer close(done)
	chunkDur := time.Duration(pumpChunkSize) * time.Second / time.Duration(b.sampleRate)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		b.mu.Lock()
		vol := b.volume
		samples := b.pullSamplesLocked(pumpChunkSize)
		b.mu.Unlock()
		if len(samples) > 0 {
			_ = device.Write(applyVolume(samples, vol))
		}
		t := time.NewTimer(chunkDur)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

func applyVolume(buf []int16, vol int) []int16 {
	if vol >= 100 {
		return buf
	}
	out := make([]int16, len(buf))
	gain := float64(vol) / 100.0
	for i, s := range buf {
		v := float64(s) * gain
		if v >= float64(maxInt16) {
			out[i] = maxInt16
		} else if v <= float64(minInt16) {
			out[i] = minInt16
		} else {
			out[i] = int16(v)
		}
	}
	return out
}

const (
	maxInt16 = int16(^uint16(0) >> 1)
	minInt16 = -maxInt16 - 1
)

// fillLocked draws count samples from the generator and writes
// them into the ring buffer, wrapping around as needed. Caller
// MUST hold b.mu.
func (b *ProceduralBackend) fillLocked(count int) {
	if b.gen == nil {
		return
	}
	samples := b.gen.Next(count)
	for _, s := range samples {
		b.buf[b.tail] = s
		b.tail = (b.tail + 1) % len(b.buf)
		if b.size == len(b.buf) {
			// Overwrite oldest sample.
			b.head = (b.head + 1) % len(b.buf)
		} else {
			b.size++
		}
	}
}
