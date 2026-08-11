package audio

import (
	"sync"
)

// ProceduralBackend is the AudioBackend adapter that drives a
// SampleGenerator through a fixed-size ring buffer. It is the
// "ambient noise" fallback used when mpv is unavailable; the
// generator is fully deterministic so the buffer can be replayed
// identically from any process given the same seed and Track id.
//
// The ring buffer holds exactly bufferSize int16 samples. A
// consumer (real audio device in production, tests here via the
// pullSamples hook) drains samples and the backend refills from
// the wrapped generator on demand. Close releases every resource
// and is safe to call repeatedly.
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
// generator MUST be non-nil; passing nil returns nil so callers
// fail loud at construction time instead of panicking at play.
func NewProceduralBackend(opts ...ProceduralOption) *ProceduralBackend {
	b := &ProceduralBackend{
		sampleRate: 44100,
		bufferSize: defaultProceduralBufferSize,
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
// validation. 0 means muted; the default before any SetVolume is 0.
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
// eagerly so a consumer never blocks on the first read; the
// generator continues to refill on demand.
func (b *ProceduralBackend) Play() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBackendUnavailable
	}
	if !b.loaded {
		return ErrBackendUnavailable
	}
	b.fillLocked(b.bufferSize)
	b.playing = true
	return nil
}

// Pause suspends playback without unloading the generator or
// clearing the buffer. State() will report playing=false until
// Play() is called again.
func (b *ProceduralBackend) Pause() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBackendUnavailable
	}
	b.playing = false
	return nil
}

// Stop halts playback and rewinds to the start. The ring buffer
// is cleared; the next Play() will refill from the generator's
// beginning.
func (b *ProceduralBackend) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBackendUnavailable
	}
	b.playing = false
	b.head, b.tail, b.size, b.pos = 0, 0, 0, 0
	b.gen.Reset()
	return nil
}

// SetVolume sets the playback level. Values outside [0, 100] are
// rejected with ErrVolumeOutOfRange so callers can match on
// errors.Is and mirror MpvBackend's contract.
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

// Seek jumps to an absolute position in milliseconds. The
// internal sample counter is reset to the equivalent sample
// index; the ring buffer itself is not modified, so the next
// consumer pull will continue from the new logical position.
func (b *ProceduralBackend) Seek(ms int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBackendUnavailable
	}
	if ms < 0 {
		ms = 0
	}
	b.pos = ms * b.sampleRate / 1000
	return nil
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

// Close permanently shuts the backend down. Subsequent calls
// return ErrBackendUnavailable. Close is idempotent: a second
// call is a no-op that returns nil so callers can defer it
// without worrying about double-close from cleanup paths.
func (b *ProceduralBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	b.playing = false
	b.head, b.tail, b.size = 0, 0, 0
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
	if b.closed || n <= 0 {
		return nil
	}
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		if b.size == 0 {
			b.fillLocked(b.bufferSize)
			if b.size == 0 {
				// Generator exhausted (should not happen for
				// our infinite-sample procedural sources, but
				// defend anyway): emit silence.
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
