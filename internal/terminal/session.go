// Package terminal provides PTY-based terminal sessions for web terminal feature.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aymanbagabas/go-pty"
	"go.uber.org/zap"
)

// Common errors
var (
	ErrSessionClosed     = errors.New("terminal session is closed")
	ErrSessionNotFound   = errors.New("terminal session not found")
	ErrInvalidSize       = errors.New("invalid terminal size")
	ErrMaxSessionsReached = errors.New("maximum number of sessions reached")
)

// SessionState represents the state of a terminal session.
type SessionState int32

const (
	StateRunning SessionState = iota
	StateExiting
	StateExited
)

// InteractionState controls serialization strategy (IDE three-state).
// None: terminal has never been interacted with
// ReplayOnly: only replayed data, no direct user interaction
// Session: user has directly interacted (input/setTitle/setIcon)
type InteractionState int32

const (
	InteractionNone      InteractionState = 0
	InteractionReplayOnly InteractionState = 1
	InteractionSession    InteractionState = 2
)

// Grace time defaults (IDE PersistentTerminalProcess alignment)
const (
	graceTime          = 5 * time.Minute          // Long grace: 5min on detach
	shortGraceTime     = 30 * time.Second         // Short grace: 30s after orphan confirmation
	orphanBarrier      = 4 * time.Second          // Barrier timeout waiting for orphan_ack
	conptyResizeDelay  = 200 * time.Millisecond   // P5-3: ConPty resize delay after spawn (Windows only)
)

// OutputEntry represents a single output entry with sequence number.
type OutputEntry struct {
	Seq  uint64
	Data []byte
}

// resizeRequest holds a pending resize for delayed ConPty resize (P5-3).
type resizeRequest struct {
	cols uint16
	rows uint16
	time.Time // when the request was made
}

