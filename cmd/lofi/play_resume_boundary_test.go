package main

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/asolis87/lo-fi-player/internal/catalog"
	"github.com/asolis87/lo-fi-player/internal/config"
	"github.com/asolis87/lo-fi-player/internal/tui"
)

// TestResumeBoundary_Slice3VerificationRemediation: dormidos en verify
// report (FILTER dormido, volume-only descartado, SelectedIdx sin reconciliar).
func TestResumeBoundary_Slice3VerificationRemediation(t *testing.T) {
	t.Run("FilterDropsUnknownIDs_ReconciledHistory_Idx0", func(t *testing.T) {
		stub := &stubLauncher{err: nil}
		withStubLauncher(t, stub)
		withStdinIsTTYStub(t, true)
		withResumeReaderStub(t, bytes.NewReader([]byte{'y'}))
		catalogForResumeStub(t, func() (*catalog.Catalog, error) {
			return &catalog.Catalog{Tracks: []catalog.Track{{ID: "A"}, {ID: "C"}}}, nil
		})
		withLoadStateForResumeStub(t, func() *config.PlaybackState {
			return stateForResumeWithVolumeAndHistory(t, 65, []string{"A", "Z-MISSING", "C", "Q-MISSING"})
		})
		code, _ := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
		if code != 0 || stub.model.State == nil {
			t.Fatalf("code=%d state=%+v", code, stub.model.State)
		}
		if got := stub.model.State.History(); !reflect.DeepEqual(got, []string{"A", "C"}) {
			t.Fatalf("State.History = %v, want [A C]", got)
		}
		if stub.model.SelectedIdx != 0 || stub.model.Volume != 65 || stub.model.Mode != tui.ModeNowPlaying {
			t.Fatalf("Idx/Vol/Mode = %d/%d/%v want 0/65/NowPlaying", stub.model.SelectedIdx, stub.model.Volume, stub.model.Mode)
		}
	})

	t.Run("VolumeOnlyPriorState_Hydrates", func(t *testing.T) {
		stub := &stubLauncher{err: nil}
		withStubLauncher(t, stub)
		withStdinIsTTYStub(t, true)
		withResumeReaderStub(t, bytes.NewReader([]byte{'y'}))
		catalogForResumeStub(t, func() (*catalog.Catalog, error) {
			return &catalog.Catalog{Tracks: []catalog.Track{{ID: "anything"}}}, nil
		})
		withLoadStateForResumeStub(t, func() *config.PlaybackState {
			return stateForResumeWithVolumeAndHistory(t, 88, []string{})
		})
		code, _ := captureStderr(t, func() int { return codeFor(runPlay(nil)) })
		if code != 0 || stub.model.Volume != 88 {
			t.Fatalf("code=%d vol=%d, want 0/88", code, stub.model.Volume)
		}
		if stub.model.State == nil || len(stub.model.State.History()) != 0 {
			t.Fatalf("volume-only state NO hidrato: %+v", stub.model.State)
		}
	})
}
