package audio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeMpv is a tiny JSON-IPC server used by the adapter tests. It
// listens on a private Unix socket, accepts one connection, and
// replies {"error":"success","request_id":N} to every command except
// quit. crash() yanks the server-side conn to drive the recovery
// path and blocks until the per-handler goroutine has fully exited
// (so the close has propagated to the kernel before crash returns).
type fakeMpv struct {
	dir         string
	sock        string
	ln          net.Listener
	mu          sync.Mutex
	conn        net.Conn
	handlerDone chan struct{}
	cmds        []ipcRequest
	done        chan struct{}

	// onLoad, when non-nil, is invoked after the fake sends the
	// success reply to a "loadfile" command. It is the seam used
	// by TestMpvGenCorrelation to inject start-file / end-file
	// events that exercise the generation correlation path. The
	// default (nil) keeps the pre-PR-1B behavior: the fake only
	// replies to commands and never emits async events, so the
	// existing tests (which never read b.Events() during a Load)
	// keep passing.
	onLoad func(conn net.Conn, req ipcRequest)

	// entryCounter allocates monotonically increasing playlist
	// entry IDs for the events injected via onLoad. Tests that
	// need a specific ID (e.g. a never-seen "unknown" entry) can
	// set it directly under f.mu.
	entryCounter int
}

// nextEntryID allocates a fresh, never-reused playlist entry id.
// Used by the onLoad callbacks to keep start-file and end-file
// correlated in the backend's entryGen map.
func (f *fakeMpv) nextEntryID() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entryCounter++
	return f.entryCounter
}

// sendAsync writes one mpv event frame (start-file, end-file, etc.)
// on the connection. The event field is set; data is JSON-encoded
// and sent as the reply's data field. The mpv client treats each
// newline-delimited frame as one event.
func (f *fakeMpv) sendAsync(conn net.Conn, event string, data any) {
	payload, _ := json.Marshal(data)
	msg, _ := json.Marshal(ipcReply{Event: event, Data: payload})
	_, _ = conn.Write(append(msg, '\n'))
}

// fakeSocketDir returns a private directory whose absolute path is
// short enough to keep the resulting socket path under macOS's 104-byte
// sockaddr_un.sun_path limit. On Linux, os.TempDir() is typically /tmp
// and short enough; on macOS, $TMPDIR (/var/folders/.../T/...) plus a
// typical test name routinely exceeds the limit and bind(2) fails with
// "invalid argument", so we anchor under /tmp.
func fakeSocketDir(t *testing.T) string {
	t.Helper()
	const maxBase = 32
	base := os.TempDir()
	if len(base) > maxBase {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "lo-fi-mpv-")
	if err != nil {
		t.Fatalf("fake socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newFakeMpv(t *testing.T) *fakeMpv {
	t.Helper()
	dir := fakeSocketDir(t)
	sock := filepath.Join(dir, "ipc.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("fake listen: %v", err)
	}
	f := &fakeMpv{dir: dir, sock: sock, ln: ln, done: make(chan struct{})}
	go f.serve()
	return f
}

func (f *fakeMpv) serve() {
	defer close(f.done)
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = conn
		f.handlerDone = make(chan struct{})
		done := f.handlerDone
		f.mu.Unlock()
		f.handle(conn)
		close(done)
	}
}

func (f *fakeMpv) handle(conn net.Conn) {
	defer func() {
		f.mu.Lock()
		f.conn = nil
		f.mu.Unlock()
		_ = conn.Close()
	}()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		var req ipcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		f.mu.Lock()
		f.cmds = append(f.cmds, req)
		f.mu.Unlock()
		if len(req.Command) >= 1 && req.Command[0] == "quit" {
			return
		}
		reply, _ := json.Marshal(ipcReply{Error: "success", RequestID: req.RequestID})
		if _, err := conn.Write(append(reply, '\n')); err != nil {
			return
		}
		// PR-1B seam: after acknowledging a loadfile, optionally
		// inject async start-file / end-file frames so the backend
		// can be tested under realistic playlist correlation.
		// Existing tests leave onLoad nil and see no async events.
		if len(req.Command) >= 1 && req.Command[0] == "loadfile" && f.onLoad != nil {
			f.onLoad(conn, req)
		}
	}
}