// Session represents a single terminal session.
type Session struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	CWD       string    `json:"cwd"`
	Shell     string    `json:"shell"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"` // "running" or "exited"

	pty   pty.Pty
	state int32 // atomic: SessionState
	mu    sync.RWMutex

	// Output broadcasting: subscribers consume PTY output via channels
	outputMu   sync.RWMutex
	outputSubs map[string]chan OutputEntry // subscriberID -> channel
	outputSeq  uint64                      // monotonic sequence number for resume
	ringBuf    *RingBuffer                 // circular buffer for replay on reconnect

	// Exit listeners (append-only, never overwrite)
	exitMu       sync.RWMutex
	exitListeners []func(code int)

	// Grace time (IDE PersistentTerminalProcess dual-layer timeout)
	// Layer 1 (graceTime): on detach, start 5min timer → if no reconnect, proceed to orphan check
	// Layer 2 (shortGraceTime): after orphan confirmation, start 30s timer → if no reconnect, kill session
	disconnectAt atomic.Int64 // ms timestamp of disconnect; 0 means connected
	graceTimer1  *time.Timer  // long grace (5min) — started on detach
	graceTimer2  *time.Timer  // short grace (30s) — started after orphan confirmation

	// Orphan confirmation state (IDE AutoOpenBarrier pattern)
	orphanMu       sync.Mutex
	orphanBarrier  *time.Timer // 4s barrier waiting for orphan_ack
	orphanAcked    atomic.Bool // true if frontend confirmed it's alive
	orphanConfirmed atomic.Bool // true if orphan (frontend dead) confirmed after barrier timeout
	onOrphanReq    func(sessionID string) // callback to send orphan_req to frontend
	onDetach       func(sessionID string) // callback to send detach to frontend before killing

	// InteractionState (IDE three-state: None → ReplayOnly → Session)
	interactionState atomic.Int32 // 0=None, 1=ReplayOnly, 2=Session

	// Serializer for VT state machine synchronization (IDE XtermSerializer equivalent).
	// Every PTY output byte is dual-written to both RingBuffer (for replay fallback)
	// and the headless terminal (for ANSI serialization on reconnect).
	serializer *NativeSerializer

	// Periodic snapshot: every 30s, serialize headless terminal and persist to disk.
	// Aligns with IDE: session output triggers serialization (debounced).
	snapshotTimer *time.Timer
	lastSnapshot  atomic.Int64 // ms timestamp of last persisted snapshot
	onSnapshot    func(sessionID string) // callback to trigger snapshot persistence

	// Replay state: during replay, input/signal/resize are discarded.
	// Aligns with IDE PersistentTerminalProcess._inReplay.
	inReplay atomic.Bool

	// PersistManager for disk persistence (P3 Revive).
	persistMgr *PersistManager

	// ChildProcessMonitor (P4: IDE ChildProcessMonitor alignment)
	processMonitor *ChildProcessMonitor
	hasChildProcs  atomic.Bool // true if session has running child processes
	onHasChildProcs func(sessionID string, has bool) // callback to notify frontend

	// Delayed resize for ConPty on Windows (P5-3).
	// ConPty fails if resize comes too early after spawn — delay 200ms.
	// On Unix this is unused (immediate resize works).
	delayedResizeMu    sync.Mutex
	delayedResizeTimer *time.Timer
	pendingResize      *resizeRequest
	spawnAt            time.Time // when the shell process was started (for ConPty delay calc)

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewSession creates a new terminal session.
func NewSession(id, cwd, shell string, cols, rows uint16) (*Session, error) {
	if shell == "" {
		shell = getDefaultShell()
	}

	// Validate working directory
	if cwd == "" {
		cwd = getDefaultCWD()
	}
	if _, err := os.Stat(cwd); err != nil {
		zap.L().Warn("Invalid working directory, using default", 
			zap.String("cwd", cwd), 
			zap.Error(err))
		cwd = getDefaultCWD()
	}

	// Create PTY with retry logic.
	// On macOS, pty.New() can fail with "device not configured" (ENXIO)
	// under transient conditions: PTY resource exhaustion, brief system
	// pressure, or sandbox restrictions. Retrying with a short delay
	// typically resolves these transient failures.
	var p pty.Pty
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		p, err = pty.New()
		if err == nil {
			break
		}
		zap.L().Warn("PTY creation failed, retrying",
			zap.Int("attempt", attempt+1),
			zap.Error(err))
		if attempt < 4 {
			time.Sleep(time.Duration(200*(attempt+1)) * time.Millisecond)
		}
	}
	if err != nil {
		// Provide actionable advice for common PTY exhaustion scenarios
		detail := err.Error()
		userMsg := fmt.Sprintf("failed to create PTY after retries: %s", detail)
		if strings.Contains(detail, "device not configured") || strings.Contains(detail, "ENXIO") {
			userMsg = "Terminal device unavailable — system PTY resources exhausted. Try closing other terminal applications (e.g. IDE terminals) and retry."
		}
		return nil, errors.New(userMsg)
	}

	// Set initial size with validation
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	// Limit terminal size to reasonable values
	if cols > 500 {
		cols = 500
	}
	if rows > 200 {
		rows = 200
	}

	if err := p.Resize(int(cols), int(rows)); err != nil {
		p.Close()
		return nil, fmt.Errorf("failed to resize PTY: %w", err)
	}

	// Start shell process.
	// On Unix, use login shell (-l flag) so the shell sources
	// ~/.zprofile / ~/.zshrc / ~/.bash_profile etc., giving the
	// terminal session the same PATH and environment the user sees
	// in a regular terminal emulator.
	shellArgs := []string{}
	if runtime.GOOS != "windows" {
		shellArgs = []string{"-l"}
	}
	c := p.Command(shell, shellArgs...)
	c.Dir = cwd

	// Build environment: inherit process env + ensure key variables
	c.Env = buildShellEnv(shell)

	// Set process group ID on Unix systems
	setProcessGroupAttr(c)

	if err := c.Start(); err != nil {
		p.Close()
		return nil, fmt.Errorf("failed to start shell: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create serializer for VT state machine synchronization.
	// Aligns with IDE: this._serializer = new XtermSerializer(...)
	serializer := NewNativeSerializer()
	scrollbackLines := 1000 // default scrollback for headless terminal
	if err := serializer.Create(id, int(cols), int(rows), scrollbackLines); err != nil {
		cancel() // Clean up context before returning
		p.Close()
		killProcessGroup(c.Process.Pid)
		return nil, fmt.Errorf("failed to create serializer: %w", err)
	}

	session := &Session{
		ID:          id,
		PID:         c.Process.Pid,
		CWD:         cwd,
		Shell:       shell,
		CreatedAt:   time.Now(),
		Status:      "running",
		pty:         p,
		state:       int32(StateRunning),
		outputSubs:  make(map[string]chan OutputEntry),
		ringBuf:     NewRingBuffer(2048), // ~2048 output entries for replay
		serializer:  serializer,
		spawnAt:     time.Now(),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize child process monitor (P4)
	monitor := NewChildProcessMonitor(c.Process.Pid)
	monitor.SetOnChange(func(has bool) {
		session.hasChildProcs.Store(has)
		if session.onHasChildProcs != nil {
			session.onHasChildProcs(session.ID, has)
		}
	})
	session.processMonitor = monitor

	// Monitor process exit in background
	go session.monitorExit(c.Process)

	// Start output loop: reads from PTY and broadcasts to subscribers
	go session.outputLoop()

	// Start periodic snapshot goroutine (P1-6, every 30s)
	// Aligns with IDE: serialize terminal state periodically for revive.
	go session.snapshotLoop()

	zap.L().Info("Terminal session created",
		zap.String("id", id),
		zap.Int("pid", c.Process.Pid),
		zap.String("shell", shell),
		zap.Uint16("cols", cols),
		zap.Uint16("rows", rows))

	return session, nil
}

// isRunning checks if the session is still running.
func (s *Session) isRunning() bool {
	return atomic.LoadInt32(&s.state) == int32(StateRunning)
}

// Write sends input to the terminal.
func (s *Session) Write(data []byte) error {
	if !s.isRunning() {
		return fmt.Errorf("session not running")
	}

	// Discard input during replay (IDE: if (this._inReplay) { return; })
	if s.inReplay.Load() {
		return nil
	}

	s.mu.RLock()
	pty := s.pty
	s.mu.RUnlock()

	if pty == nil {
		return fmt.Errorf("pty closed")
	}

	_, err := pty.Write(data)
	if err != nil {
		zap.L().Debug("Terminal write error", zap.String("id", s.ID), zap.Error(err))
		return err
	}

	// Transition to Session interaction state on user input
	s.TransitionToSession()

	// Trigger child process monitor on input (P4: IDE debounce 1s)
	if s.processMonitor != nil {
		s.processMonitor.OnInput()
	}

	return nil
}

// Read reads output from the terminal (blocking).
func (s *Session) Read(buf []byte) (int, error) {
	if !s.isRunning() {
		return 0, io.EOF
	}

	s.mu.RLock()
	pty := s.pty
	s.mu.RUnlock()

	if pty == nil {
		return 0, io.EOF
	}

	n, err := pty.Read(buf)
	if err != nil {
		zap.L().Debug("Terminal read ended", zap.String("id", s.ID), zap.Error(err))
	}
	return n, err
}

// Resize resizes the terminal window.
func (s *Session) Resize(cols, rows uint16) error {
	if !s.isRunning() {
		return fmt.Errorf("session not running")
	}

	// Discard resize during replay (IDE: if (this._inReplay) { return; })
	if s.inReplay.Load() {
		return nil
	}

	// Validate size
	if cols == 0 || rows == 0 {
		return fmt.Errorf("invalid terminal size: %dx%d", cols, rows)
	}
	if cols > 500 || rows > 200 {
		return fmt.Errorf("terminal size too large: %dx%d", cols, rows)
	}

	// P5-3: ConPty early resize delay on Windows.
	// ConPty fails if resize comes too early after spawn — delay until 200ms
	// after spawn. Aligns with VS Code DelayedResizer.
	// On Unix, this is a no-op (immediate resize works).
	if runtime.GOOS == "windows" {
		elapsed := time.Since(s.spawnAt)
		if elapsed < conptyResizeDelay {
			// Buffer the resize request and schedule delayed execution
			s.delayedResizeMu.Lock()
			s.pendingResize = &resizeRequest{cols: cols, rows: rows, Time: time.Now()}
			if s.delayedResizeTimer != nil {
				s.delayedResizeTimer.Stop()
			}
			delay := conptyResizeDelay - elapsed
			s.delayedResizeTimer = time.AfterFunc(delay, func() {
				s.executeDelayedResize()
			})
			s.delayedResizeMu.Unlock()

			// Synchronize headless terminal dimensions immediately (no ConPty risk)
			if s.serializer != nil {
				s.serializer.Resize(s.ID, int(cols), int(rows))
			}
			return nil
		}
	}

	s.mu.RLock()
	pty := s.pty
	s.mu.RUnlock()

	if pty == nil {
		return fmt.Errorf("pty closed")
	}

	err := pty.Resize(int(cols), int(rows))
	if err != nil {
		return err
	}

	// Synchronize headless terminal dimensions (VS Code: handleResize)
	if s.serializer != nil {
		if resizeErr := s.serializer.Resize(s.ID, int(cols), int(rows)); resizeErr != nil {
			zap.L().Debug("Serializer resize error",
				zap.String("id", s.ID), zap.Error(resizeErr))
			// Non-critical: serializer resize failure should not block PTY resize
		}
	}

	return nil
}

// executeDelayedResize applies a pending ConPty resize that was buffered
// during the early spawn window (P5-3). Called by delayedResizeTimer
// after conptyResizeDelay has elapsed since spawn.
// Aligns with VS Code DelayedResizer: buffer early resizes, apply after delay.
func (s *Session) executeDelayedResize() {
	s.delayedResizeMu.Lock()
	req := s.pendingResize
	s.pendingResize = nil
	s.delayedResizeTimer = nil
	s.delayedResizeMu.Unlock()

	if req == nil || !s.isRunning() {
		return
	}

	s.mu.RLock()
	pty := s.pty
	s.mu.RUnlock()

	if pty == nil {
		return
	}

	if err := pty.Resize(int(req.cols), int(req.rows)); err != nil {
		zap.L().Debug("Delayed ConPty resize failed",
			zap.String("id", s.ID),
			zap.Uint16("cols", req.cols),
			zap.Uint16("rows", req.rows),
			zap.Error(err))
		return
	}

	zap.L().Debug("Applied delayed ConPty resize",
		zap.String("id", s.ID),
		zap.Uint16("cols", req.cols),
		zap.Uint16("rows", req.rows))
}

// Close closes the terminal session.
func (s *Session) Close() error {
	// Use atomic compare-and-swap to ensure we only close once
	if !atomic.CompareAndSwapInt32(&s.state, int32(StateRunning), int32(StateExiting)) {
		// Already exiting or exited
		return nil
	}

	zap.L().Info("Closing terminal session", zap.String("id", s.ID), zap.Int("pid", s.PID))

	// Cancel context
	if s.cancel != nil {
		s.cancel()
	}

	// Stop all grace timers
	s.mu.Lock()
	if s.graceTimer1 != nil {
		s.graceTimer1.Stop()
		s.graceTimer1 = nil
	}
	if s.graceTimer2 != nil {
		s.graceTimer2.Stop()
		s.graceTimer2 = nil
	}
	s.mu.Unlock()

	// Stop orphan barrier
	s.orphanMu.Lock()
	if s.orphanBarrier != nil {
		s.orphanBarrier.Stop()
		s.orphanBarrier = nil
	}
	s.orphanMu.Unlock()

	// Stop snapshot timer
	s.mu.Lock()
	if s.snapshotTimer != nil {
		s.snapshotTimer.Stop()
		s.snapshotTimer = nil
	}
	s.mu.Unlock()

	// Stop delayed resize timer (P5-3 ConPty)
	s.delayedResizeMu.Lock()
	if s.delayedResizeTimer != nil {
		s.delayedResizeTimer.Stop()
		s.delayedResizeTimer = nil
	}
	s.pendingResize = nil
	s.delayedResizeMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Kill the process group
	if s.PID > 0 {
		killProcessGroup(s.PID)
	}

	// Close PTY
	if s.pty != nil {
		if err := s.pty.Close(); err != nil {
			zap.L().Debug("PTY close error", zap.String("id", s.ID), zap.Error(err))
		}
		s.pty = nil
	}

	// Update state
	atomic.StoreInt32(&s.state, int32(StateExited))
	s.Status = "exited"

	// Close all output subscribers
	s.outputMu.Lock()
	for id, ch := range s.outputSubs {
		close(ch)
		delete(s.outputSubs, id)
	}
	s.outputMu.Unlock()

	// Destroy serializer (IDE: this._serializer.dispose())
	if s.serializer != nil {
		if err := s.serializer.Destroy(s.ID); err != nil {
			zap.L().Debug("Serializer destroy error",
				zap.String("id", s.ID), zap.Error(err))
		}
	}

	// Stop child process monitor
	if s.processMonitor != nil {
		s.processMonitor.Stop()
	}

	return nil
}

// SetOnOutput sets the output callback (kept for backward compatibility).
func (s *Session) SetOnOutput(fn func(data []byte)) {
	// No-op: output is now broadcast via Subscribe/Unsubscribe
}

// SetOnExit appends an exit listener. Multiple listeners can be registered.
// This replaces the old SetOnExit which would overwrite previous listeners.
func (s *Session) SetOnExit(fn func(code int)) {
	s.exitMu.Lock()
	defer s.exitMu.Unlock()
	s.exitListeners = append(s.exitListeners, fn)
}

// AddOnExit appends an exit listener (alias for SetOnExit for clarity).
func (s *Session) AddOnExit(fn func(code int)) {
	s.SetOnExit(fn)
}

// Subscribe registers a subscriber to receive output entries.
// Returns a channel that will receive output entries and the current sequence number.
// The subscriber must call Unsubscribe when done to prevent leaks.
// On first subscribe after disconnect, triggers MarkReconnected (cancels grace timers).
func (s *Session) Subscribe(subscriberID string) (<-chan OutputEntry, uint64) {
	ch := make(chan OutputEntry, 4096)
	s.outputMu.Lock()
	s.outputSubs[subscriberID] = ch
	currentSeq := s.outputSeq
	subCount := len(s.outputSubs)
	s.outputMu.Unlock()

	// If this is a reconnect (session was disconnected), cancel grace timers
	if s.IsDisconnected() && subCount > 0 {
		s.MarkReconnected()
	}

	return ch, currentSeq
}

// Unsubscribe removes a subscriber.
// When all subscribers are gone, triggers MarkDisconnected (starts grace timers).
func (s *Session) Unsubscribe(subscriberID string) {
	s.outputMu.Lock()
	ch, ok := s.outputSubs[subscriberID]
	if ok {
		delete(s.outputSubs, subscriberID)
	}
	remaining := len(s.outputSubs)
	s.outputMu.Unlock()

	if ok {
		close(ch)
	}

	// When all subscribers are gone and session is still running, start grace timers
	if remaining == 0 && s.isRunning() && !s.IsDisconnected() {
		s.MarkDisconnected()
	}
}

// broadcastOutput sends output data to all subscribers and writes to ring buffer.
//
// Lock discipline: only lightweight non-blocking sends are performed under
// outputMu. If a subscriber's channel is full, we record it and retry outside
// the lock with a short timeout. This prevents a slow subscriber from
// blocking the entire broadcast path (ringBuf writes, other subscribers, etc.).
func (s *Session) broadcastOutput(data []byte) {
	s.outputMu.Lock()
	seq := s.outputSeq + 1
	s.outputSeq = seq

	entry := OutputEntry{Seq: seq, Data: data}
	s.ringBuf.Write(entry)

	// Non-blocking send to each subscriber under the lock.
	// Record any subscriber whose channel was full for retry outside the lock.
	var blocked []chan OutputEntry
	for _, ch := range s.outputSubs {
		select {
		case ch <- entry:
		default:
			blocked = append(blocked, ch)
		}
	}
	s.outputMu.Unlock()

	// Outside the lock: retry blocked subscribers with a short timeout.
	// This applies backpressure (outputLoop stalls → PTY buffer fills →
	// cat/ls throttles) without holding the lock.
	if len(blocked) > 0 {
		const writeTimeout = 5 * time.Second
		for _, ch := range blocked {
			select {
			case ch <- entry:
			case <-time.After(writeTimeout):
				zap.L().Warn("Output subscriber channel full after timeout, dropping message",
					zap.String("sessionID", s.ID))
			}
		}
	}
}

// ReplaySince returns buffered output entries with seq > sinceSeq for reconnection replay.
func (s *Session) ReplaySince(sinceSeq uint64) []OutputEntry {
	s.outputMu.RLock()
	defer s.outputMu.RUnlock()
	return s.ringBuf.ReadSince(sinceSeq)
}

// LatestSeq returns the current output sequence number.
func (s *Session) LatestSeq() uint64 {
	s.outputMu.RLock()
	defer s.outputMu.RUnlock()
	return s.outputSeq
}

// HasActiveSubscriber returns true if there is at least one output subscriber.
func (s *Session) HasActiveSubscriber() bool {
	s.outputMu.RLock()
	defer s.outputMu.RUnlock()
	return len(s.outputSubs) > 0
}

// MarkDisconnected marks the session as disconnected and starts the long grace timer.
// This is called when the last WebSocket subscriber disconnects.
// Aligns with IDE PersistentTerminalProcess._disconnectRunner1 (graceTime).
func (s *Session) MarkDisconnected() {
	// Only start grace timer if not already disconnected
	if s.disconnectAt.Load() != 0 {
		return
	}

	s.disconnectAt.Store(time.Now().UnixMilli())
	zap.L().Info("Terminal session disconnected, starting long grace timer",
		zap.String("id", s.ID),
		zap.Duration("graceTime", graceTime))

	// Start long grace timer (5min). On expiry, begin orphan confirmation.
	s.mu.Lock()
	if s.graceTimer1 != nil {
		s.graceTimer1.Stop()
	}
	s.graceTimer1 = time.AfterFunc(graceTime, func() {
		s.beginOrphanCheck()
	})
	s.mu.Unlock()
}

// MarkReconnected marks the session as reconnected and cancels both grace timers.
// This is called when a new WebSocket subscriber connects.
func (s *Session) MarkReconnected() {
	s.disconnectAt.Store(0)
	s.orphanAcked.Store(false)
	s.orphanConfirmed.Store(false)

	s.mu.Lock()
	if s.graceTimer1 != nil {
		s.graceTimer1.Stop()
		s.graceTimer1 = nil
	}
	if s.graceTimer2 != nil {
		s.graceTimer2.Stop()
		s.graceTimer2 = nil
	}
	s.mu.Unlock()

	s.orphanMu.Lock()
	if s.orphanBarrier != nil {
		s.orphanBarrier.Stop()
		s.orphanBarrier = nil
	}
	s.orphanMu.Unlock()

	// Transition to Session interaction state on reconnect
	s.interactionState.Store(int32(InteractionSession))

	zap.L().Info("Terminal session reconnected, grace timers cancelled",
		zap.String("id", s.ID))
}

// beginOrphanCheck starts the orphan confirmation protocol.
// After long grace expires, send orphan_req and wait 4s for orphan_ack.
// Aligns with IDE AutoOpenBarrier(4000) pattern.
func (s *Session) beginOrphanCheck() {
	zap.L().Info("Long grace expired, beginning orphan check",
		zap.String("id", s.ID),
		zap.Duration("barrier", orphanBarrier))

	s.orphanMu.Lock()
	if s.orphanBarrier != nil {
		s.orphanBarrier.Stop()
	}
	s.orphanAcked.Store(false)

	// Start barrier: wait 4s for orphan_ack
	s.orphanBarrier = time.AfterFunc(orphanBarrier, func() {
		s.onOrphanBarrierTimeout()
	})
	callback := s.onOrphanReq
	s.orphanMu.Unlock()

	// Trigger orphan_req callback to push orphan_req to all subscribers
	if callback != nil {
		callback(s.ID)
	}
}

// OnOrphanAck handles receiving orphan_ack from the frontend.
// Called when the frontend confirms it's alive.
func (s *Session) OnOrphanAck() {
	if s.orphanConfirmed.Load() {
		return // Already confirmed as orphan, too late
	}

	s.orphanAcked.Store(true)

	s.orphanMu.Lock()
	if s.orphanBarrier != nil {
		s.orphanBarrier.Stop()
		s.orphanBarrier = nil
	}
	s.orphanMu.Unlock()

	// Frontend is alive — reduce grace time to short (30s)
	s.reduceGraceTime()

	zap.L().Info("Orphan ack received, reduced to short grace",
		zap.String("id", s.ID))
}

// onOrphanBarrierTimeout is called when 4s barrier expires without orphan_ack.
// The frontend is confirmed orphan (dead) — send detach, then kill the session.
func (s *Session) onOrphanBarrierTimeout() {
	s.orphanConfirmed.Store(true)

	zap.L().Info("Orphan barrier timeout, frontend confirmed orphan — killing session",
		zap.String("id", s.ID))

	// Send detach notification before killing
	s.orphanMu.Lock()
	detachCb := s.onDetach
	s.orphanMu.Unlock()

	if detachCb != nil {
		detachCb(s.ID)
	}

	// Kill the session — no frontend is alive to reconnect
	s.Close()
}

// reduceGraceTime stops the long grace timer and starts the short grace timer.
// Aligns with IDE PersistentTerminalProcess.reduceGraceTime().
func (s *Session) reduceGraceTime() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.graceTimer2 != nil {
		return // Short grace already running
	}

	if s.graceTimer1 != nil {
		s.graceTimer1.Stop()
		s.graceTimer1 = nil
	}

	zap.L().Info("Reducing grace time to short grace",
		zap.String("id", s.ID),
		zap.Duration("shortGraceTime", shortGraceTime))

	s.graceTimer2 = time.AfterFunc(shortGraceTime, func() {
		zap.L().Info("Short grace expired — killing session",
			zap.String("id", s.ID))

		// Send detach before killing so frontend stops reconnecting
		s.orphanMu.Lock()
		detachCb := s.onDetach
		s.orphanMu.Unlock()

		if detachCb != nil {
			detachCb(s.ID)
		}

		s.Close()
	})
}

// IsDisconnected returns true if the session is currently in disconnected state.
func (s *Session) IsDisconnected() bool {
	return s.disconnectAt.Load() != 0
}

// DisconnectDuration returns how long the session has been disconnected.
// Returns 0 if connected.
func (s *Session) DisconnectDuration() time.Duration {
	ms := s.disconnectAt.Load()
	if ms == 0 {
		return 0
	}
	return time.Duration(time.Now().UnixMilli()-ms) * time.Millisecond
}

// NeedsOrphanCheck returns true if the session is in disconnected state
// and needs orphan_req to be sent to the frontend.
func (s *Session) NeedsOrphanCheck() bool {
	if s.disconnectAt.Load() == 0 {
		return false // Still connected
	}
	// Needs check if barrier timer exists (means long grace expired
	// and we're waiting for orphan_ack)
	s.orphanMu.Lock()
	needs := s.orphanBarrier != nil && !s.orphanAcked.Load()
	s.orphanMu.Unlock()
	return needs
}

// GetInteractionState returns the current interaction state.
func (s *Session) GetInteractionState() InteractionState {
	return InteractionState(s.interactionState.Load())
}

// TransitionToSession transitions the interaction state to Session
// when the user directly interacts (input/setTitle/setIcon).
func (s *Session) TransitionToSession() {
	s.interactionState.Store(int32(InteractionSession))
}

// Serializer returns the session's NativeSerializer for VT state machine access.
// Used by the API layer to serialize terminal state on reconnect.
func (s *Session) Serializer() *NativeSerializer {
	return s.serializer
}

// SetOnOrphanReq sets the callback invoked when orphan_req should be sent.
// The handler layer uses this to push orphan_req via WebSocket.
func (s *Session) SetOnOrphanReq(fn func(sessionID string)) {
	s.orphanMu.Lock()
	defer s.orphanMu.Unlock()
	s.onOrphanReq = fn
}

// SetOnDetach sets the callback invoked before session is killed by grace timer.
// The handler layer uses this to send detach message via WebSocket.
func (s *Session) SetOnDetach(fn func(sessionID string)) {
	s.orphanMu.Lock()
	defer s.orphanMu.Unlock()
	s.onDetach = fn
}

// HasChildProcesses returns whether the session currently has running child processes.
// Aligns with IDE ProcessPropertyType.HasChildProcesses.
func (s *Session) HasChildProcesses() bool {
	return s.hasChildProcs.Load()
}

// SetOnHasChildProcs sets the callback invoked when hasChildProcesses changes.
// The handler layer uses this to push has_child_processes to frontend via WebSocket.
func (s *Session) SetOnHasChildProcs(fn func(sessionID string, has bool)) {
	s.onHasChildProcs = fn
}

// monitorExit monitors the process exit.
func (s *Session) monitorExit(process *os.Process) {
	state, err := process.Wait()

	// Check if we're already closing
	if !atomic.CompareAndSwapInt32(&s.state, int32(StateRunning), int32(StateExited)) {
		return
	}

	s.mu.Lock()
	s.Status = "exited"
	s.mu.Unlock()

	exitCode := 0
	if err == nil && state != nil {
		exitCode = state.ExitCode()
	}

	zap.L().Info("Terminal process exited",
		zap.String("id", s.ID),
		zap.Int("pid", s.PID),
		zap.Int("exitCode", exitCode))

	// Notify all exit listeners
	s.exitMu.RLock()
	listeners := make([]func(code int), len(s.exitListeners))
	copy(listeners, s.exitListeners)
	s.exitMu.RUnlock()

	for _, fn := range listeners {
		fn(exitCode)
	}

	// Close PTY to release system resources (PTY fd leak is the
	// primary cause of "device not configured" on macOS).
	s.mu.Lock()
	if s.pty != nil {
		if closeErr := s.pty.Close(); closeErr != nil {
			zap.L().Debug("PTY close error in monitorExit",
				zap.String("id", s.ID), zap.Error(closeErr))
		}
		s.pty = nil
	}
	s.mu.Unlock()

	// Close all output subscribers
	s.outputMu.Lock()
	for id, ch := range s.outputSubs {
		close(ch)
		delete(s.outputSubs, id)
	}
	s.outputMu.Unlock()
}

// outputLoop reads from the PTY and broadcasts output to all subscribers.
// This runs in its own goroutine and decouples PTY output from WebSocket lifecycle.
//
// Coalescing strategy (adaptive, zero-cost for interactive input):
//   1. A reader goroutine reads from the PTY (blocking) and sends chunks to readCh.
//   2. The main loop reads the first chunk from readCh.
//   3. Non-blocking drain: if more chunks are immediately available, this is bulk
//      output (cat/ls) — keep draining, then wait 2ms for the kernel to batch
//      more PTY data, drain once more, and broadcast the accumulated payload.
//   4. If no more chunks are immediately available, this is interactive input —
//      broadcast the single chunk immediately with zero added latency.
func (s *Session) outputLoop() {
	const (
		readBufSize   = 4096                // per-read buffer size
		readChCap     = 64                   // read channel capacity (chunks)
		coalesceDelay = 2 * time.Millisecond // wait for more data before broadcast
		maxAccumSize  = 1048576              // 1 MB safety limit per broadcast
	)

	readBuf := make([]byte, readBufSize)
	readCh := make(chan []byte, readChCap)

	// Reader goroutine: reads from PTY (blocking) and sends chunks to readCh.
	// Exits when the PTY is closed (Read returns error) or session is closed.
	go func() {
		defer close(readCh)
		for {
			if !s.isRunning() {
				return
			}

			s.mu.RLock()
			p := s.pty
			s.mu.RUnlock()

			if p == nil {
				return
			}

			n, err := p.Read(readBuf)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					zap.L().Debug("PTY read ended", zap.String("id", s.ID), zap.Error(err))
				}
				return
			}

			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, readBuf[:n])
				readCh <- chunk
			}
		}
	}()

	// Main loop: read chunks from readCh, coalesce adaptively, and broadcast.
	for chunk := range readCh {
		accum := chunk

		// Non-blocking drain: grab any immediately-available chunks.
		draining := true
		for draining && len(accum) < maxAccumSize {
			select {
			case more, ok := <-readCh:
				if !ok {
					draining = false
					break
				}
				accum = append(accum, more...)
			default:
				draining = false
			}
		}

		// If we drained extra chunks, this is bulk output — wait briefly
		// for more data, then drain once more before broadcasting.
		// If no extra chunks were available, this is interactive input —
		// skip the wait and broadcast immediately (zero added latency).
		if draining && len(accum) < maxAccumSize {
			select {
			case more, ok := <-readCh:
				if ok {
					accum = append(accum, more...)
					// Final drain after the wait.
					draining = true
					for draining && len(accum) < maxAccumSize {
						select {
						case more2, ok2 := <-readCh:
							if !ok2 {
								draining = false
								break
							}
							accum = append(accum, more2...)
						default:
							draining = false
						}
					}
				}
			case <-time.After(coalesceDelay):
			}
		}

		// Broadcast accumulated output as a single entry.
		s.broadcastOutput(accum)

		// Dual-write to headless terminal (IDE: this._serializer.handleData(e))
		// Every PTY output byte goes to both RingBuffer and VT state machine.
		if s.serializer != nil {
			if err := s.serializer.Write(s.ID, accum); err != nil {
				zap.L().Debug("Serializer write error",
					zap.String("id", s.ID), zap.Error(err))
			}
		}

		// Trigger child process monitor on output (P4: IDE throttle 5s)
		if s.processMonitor != nil {
			s.processMonitor.OnOutput()
		}
	}
}

