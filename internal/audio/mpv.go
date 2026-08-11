// Package audio — mpv JSON-IPC adapter for the AudioBackend port.
//
// Architectural guarantees (asserted by mpv_test.go): argv-only
// subprocess launch; private IPC socket in os.MkdirTemp (0o700);
// serialized request IDs dispatched by a single reader goroutine;
// bounded readiness; bounded teardown. No cgo, no libmpv.
package audio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrMpvNotFound       = errors.New("audio: mpv binary not found")
	ErrBackendUnavailable = errors.New("audio: backend unavailable")
)

type EventType string

const (
	EventError EventType = "error"
	EventEnd   EventType = "end-file"
)

type Event struct {
	Type    EventType
	Message string
}

type ipcRequest struct {
	Command   []any `json:"command"`
	RequestID int   `json:"request_id"`
}

type ipcReply struct {
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data,omitempty"`
	Event     string          `json:"event,omitempty"`
	RequestID int             `json:"request_id,omitempty"`
}

type Option func(*MpvBackend)

func WithBinary(p string) Option             { return func(b *MpvBackend) { b.bin = p } }
func WithSocketPath(p string) Option          { return func(b *MpvBackend) { b.socketPath = p } }
func WithTempDir(d string) Option             { return func(b *MpvBackend) { b.tmpDir = d } }
func WithReadyTimeout(d time.Duration) Option { return func(b *MpvBackend) { b.readyTimeout = d } }
func WithRecoverMax(n int) Option             { return func(b *MpvBackend) { b.recoverMax = n } }
func WithDialer(fn func(context.Context, string) (net.Conn, error)) Option {
	return func(b *MpvBackend) { b.dialFn = fn }
}

type MpvBackend struct {
	bin          string
	socketPath   string
	tmpDir       string // non-empty ⇒ Close removes the dir
	readyTimeout time.Duration
	recoverMax   int
	dialFn       func(context.Context, string) (net.Conn, error)

	mu        sync.Mutex
	cmd       *exec.Cmd
	conn      net.Conn
	scanner   *bufio.Scanner
	pending   map[int]chan *ipcReply
	nextID    int
	closed    bool
	ready     bool
	playing   bool
	recovered int
	stickyErr error
	events    chan Event
	readerWG  sync.WaitGroup
}

// NewMpvBackend validates the binary path (returning ErrMpvNotFound
// when missing) and allocates a private temp dir for the socket,
// but does NOT spawn the subprocess; the first AudioBackend call
// triggers the lazy start.
func NewMpvBackend(opts ...Option) (*MpvBackend, error) {
	b := &MpvBackend{
		bin:          "mpv",
		readyTimeout: 2 * time.Second,
		recoverMax:   1,
		events:       make(chan Event, 4),
		pending:      make(map[int]chan *ipcReply),
	}
	for _, opt := range opts {
		opt(b)
	}
	resolved, err := exec.LookPath(b.bin)
	if err != nil {
		return nil, fmt.Errorf("%w: %q (install mpv or set LOFI_MPV)", ErrMpvNotFound, b.bin)
	}
	b.bin = resolved

	if b.socketPath == "" {
		parent := b.tmpDir
		if parent == "" {
			parent = os.TempDir()
		}
		dir, err := os.MkdirTemp(parent, "lofi-mpv-*")
		if err != nil {
			return nil, fmt.Errorf("audio: create ipc dir: %w", err)
		}
		_ = os.Chmod(dir, 0o700)
		b.tmpDir = dir
		b.socketPath = filepath.Join(dir, "ipc.sock")
	}
	return b, nil
}

func (b *MpvBackend) Events() <-chan Event { return b.events }

var _ AudioBackend = (*MpvBackend)(nil)

func (b *MpvBackend) Load(t Track) error {
	if err := b.call(context.Background(), []any{"loadfile", t.Path, "append"}); err != nil {
		return err
	}
	b.mu.Lock()
	b.playing = false
	b.mu.Unlock()
	return nil
}
func (b *MpvBackend) Play() error {
	if err := b.call(context.Background(), []any{"set_property", "pause", false}); err != nil {
		return err
	}
	b.mu.Lock()
	b.playing = true
	b.mu.Unlock()
	return nil
}
func (b *MpvBackend) Pause() error {
	if err := b.call(context.Background(), []any{"set_property", "pause", true}); err != nil {
		return err
	}
	b.mu.Lock()
	b.playing = false
	b.mu.Unlock()
	return nil
}
func (b *MpvBackend) Stop() error {
	if err := b.call(context.Background(), []any{"stop"}); err != nil {
		return err
	}
	b.mu.Lock()
	b.playing = false
	b.mu.Unlock()
	return nil
}
func (b *MpvBackend) SetVolume(v int) error {
	if v < 0 || v > 100 {
		return ErrVolumeOutOfRange
	}
	return b.call(context.Background(), []any{"set_property", "volume", v})
}
func (b *MpvBackend) Seek(ms int) error {
	return b.call(context.Background(), []any{"seek", float64(ms) / 1000.0, "absolute"})
}

