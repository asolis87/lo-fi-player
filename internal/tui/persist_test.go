package tui

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asolis87/lo-fi-player/internal/audio"
	"github.com/asolis87/lo-fi-player/internal/config"
)

func withPersisterStub(t *testing.T, fn func() error) {
	t.Helper()
	orig := tuiStatePersister
	tuiStatePersister = fn
	t.Cleanup(func() { tuiStatePersister = orig })
}

func TestSignalFinalizeQ(t *testing.T) {
	var calls int32
	withPersisterStub(t, func() error { atomic.AddInt32(&calls, 1); return nil })
	_, cmd := Model{Backend: audio.NewMockBackend(), State: config.NewPlaybackState(config.Default()), Mode: ModeNowPlaying, Volume: defaultVolume}.Update(keyMsg('q'))
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q no devolvio tea.QuitMsg")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("persister calls = %d, want 1", atomic.LoadInt32(&calls))
	}
}

func TestPersistOnVolumeChange(t *testing.T) {
	state := config.NewPlaybackState(config.Default())
	var calls int32
	withPersisterStub(t, func() error { atomic.AddInt32(&calls, 1); return nil })
	m := applyKey(Model{Backend: audio.NewMockBackend(), State: state, Mode: ModeNowPlaying, Volume: defaultVolume}, keyMsg('+'))
	if atomic.LoadInt32(&calls) != 1 || m.Volume != defaultVolume+volumeStep || state.EffectiveVolume() != defaultVolume+volumeStep {
		t.Fatalf("calls=%d vol=%d stateVol=%d", atomic.LoadInt32(&calls), m.Volume, state.EffectiveVolume())
	}
}

func TestSaveErrorContinues(t *testing.T) {
	withPersisterStub(t, func() error { return fmt.Errorf("disk full") })
	out, cmd := Model{Backend: audio.NewMockBackend(), State: config.NewPlaybackState(config.Default()), Mode: ModeNowPlaying, Volume: defaultVolume}.Update(keyMsg('q'))
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q no devolvio tea.QuitMsg")
	}
	if !strings.Contains(out.(Model).LastError, "disk full") {
		t.Fatalf("LastError = %q", out.(Model).LastError)
	}
}