// snapshotLoop periodically serializes the headless terminal and persists snapshots.
// Runs every 30s, aligning with IDE's periodic serialization for Revive.
func (s *Session) snapshotLoop() {
	const snapshotInterval = 30 * time.Second

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(snapshotInterval):
			if !s.isRunning() {
				return
			}
			s.takeSnapshot()
		}
	}
}

// takeSnapshot serializes the headless terminal and writes to disk via PersistManager.
func (s *Session) takeSnapshot() {
	if s.serializer == nil || s.persistMgr == nil {
		return
	}

	resultCh := s.serializer.Serialize(s.ID, true)
	select {
	case result := <-resultCh:
		if result.Err != nil {
			zap.L().Debug("Snapshot serialization failed",
				zap.String("id", s.ID), zap.Error(result.Err))
			return
		}

		snap := &SessionSnapshot{
			ID: s.ID,
			ShellLaunchConfig: ShellLaunchConfig{
				Executable: s.Shell,
				Cwd:        s.CWD,
			},
			ReplayEvent: ReplayEvent{
				Events: []ReplayEventEntry{
					{Data: result.Data},
				},
				Commands: result.Commands,
			},
			Timestamp: time.Now().UnixMilli(),
			Source:    "serialize",
		}

		if err := s.persistMgr.WriteSnapshot(snap); err != nil {
			zap.L().Debug("Snapshot persistence failed",
				zap.String("id", s.ID), zap.Error(err))
			return
		}

		s.lastSnapshot.Store(time.Now().UnixMilli())
	case <-time.After(5 * time.Second):
		zap.L().Debug("Snapshot serialization timeout",
			zap.String("id", s.ID))
	}
}

