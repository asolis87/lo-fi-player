// Package audio — device sink for the procedural backend. The
// default implementation drives github.com/ebitengine/oto/v3; a
// test fake records every Write so the pump can be asserted
// without a sound card. All concrete Device implementations
// live in this file so the layering rule (only this package may
// import an audio adapter) is mechanically enforced.
package audio

import (
	"fmt"
	"io"
	"sync"

	"github.com/ebitengine/oto/v3"
)

// Device is the audio output sink behind the procedural backend.
type Device interface {
	Write(samples []int16) error
	Close() error
}

// WithDevice injects a Device. Passing nil falls back to the
// lazy default factory on Play().
func WithDevice(d Device) ProceduralOption {
	return func(b *ProceduralBackend) { b.device = d }
}

// pumpChunkSize is the samples-per-chunk budget the pump
// goroutine pulls and forwards. ~46 ms at 44.1 kHz.
const pumpChunkSize = 2048

// otoDevice wraps github.com/ebitengine/oto/v3. The Player reads
// from an internal io.Reader (pumpReader); Write marshals int16
// samples into little-endian bytes and feeds them into the
// reader's buffer.
type otoDevice struct {
	reader *pumpReader
	player *oto.Player

	mu     sync.Mutex
	closed bool
}

// pumpReader is the io.Reader oto's Player pulls from. The pump
// goroutine pushes chunks via push(); the audio thread calls
// Read, which blocks on a sync.Cond until a chunk is available.
type pumpReader struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newPumpReader() *pumpReader {
	r := &pumpReader{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *pumpReader) push(chunk []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.buf = append(r.buf, chunk...)
	r.cond.Signal()
}

func (r *pumpReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.buf) == 0 && !r.closed {
		r.cond.Wait()
	}
	if r.closed && len(r.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *pumpReader) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.buf = nil
	r.cond.Broadcast()
}

func newOtoDevice(sampleRate int) (*otoDevice, error) {
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	op := &oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
	}
	ctx, ready, err := oto.NewContext(op)
	if err != nil {
		return nil, fmt.Errorf("audio: oto new context: %w", err)
	}
	<-ready
	if cerr := ctx.Err(); cerr != nil {
		return nil, fmt.Errorf("audio: oto context error: %w", cerr)
	}
	r := newPumpReader()
	p := ctx.NewPlayer(r)
	p.Play()
	return &otoDevice{reader: r, player: p}, nil
}

func (d *otoDevice) Write(samples []int16) error {
	if len(samples) == 0 {
		return nil
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrBackendUnavailable
	}
	d.mu.Unlock()
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		buf[i*2] = byte(s)
		buf[i*2+1] = byte(s >> 8)
	}
	d.reader.push(buf)
	return nil
}

// Close shuts the player and the underlying reader. Idempotent.
// oto.Player.Close is a no-op since v3.4 but we still call it
// for older releases where it actually released resources.
func (d *otoDevice) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	d.mu.Unlock()
	d.reader.close()
	return d.player.Close()
}

// noopDevice discards every Write. Used when no audio subsystem
// is available and no explicit Device was injected — the pump
// still runs so the backend's lifecycle methods keep working.
type noopDevice struct{}

func (noopDevice) Write([]int16) error { return nil }
func (noopDevice) Close() error        { return nil }

var deviceFactoryOverride Device

// defaultDeviceFactory constructs the device the procedural
// backend uses when WithDevice was not supplied. Tries to open
// a real oto audio context; falls back to a no-op device so
// the backend's lifecycle methods keep working when no audio
// subsystem is available.
func defaultDeviceFactory(sampleRate int) Device {
	if deviceFactoryOverride != nil {
		return deviceFactoryOverride
	}
	d, err := newOtoDevice(sampleRate)
	if err != nil {
		return noopDevice{}
	}
	return d
}