func (f *fakeMpv) crash() {
	f.mu.Lock()
	done := f.handlerDone
	conn := f.conn
	f.conn = nil
	f.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	// Wait for the per-handler goroutine to finish so the server-side
	// close fully propagates to the kernel before we let the test
	// drive the next backend operation.
	if done != nil {
		<-done
	}
	// Closing the listener mimics a real crash that takes the whole
	// process down: any subsequent dial fails with "connection
	// refused", forcing the backend to exhaust its recovery budget
	// instead of silently reconnecting to a fresh fake handler.
	_ = f.ln.Close()
}

func (f *fakeMpv) commands() []ipcRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ipcRequest, len(f.cmds))
	copy(out, f.cmds)
	return out
}

func (f *fakeMpv) waitForCount(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(f.commands()) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fake only saw %d commands, want >= %d", len(f.commands()), n)
}

// dialerCounter returns a WithDialer whose dial function returns
// connections to fakes in the supplied order; any dial beyond the
// last reuses the last fake (so the post-recovery handshake can
// land on the fresh process).
func dialerCounter(fakes ...*fakeMpv) Option {
	var n atomic.Int32
	return WithDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		i := int(n.Add(1)) - 1
		if i >= len(fakes) {
			i = len(fakes) - 1
		}
		return net.Dial("unix", fakes[i].sock)
	})
}