// SetPersistManager sets the persist manager for disk persistence.
func (s *Session) SetPersistManager(pm *PersistManager) {
	s.persistMgr = pm
}

// SetOnSnapshot sets the callback for snapshot events (unused — persistMgr handles it).
func (s *Session) SetOnSnapshot(fn func(sessionID string)) {
	s.onSnapshot = fn
}

// IsInReplay returns true if the session is currently replaying serialized data.
// During replay, input/signal/resize are discarded (IDE: _inReplay).
func (s *Session) IsInReplay() bool {
	return s.inReplay.Load()
}

// SetInReplay sets the replay state. Called by the API layer during resume.
func (s *Session) SetInReplay(in bool) {
	s.inReplay.Store(in)
}

// PersistTerminalState synchronously persists the terminal state for graceful shutdown.
// Called during backend shutdown to ensure all sessions are persisted before exit.
func (s *Session) PersistTerminalState() error {
	if s.serializer == nil || s.persistMgr == nil {
		return nil
	}

	resultCh := s.serializer.Serialize(s.ID, true)
	select {
	case result := <-resultCh:
		if result.Err != nil {
			return result.Err
		}
		snap := &SessionSnapshot{
			ID: s.ID,
			ShellLaunchConfig: ShellLaunchConfig{
				Executable: s.Shell,
				Cwd:        s.CWD,
			},
			ReplayEvent: ReplayEvent{
				Events: []ReplayEventEntry{
					{Data: result.Data},
				},
				Commands: result.Commands,
			},
			Timestamp: time.Now().UnixMilli(),
			Source:    "serialize",
		}
		return s.persistMgr.WriteSnapshot(snap)
	case <-time.After(10 * time.Second):
		return fmt.Errorf("persist state timeout for session %s", s.ID)
	}
}

