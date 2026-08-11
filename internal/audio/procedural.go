package audio

import (
	"math"
	"math/rand"
)

// SampleGenerator produces N mono PCM 16-bit samples on demand.
// Implementations MUST be deterministic for the same construction
// parameters (seed + sampleRate) so callers can rely on golden
// output for tests and reproducible playback. Reset() returns the
// generator to the state it had immediately after construction;
// calling Next(N) after Reset() MUST yield the same bytes as the
// first Next(N) call did.
type SampleGenerator interface {
	// Next returns N samples at the generator's configured sample
	// rate. The returned slice is freshly allocated and owned by
	// the caller; implementations may reuse an internal buffer
	// across calls but MUST NOT return a slice that aliases
	// caller-held memory.
	Next(n int) []int16
	// Reset restores the generator to its initial state. After
	// Reset, Next must produce the same sequence as on a freshly
	// constructed generator with the same parameters.
	Reset()
}

// --- WhiteNoiseGenerator ---

// WhiteNoiseGenerator emits uniformly distributed pseudo-random
// samples in the int16 range. Each sample is independent, so the
// output is spectrally flat and statistically bounded by construction.
type WhiteNoiseGenerator struct {
	rng        *rand.Rand
	seed       int64
	sampleRate int
}

// NewWhiteNoiseGenerator constructs a WhiteNoiseGenerator with a
// fixed default seed so callers get deterministic output without
// having to manage seed lifecycles. Override the seed with
// NewWhiteNoiseGeneratorWithSeed when tests need to assert exact
// byte sequences.
func NewWhiteNoiseGenerator(sampleRate int) *WhiteNoiseGenerator {
	return NewWhiteNoiseGeneratorWithSeed(sampleRate, defaultSampleSeed)
}

// NewWhiteNoiseGeneratorWithSeed is the seed-aware constructor used
// by tests; production callers should prefer NewWhiteNoiseGenerator.
func NewWhiteNoiseGeneratorWithSeed(sampleRate int, seed int64) *WhiteNoiseGenerator {
	return &WhiteNoiseGenerator{
		rng:        rand.New(rand.NewSource(seed)),
		seed:       seed,
		sampleRate: sampleRate,
	}
}

func (g *WhiteNoiseGenerator) Reset() {
	g.rng = rand.New(rand.NewSource(g.seed))
}

func (g *WhiteNoiseGenerator) Next(n int) []int16 {
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		// Uniform on [-1, 1); rng.Float64 is in [0, 1).
		v := g.rng.Float64()*2 - 1
		out[i] = clampInt16(v * float64(math.MaxInt16))
	}
	return out
}

// --- BrownNoiseGenerator ---

// BrownNoiseGenerator is a leaky integrator driven by white noise:
// y[n] = leak*y[n-1] + gain*w[n], where w[n] is uniform white
// noise. The leak keeps the random walk from drifting without
// bound; with the constants below the steady-state peak stays
// comfortably inside int16 range for any plausible sample window.
type BrownNoiseGenerator struct {
	rng        *rand.Rand
	seed       int64
	sampleRate int

	y          float64
	leak       float64
	gain       float64
}

// NewBrownNoiseGenerator constructs a BrownNoiseGenerator with a
// fixed default seed. See NewWhiteNoiseGenerator for the rationale.
func NewBrownNoiseGenerator(sampleRate int) *BrownNoiseGenerator {
	return NewBrownNoiseGeneratorWithSeed(sampleRate, defaultSampleSeed)
}

// NewBrownNoiseGeneratorWithSeed is the seed-aware constructor used
// by tests.
func NewBrownNoiseGeneratorWithSeed(sampleRate int, seed int64) *BrownNoiseGenerator {
	return &BrownNoiseGenerator{
		rng:        rand.New(rand.NewSource(seed)),
		seed:       seed,
		sampleRate: sampleRate,
		leak:       0.997,
		gain:       0.05,
	}
}

func (g *BrownNoiseGenerator) Reset() {
	g.rng = rand.New(rand.NewSource(g.seed))
	g.y = 0
}

func (g *BrownNoiseGenerator) Next(n int) []int16 {
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		w := g.rng.Float64()*2 - 1
		g.y = g.y*g.leak + w*g.gain
		// 0.5 headroom: peak excursions stay inside the int16
		// envelope even for long draws (steady-state RMS ≈ 0.37).
		out[i] = clampInt16(g.y * float64(math.MaxInt16) * 0.5)
	}
	return out
}