// State reports the backend's last-known playback state. The reported
// position is always 0; querying mpv's time-pos on every call would
// double the IPC traffic for marginal value in slice #1.
func (b *MpvBackend) State() (bool, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false, 0, ErrBackendUnavailable
	}
	if b.stickyErr != nil {
		return false, 0, b.stickyErr
	}
	return b.playing, 0, nil
}

// Close permanently shuts the backend down. It is idempotent; the
// socket file and any auto-created temp directory are removed.
func (b *MpvBackend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.ready = false
	b.mu.Unlock()

	b.teardown()
	if b.tmpDir != "" {
		_ = os.RemoveAll(b.tmpDir)
	}
	return nil
}

func (b *MpvBackend) call(ctx context.Context, command []any) error {
	if err := b.ensureReady(ctx); err != nil {
		return err
	}
	reply, err := b.send(ctx, command, true)
	if err != nil {
		return err
	}
	if reply.Error != "success" {
		return fmt.Errorf("audio: mpv reply: %s", reply.Error)
	}
	return nil
}

// ensureReady lazily starts the mpv subprocess (or re-dials the
// socket when a test dialer is configured) and completes the IPC
// handshake. Recovery (one fresh-process retry) is attempted on
// spawn or handshake failure; the second failure transitions to
// the sticky-error state.
func (b *MpvBackend) ensureReady(ctx context.Context) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrBackendUnavailable
	}
	if b.stickyErr != nil {
		err := b.stickyErr
		b.mu.Unlock()
		return err
	}
	if b.ready {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, b.readyTimeout)
	defer cancel()

	start := func() error { return b.spawn(ctx) }
	hs := func() error { return b.handshake(ctx) }
	if err := start(); err != nil {
		if !b.tryRecover(ctx) {
			return b.setSticky(err)
		}
		if err := start(); err != nil {
			return b.setSticky(err)
		}
	}
	if err := hs(); err != nil {
		if !b.tryRecover(ctx) {
			return b.setSticky(err)
		}
		if err := start(); err != nil {
			return b.setSticky(err)
		}
		if err := hs(); err != nil {
			return b.setSticky(err)
		}
	}

	b.mu.Lock()
	b.ready = true
	b.mu.Unlock()
	return nil
}

// spawn starts the mpv subprocess (when no test dialer is in play)
// and connects to the IPC socket. argv is literal; no shell.
func (b *MpvBackend) spawn(ctx context.Context) error {
	if b.dialFn == nil {
		args := []string{
			"--idle", "--no-terminal", "--force-window=no", "--quiet",
			"--input-ipc-server=" + b.socketPath,
		}
		cmd := exec.CommandContext(ctx, b.bin, args...)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("audio: start mpv: %w", err)
		}
		b.mu.Lock()
		b.cmd = cmd
		b.mu.Unlock()
	}

	conn, err := b.dialSocket(ctx)
	if err != nil {
		b.teardown()
		return err
	}
	b.mu.Lock()
	b.conn = conn
	b.scanner = bufio.NewScanner(conn)
	b.scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	b.mu.Unlock()

	b.readerWG.Add(1)
	go b.readLoop()
	return nil
}