// buildShellEnv constructs the environment variables for the terminal session.
// It inherits the current process environment and ensures key terminal
// variables (TERM, COLORTERM, LANG, SHELL) are set correctly.
func buildShellEnv(shell string) []string {
	env := os.Environ()

	// Ensure key terminal variables
	env = ensureEnv(env, "TERM", "xterm-256color")
	env = ensureEnv(env, "COLORTERM", "truecolor")
	if runtime.GOOS != "windows" {
		env = ensureEnv(env, "LANG", "en_US.UTF-8")
		env = ensureEnv(env, "SHELL", shell)
	}

	return env
}

// ensureEnv ensures the environment list contains the given key=value pair.
// If the key already exists, its value is overwritten; otherwise it is appended.
func ensureEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// getDefaultShell returns the default shell for the current platform.
func getDefaultShell() string {
	if runtime.GOOS == "windows" {
		return "cmd.exe"
	}

	// 1. Prefer the $SHELL environment variable (user's login shell)
	if shell := os.Getenv("SHELL"); shell != "" {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}

	// 2. macOS fallback: query dscl for the user's default shell
	//    This handles the case where the desktop app was launched from
	//    Finder and $SHELL is not in the process environment.
	if runtime.GOOS == "darwin" {
		if shell, err := getUserShellFromDscl(); err == nil && shell != "" {
			if _, err := os.Stat(shell); err == nil {
				return shell
			}
		}
	}

	// 3. Final fallback: try common shells (zsh first on modern systems)
	shells := []string{"/bin/zsh", "/bin/bash", "/bin/sh", "/usr/bin/bash", "/usr/bin/sh"}
	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}

	return "/bin/sh"
}

