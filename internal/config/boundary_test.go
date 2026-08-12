package config

import (
	"os"
	"strings"
	"testing"
)

func TestPublicBoundary_Slice3VerificationRemediation(t *testing.T) {
	t.Run("SaveV2_LoadSameVolumeHistory", func(t *testing.T) {
		target := withTempConfigHome(t)
		if err := Save(&Config{History: []string{"A", "B", "C"}, Volume: 77}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		b, _ := os.ReadFile(target)
		if s := string(b); !strings.Contains(s, "schema_version = 2") || strings.Contains(s, "last_queue") {
			t.Fatalf("Save no emitio v2: %q", s)
		}
		got, err := Load()
		if err != nil || got.Volume != 77 || len(got.History) != 3 || got.History[0] != "A" {
			t.Fatalf("Load round-trip: err=%v cfg=%+v", err, got)
		}
	})

	t.Run("LegacyLoad_RewritesFileAsV2", func(t *testing.T) {
		target := withTempConfigHome(t)
		os.WriteFile(target, []byte("last_queue = [\"A\", \"B\", \"A\", \"C\"]\nlast_track_index = 0\nvolume = 60\n"), 0o600)
		state := LoadPlaybackState()
		if state == nil || state.EffectiveVolume() != 60 {
			t.Fatalf("LoadPlaybackState incorrecto: state=%+v", state)
		}
		if got := state.History(); len(got) != 3 || got[0] != "C" || got[2] != "A" {
			t.Fatalf("History = %v, want [C B A]", got)
		}
		b, _ := os.ReadFile(target)
		if s := string(b); !strings.Contains(s, "schema_version = 2") || strings.Contains(s, "last_queue") {
			t.Fatalf("legacy no reescrito a v2: %q", s)
		}
	})
}