func newBackendForFake(t *testing.T, fake *fakeMpv, extra ...Option) *MpvBackend {
	t.Helper()
	opts := []Option{
		WithBinary("/bin/cat"), // harmless binary; tests bypass via WithDialer
		WithSocketPath(fake.sock),
		WithReadyTimeout(2 * time.Second),
		WithDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.Dial("unix", fake.sock)
		}),
	}
	opts = append(opts, extra...)
	b, err := NewMpvBackend(opts...)
	if err != nil {
		t.Fatalf("NewMpvBackend: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func hasCommand(cmds []ipcRequest, name string) bool {
	for _, c := range cmds {
		if len(c.Command) >= 1 && c.Command[0] == name {
			return true
		}
	}
	return false
}

func volumeSetCount(cmds []ipcRequest) int {
	n := 0
	for _, c := range cmds {
		if len(c.Command) >= 3 && c.Command[0] == "set_property" && c.Command[1] == "volume" {
			n++
		}
	}
	return n
}

// --- Tests ---

func TestMpvBackend_LoadAndPlay_OverFakeSocket(t *testing.T) {
	fake := newFakeMpv(t)
	b := newBackendForFake(t, fake)

	if err := b.Load(Track{ID: "track-1", Path: "/tmp/track-1.mp3"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	fake.waitForCount(t, 3) // handshake + loadfile + set_property pause

	cmds := fake.commands()
	if !hasCommand(cmds, "observe_property") {
		t.Fatalf("expected observe_property handshake, got %v", commandNames(cmds))
	}
	if !hasCommand(cmds, "loadfile") {
		t.Fatalf("expected loadfile command, got %v", commandNames(cmds))
	}
	if !hasCommand(cmds, "set_property") {
		t.Fatalf("expected set_property command (Play), got %v", commandNames(cmds))
	}
}

func TestMpvBackend_Pause_OverFakeSocket(t *testing.T) {
	fake := newFakeMpv(t)
	b := newBackendForFake(t, fake)

	if err := b.Load(Track{ID: "t", Path: "/tmp/t.mp3"}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := b.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	fake.waitForCount(t, 4) // handshake + loadfile + play + pause

	// Both Play and Pause issue set_property pause true/false.
	playPause := 0
	for _, c := range fake.commands() {
		if len(c.Command) >= 3 && c.Command[0] == "set_property" && c.Command[1] == "pause" {
			playPause++
		}
	}
	if playPause != 2 {
		t.Fatalf("pause-flips = %d, want 2 (Play+Pause)", playPause)
	}
}

func TestMpvBackend_SetVolume_Clamped(t *testing.T) {
	fake := newFakeMpv(t)
	b := newBackendForFake(t, fake)

	if err := b.SetVolume(50); err != nil {
		t.Fatalf("SetVolume(50): %v", err)
	}
	if err := b.SetVolume(-1); !errors.Is(err, ErrVolumeOutOfRange) {
		t.Fatalf("SetVolume(-1) = %v, want ErrVolumeOutOfRange", err)
	}
	if err := b.SetVolume(101); !errors.Is(err, ErrVolumeOutOfRange) {
		t.Fatalf("SetVolume(101) = %v, want ErrVolumeOutOfRange", err)
	}
	fake.waitForCount(t, 2) // handshake + set_property volume

	if got := volumeSetCount(fake.commands()); got != 1 {
		t.Fatalf("volume set count = %d, want 1 (out-of-range rejected pre-IPC)", got)
	}
}

func TestMpvBackend_MissingBinary_FailsCleanly(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	_, err := NewMpvBackend(WithBinary("/no/such/path/mpv-lofi-xyz"))
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if !errors.Is(err, ErrMpvNotFound) {
		t.Fatalf("error = %v, want wrapped ErrMpvNotFound", err)
	}
	if !strings.Contains(err.Error(), "/no/such/path/mpv-lofi-xyz") {
		t.Fatalf("error %q does not mention the missing path", err.Error())
	}
	if !strings.Contains(err.Error(), "install mpv") {
		t.Fatalf("error %q lacks actionable install guidance", err.Error())
	}
}

func TestMpvBackend_Crash_RecoversWithFreshProcess(t *testing.T) {
	fake1 := newFakeMpv(t)
	fake2 := newFakeMpv(t)

	dialer := dialerCounter(fake1, fake2)
	b, err := NewMpvBackend(
		WithBinary("/bin/cat"),
		WithSocketPath(fake1.sock),
		WithReadyTimeout(2*time.Second),
		dialer,
	)
	if err != nil {
		t.Fatalf("NewMpvBackend: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	// Round 1: dial fake1, handshake OK, Load OK.
	if err := b.Load(Track{ID: "t1", Path: "/tmp/t1.mp3"}); err != nil {
		t.Fatalf("Load (round 1): %v", err)
	}
	fake1.waitForCount(t, 2)

	// fake1 "crashes" — next call triggers recovery.
	fake1.crash()

	if err := b.SetVolume(50); err != nil {
		t.Fatalf("SetVolume (recovery round): %v", err)
	}
	fake2.waitForCount(t, 2) // recovery handshake + set_property

	// fake2 also crashes — recovery budget exhausted.
	fake2.crash()

	err = b.SetVolume(60)
	if err == nil {
		t.Fatal("SetVolume after second crash: expected error, got nil")
	}
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("SetVolume after second crash = %v, want ErrBackendUnavailable", err)
	}
	select {
	case ev := <-b.Events():
		if ev.Type != EventError {
			t.Fatalf("event type = %v, want %v", ev.Type, EventError)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected error event within 1s, got none")
	}

	// Sticky: subsequent calls also fail without IPC.
	if err := b.Play(); !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("Play after sticky error = %v, want ErrBackendUnavailable", err)
	}
}

func TestMpvBackend_SocketTeardown_RemovesFile(t *testing.T) {
	tmpRoot := t.TempDir()
	b, err := NewMpvBackend(
		WithBinary("/bin/cat"),
		WithTempDir(tmpRoot),
		WithReadyTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("NewMpvBackend: %v", err)
	}

	// Backend owns exactly one private dir under tmpRoot.
	entries, err := os.ReadDir(tmpRoot)
	if err != nil {
		t.Fatalf("ReadDir pre-Close: %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("pre-Close entries = %d, want 1 private dir", len(entries))
	}
	privateDir := filepath.Join(tmpRoot, entries[0].Name())
	socketPath := filepath.Join(privateDir, "ipc.sock")

	// Plant a fake socket file so we can prove Close removes it.
	if err := os.WriteFile(socketPath, []byte("not a real socket"), 0o600); err != nil {
		t.Fatalf("plant fake socket: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket file still present after Close: stat err = %v", err)
	}
	if _, err := os.Stat(privateDir); !os.IsNotExist(err) {
		t.Fatalf("private temp dir still present after Close: stat err = %v", err)
	}

	// Idempotency: a second Close must be a no-op (no error, no panic).
	if err := b.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestMpvGenCorrelation: correlation, FIFO, Close safety, recovery.
func TestMpvGenCorrelation(t *testing.T) {

	startEnd := func(fake *fakeMpv) {
		fake.onLoad = func(conn net.Conn, _ ipcRequest) {
			id := fake.nextEntryID()
			fake.sendAsync(conn, "start-file", map[string]any{"playlist_entry_id": id})
			fake.sendAsync(conn, "end-file", map[string]any{"playlist_entry_id": id, "reason": "eof"})
		}
	}

	t.Run("CorrelatedLoadEmitsEventEnd", func(t *testing.T) {
		fake := newFakeMpv(t)
		startEnd(fake)
		b := newBackendForFake(t, fake)

		if err := b.Load(Track{ID: "t1", Path: "/tmp/t1.mp3", Generation: 1}); err != nil {
			t.Fatalf("Load: %v", err)
		}
		endEv := mustEvent(t, b, time.Second)
		if endEv.Type != EventEnd || endEv.Generation != 1 || endEv.Message != "eof" {
			t.Fatalf("event = %+v, want EventEnd{Generation:1, Message:eof}", endEv)
		}
	})

	t.Run("UnknownEntryIDIgnored", func(t *testing.T) {
		fake := newFakeMpv(t)
		fake.onLoad = func(conn net.Conn, _ ipcRequest) {
			fake.sendAsync(conn, "end-file", map[string]any{"playlist_entry_id": 999, "reason": "eof"})
		}
		b := newBackendForFake(t, fake)
		if err := b.Load(Track{ID: "t1", Path: "/tmp/t1.mp3", Generation: 1}); err != nil {
			t.Fatalf("Load: %v", err)
		}
		select {
		case ev := <-b.Events():
			t.Fatalf("unexpected event from unknown entry_id: %+v", ev)
		case <-time.After(200 * time.Millisecond):
		}
	})

	t.Run("GenerationZeroHeadless", func(t *testing.T) {
		fake := newFakeMpv(t)
		startEnd(fake)
		b := newBackendForFake(t, fake)
		if err := b.Load(Track{ID: "h", Path: "procedural:rain", Generation: 0}); err != nil {
			t.Fatalf("Load: %v", err)
		}
		endEv := mustEvent(t, b, time.Second)
		if endEv.Type != EventEnd || endEv.Generation != 0 {
			t.Fatalf("event = %+v, want EventEnd{Generation:0}", endEv)
		}
	})

	t.Run("BurstFIFOLossless", func(t *testing.T) {
		const burst = 80
		fake := newFakeMpv(t)
		fake.onLoad = func(conn net.Conn, _ ipcRequest) {
			for i := 0; i < burst; i++ {
				id := fake.nextEntryID()
				fake.sendAsync(conn, "start-file", map[string]any{"playlist_entry_id": id})
				fake.sendAsync(conn, "end-file", map[string]any{"playlist_entry_id": id, "reason": "eof"})
			}
		}
		b := newBackendForFake(t, fake)
		if err := b.Load(Track{ID: "burst", Path: "/tmp/burst.mp3", Generation: 1}); err != nil {
			t.Fatalf("Load: %v", err)
		}
		for i := 0; i < burst; i++ {
			ev := mustEvent(t, b, 5*time.Second)
			if ev.Type != EventEnd || ev.Generation != 1 {
				t.Fatalf("event %d = %+v, want EventEnd{Generation:1}", i, ev)
			}
		}
	})

	t.Run("CloseCancelsEmitterAndIsIdempotent", func(t *testing.T) {
		const burst = 200
		fake := newFakeMpv(t)
		fake.onLoad = func(conn net.Conn, _ ipcRequest) {
			for i := 0; i < burst; i++ {
				id := fake.nextEntryID()
				fake.sendAsync(conn, "start-file", map[string]any{"playlist_entry_id": id})
				fake.sendAsync(conn, "end-file", map[string]any{"playlist_entry_id": id, "reason": "eof"})
			}
		}
		b := newBackendForFake(t, fake)
		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			for range b.Events() {
			}
		}()
		if err := b.Load(Track{ID: "burst", Path: "/tmp/burst.mp3", Generation: 1}); err != nil {
			t.Fatalf("Load: %v", err)
		}
		if err := b.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		select {
		case <-readerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("reader did not exit after Close")
		}
		if err := b.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
		select {
		case ev, ok := <-b.Events():
			if ok {
				t.Fatalf("post-Close event = %+v, want zero/closed", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("Events() did not close after Close")
		}
	})

	t.Run("RecoveryClearsEntryGen", func(t *testing.T) {
		// Causal proof: fake1 emits start-file(1) without end-file, leaving entryGen[1]=gen1.
		// After crash + recovery, the stale end-file(1) from fake2 MUST be ignored.
		fake1, fake2 := newFakeMpv(t), newFakeMpv(t)
		b, err := NewMpvBackend(
			WithBinary("/bin/cat"), WithSocketPath(fake1.sock),
			WithReadyTimeout(2*time.Second), dialerCounter(fake1, fake2),
		)
		if err != nil {
			t.Fatalf("NewMpvBackend: %v", err)
		}
		// Phase 1: fake1 injects ONLY start-file(1) and closes the conn so the
		// readLoop drains the start-file before teardown returns, setting entryGen[1]=gen1.
		fake1.onLoad = func(conn net.Conn, _ ipcRequest) {
			id := fake1.nextEntryID()
			fake1.sendAsync(conn, "start-file", map[string]any{"playlist_entry_id": id})
			_ = conn.Close()
		}
		if err := b.Load(Track{ID: "t1", Path: "/tmp/t1.mp3", Generation: 1}); err != nil {
			t.Fatalf("Load: %v", err)
		}
		fake1.crash()
		fake2.onLoad = func(conn net.Conn, _ ipcRequest) {
			// Stale end-file(1) first — must be ignored.
			fake2.sendAsync(conn, "end-file", map[string]any{"playlist_entry_id": 1, "reason": "eof"})
			id := fake2.nextEntryID()
			fake2.sendAsync(conn, "start-file", map[string]any{"playlist_entry_id": id})
			fake2.sendAsync(conn, "end-file", map[string]any{"playlist_entry_id": id, "reason": "eof"})
		}
		if err := b.SetVolume(50); err != nil {
			t.Fatalf("SetVolume: %v", err)
		}
		if err := b.Load(Track{ID: "t2", Path: "/tmp/t2.mp3", Generation: 2}); err != nil {
			t.Fatalf("Load: %v", err)
		}
		// First event MUST be EventEnd{Gen:2}; if clearEntryGen was disabled, stale gen would surface first.
		ev := mustEvent(t, b, time.Second)
		if ev.Type != EventEnd || ev.Generation != 2 {
			t.Fatalf("event = %+v, want EventEnd{Gen:2}", ev)
		}
	})
}

func commandNames(cmds []ipcRequest) string {
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		if len(c.Command) > 0 {
			if s, ok := c.Command[0].(string); ok {
				names = append(names, s)
				continue
			}
			names = append(names, "?")
		}
	}
	return strings.Join(names, ", ")
}
