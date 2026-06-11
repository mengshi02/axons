// Package terminal provides PTY-based terminal sessions for web terminal feature.
// serializer.go implements the NativeSerializer using go-headless-term for
// VT state machine synchronization — aligning with IDE XtermSerializer.
package terminal

import (
	"fmt"
	"sync"

	headlessterm "github.com/danielgatis/go-headless-term"
	"go.uber.org/zap"
)

// Serializer interface (unified, no dual-track).
// The only implementation is NativeSerializer (Go in-process, desktop/Web shared).
// Aligns with IDE XtermSerializer concept.
type Serializer interface {
	// Create initializes a headless terminal for the given session.
	Create(id string, cols, rows, scrollback int) error
	// Write feeds PTY output data to the headless terminal's VT state machine.
	Write(id string, data []byte) error
	// Resize synchronizes the headless terminal dimensions with the PTY.
	Resize(id string, cols, rows int) error
	// Serialize produces an xterm-compatible ANSI sequence stream from the
	// headless terminal's current state. Returns the result via a channel
	// to avoid blocking the caller (VT state machine serialization is async).
	Serialize(id string, full bool) <-chan SerializeResult
	// Destroy removes the headless terminal instance.
	Destroy(id string) error
}

// SerializeResult holds the output of a serialization operation.
type SerializeResult struct {
	Data     string              // xterm-compatible ANSI sequence stream
	Commands []SerializedCommand // Shell integration commands (OSC 133)
	Err      error
}

// SerializedCommand represents a shell integration command from OSC 133.
// Aligns with IDE ISerializedCommand.
type SerializedCommand struct {
	CommandLine   string
	CommandStart  int64 // ms timestamp
	CommandEnd    int64 // ms timestamp
	ExitCode      int
}

// NativeSerializer is the sole Serializer implementation.
// It maintains go-headless-term Terminal instances in-process (zero IPC).
// Desktop and Web versions share the same implementation.
type NativeSerializer struct {
	terms map[string]*headlessterm.Terminal
	mu    sync.RWMutex
}

// NewNativeSerializer creates a new in-process serializer.
func NewNativeSerializer() *NativeSerializer {
	return &NativeSerializer{
		terms: make(map[string]*headlessterm.Terminal),
	}
}

// Create initializes a headless terminal instance for a session.
func (s *NativeSerializer) Create(id string, cols, rows, scrollback int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.terms[id]; exists {
		return fmt.Errorf("serializer: terminal %s already exists", id)
	}

	// Create headless terminal with dimensions and scrollback
	opts := []headlessterm.Option{
		headlessterm.WithSize(rows, cols),
	}

	// If scrollback > 0, configure scrollback storage
	if scrollback > 0 {
		opts = append(opts, headlessterm.WithScrollback(
			headlessterm.NewMemoryScrollback(scrollback),
		))
	}

	term := headlessterm.New(opts...)
	s.terms[id] = term

	zap.L().Info("NativeSerializer: created headless terminal",
		zap.String("id", id),
		zap.Int("cols", cols),
		zap.Int("rows", rows),
		zap.Int("scrollback", scrollback))

	return nil
}

// Write feeds PTY output data to the headless terminal's VT state machine.
// This is the "dual-write" — every PTY output byte goes to both RingBuffer
// (for replay fallback) and the VT state machine (for serialization).
// Aligns with IDE: this._serializer.handleData(e)
func (s *NativeSerializer) Write(id string, data []byte) error {
	s.mu.RLock()
	term, exists := s.terms[id]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("serializer: terminal %s not found", id)
	}

	// In-process call, zero IPC, zero allocation
	term.Write(data)
	return nil
}

// Resize synchronizes the headless terminal dimensions with the PTY.
// Aligns with IDE: handleResize(cols, rows): this._xterm.resize(cols, rows)
func (s *NativeSerializer) Resize(id string, cols, rows int) error {
	s.mu.RLock()
	term, exists := s.terms[id]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("serializer: terminal %s not found", id)
	}

	term.Resize(rows, cols)
	return nil
}

// Size returns the current dimensions (cols, rows) of the headless terminal.
func (s *NativeSerializer) Size(id string) (cols, rows int, ok bool) {
	s.mu.RLock()
	term, exists := s.terms[id]
	s.mu.RUnlock()

	if !exists {
		return 0, 0, false
	}
	cols = term.Cols()
	rows = term.Rows()
	return cols, rows, true
}

// Serialize produces an xterm-compatible ANSI sequence stream from the
// headless terminal's current state. Returns asynchronously via channel.
func (s *NativeSerializer) Serialize(id string, full bool) <-chan SerializeResult {
	ch := make(chan SerializeResult, 1)

	go func() {
		s.mu.RLock()
		term, exists := s.terms[id]
		s.mu.RUnlock()

		if !exists {
			ch <- SerializeResult{Err: fmt.Errorf("serializer: terminal %s not found", id)}
			return
		}

		// Take a full snapshot of the terminal state
		snap := term.Snapshot(headlessterm.SnapshotDetailFull)

		// Convert structured snapshot to xterm-compatible ANSI sequence stream
		data := SerializeXterm(term, snap)

		ch <- SerializeResult{Data: data}
	}()

	return ch
}

// Destroy removes the headless terminal instance.
func (s *NativeSerializer) Destroy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.terms[id]; !exists {
		return fmt.Errorf("serializer: terminal %s not found", id)
	}

	delete(s.terms, id)

	zap.L().Info("NativeSerializer: destroyed headless terminal",
		zap.String("id", id))

	return nil
}