func (b *MpvBackend) dialSocket(ctx context.Context) (net.Conn, error) {
	if b.dialFn != nil {
		return b.dialFn(ctx, b.socketPath)
	}
	for {
		d := net.Dialer{Timeout: 50 * time.Millisecond}
		conn, err := d.DialContext(ctx, "unix", b.socketPath)
		if err == nil {
			return conn, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("audio: mpv socket %s not ready: %w", b.socketPath, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// handshake issues observe_property and waits for the reply,
// proving the IPC socket is actually serving commands.
func (b *MpvBackend) handshake(ctx context.Context) error {
	reply, err := b.send(ctx, []any{"observe_property", 1, "pause"}, false)
	if err != nil {
		return fmt.Errorf("audio: handshake: %w", err)
	}
	if reply.Error != "success" {
		return fmt.Errorf("audio: handshake reply: %s", reply.Error)
	}
	return nil
}

// send encodes one IPC request, writes it to mpv, and waits for the
// matching reply. The write deadline is taken from ctx when present,
// so a stalled mpv can never block the caller forever. canRecover=false
// skips the recovery branch (used by handshake itself).
func (b *MpvBackend) send(ctx context.Context, command []any, canRecover bool) (*ipcReply, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, ErrBackendUnavailable
	}
	if b.conn == nil {
		b.mu.Unlock()
		return nil, ErrBackendUnavailable
	}
	id := b.nextID
	b.nextID++
	replyCh := make(chan *ipcReply, 1)
	b.pending[id] = replyCh
	conn := b.conn
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	payload, err := json.Marshal(ipcRequest{Command: command, RequestID: id})
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')

	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(dl)
	}
	if _, err := conn.Write(payload); err != nil {
		_ = conn.SetWriteDeadline(time.Time{})
		if canRecover {
			return b.handleFailure(ctx, command, err)
		}
		return nil, err
	}
	_ = conn.SetWriteDeadline(time.Time{})

	select {
	case reply := <-replyCh:
		if reply == nil {
			return nil, ErrBackendUnavailable
		}
		return reply, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// handleFailure tears the dead connection down and tries one fresh
// start (subject to recoverMax). The retry uses canRecover=false to
// guarantee termination. On the second failure the backend
// transitions to the sticky-error state, emits EventError, and
// every subsequent call returns ErrBackendUnavailable.
func (b *MpvBackend) handleFailure(ctx context.Context, command []any, cause error) (*ipcReply, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, ErrBackendUnavailable
	}
	if b.recovered >= b.recoverMax {
		err := b.setStickyLocked(cause)
		b.mu.Unlock()
		return nil, err
	}
	b.recovered++
	b.ready = false
	b.mu.Unlock()

	b.teardown()

	ctx, cancel := context.WithTimeout(ctx, b.readyTimeout)
	defer cancel()

	if err := b.spawn(ctx); err != nil {
		return nil, b.setSticky(err)
	}
	if err := b.handshake(ctx); err != nil {
		return nil, b.setSticky(err)
	}
	return b.send(ctx, command, false)
}

func (b *MpvBackend) tryRecover(ctx context.Context) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.recovered < b.recoverMax && !b.closed
}

func (b *MpvBackend) setSticky(err error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.setStickyLocked(err)
}

func (b *MpvBackend) setStickyLocked(err error) error {
	wrapped := fmt.Errorf("%w: %s", ErrBackendUnavailable, err.Error())
	b.stickyErr = wrapped
	b.ready = false
	select {
	case b.events <- Event{Type: EventError, Message: wrapped.Error()}:
	default:
	}
	return wrapped
}

// readLoop drains newline-delimited JSON frames from mpv, dispatching
// replies to per-request channels and forwarding asynchronous events
// to the Events channel. It exits when the connection closes.
func (b *MpvBackend) readLoop() {
	defer b.readerWG.Done()
	for {
		b.mu.Lock()
		scanner := b.scanner
		b.mu.Unlock()
		if scanner == nil {
			return
		}
		if !scanner.Scan() {
			// Peer closed: cancel any pending requests.
			b.mu.Lock()
			for id, ch := range b.pending {
				select {
				case ch <- nil:
				default:
				}
				delete(b.pending, id)
			}
			b.mu.Unlock()
			return
		}
		var reply ipcReply
		if err := json.Unmarshal(scanner.Bytes(), &reply); err != nil {
			continue // malformed line — skip
		}
		if reply.Event != "" {
			b.forwardEvent(reply)
			continue
		}
		b.mu.Lock()
		ch, ok := b.pending[reply.RequestID]
		b.mu.Unlock()
		if ok {
			ch <- &reply
		}
	}
}

func (b *MpvBackend) forwardEvent(reply ipcReply) {
	ev := Event{Type: EventType(reply.Event), Message: reply.Event}
	if len(reply.Data) > 0 {
		ev.Message = ev.Message + " " + string(reply.Data)
	}
	select {
	case b.events <- ev:
	default:
	}
}

// teardown closes the connection, kills the subprocess, and waits
// for the reader goroutine to exit. Safe to call multiple times.
func (b *MpvBackend) teardown() {
	b.mu.Lock()
	conn := b.conn
	cmd := b.cmd
	b.conn = nil
	b.cmd = nil
	b.scanner = nil
	b.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if cmd != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}
	b.readerWG.Wait()
}
