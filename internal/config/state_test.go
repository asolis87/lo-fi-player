package config

import (
	"reflect"
	"testing"
)

// TestNewPlaybackState_FreshInstall_ReturnsDefaultVolume valida
// VOL-1: Default().Volume=50 produce EffectiveVolume=50 sin reescribir raw.
func TestNewPlaybackState_FreshInstall_ReturnsDefaultVolume(t *testing.T) {
	s := NewPlaybackState(Default())
	if got := s.EffectiveVolume(); got != DefaultVolume {
		t.Fatalf("EffectiveVolume() = %d, want %d", got, DefaultVolume)
	}
	if s.raw.Volume != DefaultVolume {
		t.Fatalf("raw.Volume mutated: got %d, want %d", s.raw.Volume, DefaultVolume)
	}
}

// TestNewPlaybackState_OutOfRange_Defaults50 valida VOL-2: raw < 0 o
// raw > 100 se mapea a 50 sin reescribir el raw. raw = 0 es valido (mute).
func TestNewPlaybackState_OutOfRange_Defaults50(t *testing.T) {
	cases := []struct {
		name       string
		raw        int
		wantEffVol int
	}{
		{"negative", -1, DefaultVolume},
		{"above_max", 150, DefaultVolume},
		{"one_above_max", 101, DefaultVolume},
		{"hundred_in_range", 100, 100},
		{"one_in_range", 1, 1},
		{"zero_in_range", 0, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := NewPlaybackState(Config{Volume: tc.raw})
			if got := s.EffectiveVolume(); got != tc.wantEffVol {
				t.Fatalf("EffectiveVolume() for raw=%d = %d, want %d", tc.raw, got, tc.wantEffVol)
			}
			if s.raw.Volume != tc.raw {
				t.Fatalf("raw.Volume mutated: raw=%d -> %d", tc.raw, s.raw.Volume)
			}
		})
	}
}

// TestSetVolume_RejectsOutOfRange valida el rechazo de SetVolume
// fuera de [0, 100]: el raw no se reescribe y devuelve
// ErrInvalidVolume. 0 es valido y se acepta.
func TestSetVolume_RejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name    string
		in      int
		wantErr bool
	}{
		{"negative", -1, true},
		{"above_max", 101, true},
		{"hundred_in_range", 100, false},
		{"zero_mute_valid", 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := NewPlaybackState(Config{Volume: 25})
			err := s.SetVolume(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SetVolume(%d) err = %v, wantErr = %v", tc.in, err, tc.wantErr)
			}
			if tc.wantErr {
				if err != ErrInvalidVolume {
					t.Fatalf("SetVolume(%d) err = %v, want ErrInvalidVolume", tc.in, err)
				}
				if s.raw.Volume != 25 {
					t.Fatalf("raw.Volume mutated on error: got %d, want 25", s.raw.Volume)
				}
				return
			}
			if s.raw.Volume != tc.in {
				t.Fatalf("raw.Volume after SetVolume(%d) = %d, want %d", tc.in, s.raw.Volume, tc.in)
			}
		})
	}
}

// TestHistory_ReturnsDefensiveCopy valida que History() devuelve
// una copia: mutar el slice retornado no afecta el estado interno.
func TestHistory_ReturnsDefensiveCopy(t *testing.T) {
	s := NewPlaybackState(Config{Volume: 50, History: []string{"A", "B"}})
	got := s.History()
	got[0] = "MUTATED"
	if again := s.History(); again[0] != "A" {
		t.Fatalf("History internal mutated through returned slice: %v", again)
	}
}

// TestRecordPlayed_SpecScenarios cubre HIST-1 (3 escenarios) y
// HIST-2 (1 escenario) en una sola tabla.
func TestRecordPlayed_SpecScenarios(t *testing.T) {
	fullHistory := make([]string, 25)
	for i := range fullHistory {
		fullHistory[i] = string(rune('a' + i))
	}
	cases := []struct {
		name   string
		seed   []string
		played string
		want   []string
	}{
		{"prepend_new_track", []string{"A", "B"}, "C", []string{"C", "A", "B"}},
		{"replay_front_noop", []string{"A", "B", "C"}, "A", []string{"A", "B", "C"}},
		{"replay_middle_to_front", []string{"A", "B", "C"}, "B", []string{"B", "A", "C"}},
		{"empty_history", nil, "X", []string{"X"}},
		{"cap_evicts_oldest", fullHistory, "NEW", append([]string{"NEW"}, fullHistory[:24]...)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := NewPlaybackState(Config{Volume: 50, History: tc.seed})
			if err := s.RecordPlayed(tc.played); err != nil {
				t.Fatalf("RecordPlayed(%q) error = %v", tc.played, err)
			}
			if got := s.History(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("History() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRecordPlayed_CapTailPosition valida que tras evictar la
// entrada mas antigua en historial lleno, el segundo mas antiguo
// ("x") ocupa el tail (idx 24) y la nueva ("NEW") el front.
func TestRecordPlayed_CapTailPosition(t *testing.T) {
	history := make([]string, 25)
	for i := range history {
		history[i] = string(rune('a' + i))
	}
	s := NewPlaybackState(Config{Volume: 50, History: history})
	if err := s.RecordPlayed("NEW"); err != nil {
		t.Fatalf("RecordPlayed(NEW) error = %v", err)
	}
	got := s.History()
	if got[0] != "NEW" || got[24] != "x" {
		t.Fatalf("after eviction front=%q tail=%q, want NEW/x", got[0], got[24])
	}
}

// TestRecordPlayed_EmptyIDRejected valida que RecordPlayed con id
// vacio retorna error y no muta el historial.
func TestRecordPlayed_EmptyIDRejected(t *testing.T) {
	s := NewPlaybackState(Config{Volume: 50, History: []string{"A"}})
	if err := s.RecordPlayed(""); err == nil {
		t.Fatalf("RecordPlayed(\"\") returned nil error, want non-nil")
	}
	if got := s.History(); !reflect.DeepEqual(got, []string{"A"}) {
		t.Fatalf("History() after rejected empty id = %v, want [A]", got)
	}
}