// --- RainGenerator ---

// RainGenerator imitates rainfall by layering Voss-McCartney pink
// noise (a constant low rumble) with sparse, exponentially-decaying
// "drop" impulses. The output is bounded by construction because
// the pink-noise rows are summed into a bounded buffer and each
// drop envelope decays to zero within milliseconds.
type RainGenerator struct {
	rng        *rand.Rand
	seed       int64
	sampleRate int

	// Voss-McCartney pink-noise state.
	pinkRows [7]float64
	pinkSum  float64
	pinkIdx  uint64

	// Active drop envelope state. dropAmp is the current envelope
	// value; when it falls below dropAmpFloor the drop is
	// considered inaudible and the next trigger is eligible.
	dropAmp      float64
	dropDecay    float64 // per-sample multiplier, 0 < decay < 1
	dropAmpFloor float64
}

// NewRainGenerator constructs a RainGenerator with a fixed default
// seed.
func NewRainGenerator(sampleRate int) *RainGenerator {
	return NewRainGeneratorWithSeed(sampleRate, defaultSampleSeed)
}

// NewRainGeneratorWithSeed is the seed-aware constructor used by
// tests. The seed controls both the pink-noise white source and
// the drop trigger / amplitude distribution, so equal seeds
// produce byte-identical output.
func NewRainGeneratorWithSeed(sampleRate int, seed int64) *RainGenerator {
	g := &RainGenerator{
		seed:         seed,
		sampleRate:   sampleRate,
		dropDecay:    math.Exp(-1.0 / (0.0008 * float64(sampleRate))), // 0.8 ms tau
		dropAmpFloor: 1.0 / 32767.0,
	}
	g.Reset()
	return g
}

func (g *RainGenerator) Reset() {
	g.rng = rand.New(rand.NewSource(g.seed))
	for i := range g.pinkRows {
		g.pinkRows[i] = 0
	}
	g.pinkSum = 0
	g.pinkIdx = 0
	g.dropAmp = 0
}

func (g *RainGenerator) Next(n int) []int16 {
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		// 1) Pink-noise update (Voss-McCartney with 7 rows).
		// Each row updates whenever its bit flips in pinkIdx.
		g.pinkIdx++
		for r := 0; r < len(g.pinkRows); r++ {
			if g.pinkIdx&(1<<r) == 0 {
				continue
			}
			w := g.rng.Float64()*2 - 1
			g.pinkSum -= g.pinkRows[r]
			g.pinkRows[r] = w
			g.pinkSum += w
		}
		pink := g.pinkSum / float64(len(g.pinkRows))

		// 2) Trigger a new drop with low probability. ~22 drops/s
		// at 44.1 kHz keeps the rain texture dense without
		// saturating the mix.
		if g.dropAmp <= g.dropAmpFloor && g.rng.Float64() < rainDropProbability {
			g.dropAmp = rainDropMinAmp + g.rng.Float64()*(rainDropMaxAmp-rainDropMinAmp)
		}

		// 3) Mix pink noise + active drop envelope; decays to zero.
		mix := pink*rainPinkGain + g.dropAmp
		g.dropAmp *= g.dropDecay
		out[i] = clampInt16(mix * float64(math.MaxInt16))
	}
	return out
}

// --- internal constants ---

// defaultSampleSeed is the deterministic seed used by the no-seed
// constructors. Picked once and frozen so identical constructions
// produce identical bytes across processes.
const defaultSampleSeed int64 = 0xC0FFEE_BEEF

// rainDropProbability is the per-sample chance of triggering a new
// drop once the previous one has decayed below the audible floor.
const rainDropProbability = 0.0005

// rainDropMaxAmp / rainDropMinAmp bound the random drop amplitude.
const (
	rainDropMaxAmp = 0.85
	rainDropMinAmp = 0.10
)

// rainPinkGain scales the pink-noise component below the drop
// envelope so the rain texture sits underneath the percussive drops.
const rainPinkGain = 0.45

// clampInt16 saturates a float64 to the int16 envelope. All public
// output paths in this file pass through this helper so the bounded
// contracts on the white/brown/rain generators hold even when
// internal state drifts near the edges of the float range.
func clampInt16(v float64) int16 {
	if v >= float64(math.MaxInt16) {
		return math.MaxInt16
	}
	if v <= float64(math.MinInt16) {
		return math.MinInt16
	}
	return int16(v)
}
