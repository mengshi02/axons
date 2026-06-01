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

// OutputEntry represents a single output entry with sequence number.
type OutputEntry struct {
	Seq  uint64
	Data []byte
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
			userMsg = "Terminal device unavailable — system PTY resources exhausted. Try closing other terminal applications (e.g. VS Code terminals) and retry."
		}
		return nil, fmt.Errorf(userMsg)
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
		ctx:         ctx,
		cancel:      cancel,
	}

	// Monitor process exit in background
	go session.monitorExit(c.Process)

	// Start output loop: reads from PTY and broadcasts to subscribers
	go session.outputLoop()

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

	// Validate size
	if cols == 0 || rows == 0 {
		return fmt.Errorf("invalid terminal size: %dx%d", cols, rows)
	}
	if cols > 500 || rows > 200 {
		return fmt.Errorf("terminal size too large: %dx%d", cols, rows)
	}

	s.mu.RLock()
	pty := s.pty
	s.mu.RUnlock()

	if pty == nil {
		return fmt.Errorf("pty closed")
	}

	err := pty.Resize(int(cols), int(rows))
	return err
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
func (s *Session) Subscribe(subscriberID string) (<-chan OutputEntry, uint64) {
	ch := make(chan OutputEntry, 256)
	s.outputMu.Lock()
	s.outputSubs[subscriberID] = ch
	currentSeq := s.outputSeq
	s.outputMu.Unlock()
	return ch, currentSeq
}

// Unsubscribe removes a subscriber.
func (s *Session) Unsubscribe(subscriberID string) {
	s.outputMu.Lock()
	ch, ok := s.outputSubs[subscriberID]
	if ok {
		delete(s.outputSubs, subscriberID)
		close(ch)
	}
	s.outputMu.Unlock()
}

// broadcastOutput sends output data to all subscribers and writes to ring buffer.
func (s *Session) broadcastOutput(data []byte) {
	s.outputMu.Lock()
	seq := s.outputSeq + 1
	s.outputSeq = seq

	entry := OutputEntry{Seq: seq, Data: data}
	s.ringBuf.Write(entry)

	for id, ch := range s.outputSubs {
		select {
		case ch <- entry:
		default:
			// Subscriber channel full, drop message (subscriber is too slow)
			zap.L().Debug("Output subscriber channel full, dropping message",
				zap.String("sessionID", s.ID),
				zap.String("subscriberID", id))
		}
	}
	s.outputMu.Unlock()
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
func (s *Session) outputLoop() {
	buf := make([]byte, 4096)
	for {
		// Check if session is still running
		if !s.isRunning() {
			return
		}

		s.mu.RLock()
		pty := s.pty
		s.mu.RUnlock()

		if pty == nil {
			return
		}

		n, err := pty.Read(buf)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				zap.L().Debug("PTY read ended", zap.String("id", s.ID), zap.Error(err))
			}
			return
		}

		if n > 0 {
			// Broadcast output to all subscribers and write to ring buffer
			data := make([]byte, n)
			copy(data, buf[:n])
			s.broadcastOutput(data)
		}
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