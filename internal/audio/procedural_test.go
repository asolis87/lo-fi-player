package audio

import (
	"math"
	"reflect"
	"testing"
)

// --- WhiteNoiseGenerator ---

func TestWhiteNoiseGenerator_BoundedOutput(t *testing.T) {
	g := NewWhiteNoiseGenerator(44100)
	const N = 8192
	out := g.Next(N)
	if len(out) != N {
		t.Fatalf("Next(%d) returned %d samples, want %d", N, len(out), N)
	}
	for i, s := range out {
		if s < math.MinInt16 || s > math.MaxInt16 {
			t.Fatalf("sample[%d] = %d, out of int16 range [%d,%d]",
				i, s, math.MinInt16, math.MaxInt16)
		}
	}
	// Statistical sanity: white noise over 8192 samples must have
	// both positive and negative samples. A purely-positive output
	// would indicate a sign bug in the generator.
	pos, neg := 0, 0
	for _, s := range out {
		if s > 0 {
			pos++
		}
		if s < 0 {
			neg++
		}
	}
	if pos == 0 || neg == 0 {
		t.Fatalf("white noise has no sign variation: pos=%d neg=%d (want both > 0)", pos, neg)
	}
}

// --- BrownNoiseGenerator ---

func TestBrownNoiseGenerator_BoundedOutput(t *testing.T) {
	g := NewBrownNoiseGenerator(44100)
	const N = 16384
	out := g.Next(N)
	if len(out) != N {
		t.Fatalf("Next(%d) returned %d samples, want %d", N, len(out), N)
	}
	for i, s := range out {
		if s < math.MinInt16 || s > math.MaxInt16 {
			t.Fatalf("sample[%d] = %d, out of int16 range (brown must stay bounded across many samples)",
				i, s)
		}
	}
}

// --- RainGenerator ---

func TestRainGenerator_DeterministicOutput(t *testing.T) {
	// Same seed → byte-identical output across two independent
	// generators. This is the contract downstream callers rely on
	// for golden-file playback and deterministic UI snaps.
	const seed = int64(0xC0FFEE_BEEF)
	const N = 4096

	g1 := NewRainGeneratorWithSeed(44100, seed)
	g2 := NewRainGeneratorWithSeed(44100, seed)

	a := g1.Next(N)
	b := g2.Next(N)

	if !reflect.DeepEqual(a, b) {
		// Report the first divergence to keep the failure message
		// useful when the buffers are large.
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("divergence at sample %d: g1=%d g2=%d", i, a[i], b[i])
			}
		}
	}
}

func TestRainGenerator_ResetResetsState(t *testing.T) {
	const seed = int64(0xDEAD_BEEF_CAFE)
	const N = 2048

	g := NewRainGeneratorWithSeed(44100, seed)
	first := g.Next(N)

	// Advance state by drawing more samples.
	_ = g.Next(N)

	// Reset and re-draw the same number of samples: the new buffer
	// must match the original byte-for-byte.
	g.Reset()
	again := g.Next(N)

	if !reflect.DeepEqual(first, again) {
		for i := range first {
			if first[i] != again[i] {
				t.Fatalf("Reset divergence at sample %d: first=%d again=%d",
					i, first[i], again[i])
			}
		}
	}

	// And the output must remain bounded after reset + re-draw.
	for i, s := range again {
		if s < math.MinInt16 || s > math.MaxInt16 {
			t.Fatalf("post-reset sample[%d] = %d, out of int16 range", i, s)
		}
	}
}