// getUserShellFromDscl queries the macOS Directory Service for the user's
// default shell. This is needed when the desktop app is launched from Finder
// and the $SHELL environment variable is not set in the process environment.
func getUserShellFromDscl() (string, error) {
	user := os.Getenv("USER")
	if user == "" {
		return "", fmt.Errorf("USER env not set")
	}
	out, err := exec.Command("dscl", ".", "-read", "/Users/"+user, "UserShell").Output()
	if err != nil {
		return "", err
	}
	// Output format: "UserShell: /bin/zsh"
	parts := strings.Split(strings.TrimSpace(string(out)), ": ")
	if len(parts) == 2 && parts[1] != "" {
		return parts[1], nil
	}
	return "", fmt.Errorf("unexpected dscl output: %q", strings.TrimSpace(string(out)))
}

// getDefaultCWD returns the default working directory for the current platform.
func getDefaultCWD() string {
	if runtime.GOOS == "windows" {
		// Try USERPROFILE first, then HOMEDRIVE+HOMEPATH, then C:\
		if home := os.Getenv("USERPROFILE"); home != "" {
			if _, err := os.Stat(home); err == nil {
				return home
			}
		}
		drive := os.Getenv("HOMEDRIVE")
		path := os.Getenv("HOMEPATH")
		if drive != "" && path != "" {
			fullPath := drive + path
			if _, err := os.Stat(fullPath); err == nil {
				return fullPath
			}
		}
		return "C:\\"
	}
	return "/"
}