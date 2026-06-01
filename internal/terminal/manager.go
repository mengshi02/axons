package terminal

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Manager manages terminal sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex

	// Configuration
	maxSessions int

	// Callbacks
	onSessionExit func(sessionID string, code int)

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// ManagerConfig holds manager configuration.
type ManagerConfig struct {
	MaxSessions int // Max sessions total (default: 10)
}

// NewManager creates a new terminal manager.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 10
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		sessions:    make(map[string]*Session),
		maxSessions: cfg.MaxSessions,
		ctx:         ctx,
		cancel:      cancel,
	}

	zap.L().Info("Terminal manager initialized",
		zap.Int("maxSessions", cfg.MaxSessions))

	return m
}

// CreateSession creates a new terminal session.
func (m *Manager) CreateSession(cwd, shell string, cols, rows uint16) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if manager is shutting down
	if m.ctx.Err() != nil {
		return nil, fmt.Errorf("terminal manager is shutting down")
	}

	// Check session limit
	if len(m.sessions) >= m.maxSessions {
		zap.L().Warn("Maximum sessions reached",
			zap.Int("current", len(m.sessions)),
			zap.Int("max", m.maxSessions))
		return nil, fmt.Errorf("maximum number of sessions (%d) reached, current sessions: %d", m.maxSessions, len(m.sessions))
	}

	// Generate session ID
	id := uuid.New().String()

	// Create session
	session, err := NewSession(id, cwd, shell, cols, rows)
	if err != nil {
		return nil, err
	}

	// Store session
	m.sessions[id] = session

	// Set exit callback
	session.SetOnExit(func(code int) {
		m.handleSessionExit(id, code)
	})

	return session, nil
}

// GetSession returns a session by ID.
func (m *Manager) GetSession(id string) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session ID is empty")
	}

	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}

	return session, nil
}

// KillSession kills and removes a session.
func (m *Manager) KillSession(id string) error {
	if id == "" {
		return fmt.Errorf("session ID is empty")
	}

	m.mu.Lock()
	session, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session not found: %s", id)
	}

	// Remove from map first to prevent double cleanup
	delete(m.sessions, id)
	m.mu.Unlock()

	// Close session
	if err := session.Close(); err != nil {
		zap.L().Warn("Error closing session", zap.String("id", id), zap.Error(err))
	}

	zap.L().Info("Terminal session killed", zap.String("id", id))
	return nil
}

// ListSessions returns all active sessions.
func (m *Manager) ListSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}

	return sessions
}

// SetOnSessionExit sets the callback for session exit.
func (m *Manager) SetOnSessionExit(fn func(sessionID string, code int)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSessionExit = fn
}

// handleSessionExit handles session exit.
func (m *Manager) handleSessionExit(id string, code int) {
	m.mu.Lock()
	delete(m.sessions, id)
	onExit := m.onSessionExit
	m.mu.Unlock()

	zap.L().Info("Terminal session exited", zap.String("id", id), zap.Int("code", code))

	if onExit != nil {
		onExit(id, code)
	}
}

// Close closes all sessions and stops the manager.
func (m *Manager) Close() error {
	zap.L().Info("Closing terminal manager")

	// Cancel context
	m.cancel()

	// Close all sessions
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for id, session := range m.sessions {
		if err := session.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close session %s: %w", id, err))
		}
	}

	m.sessions = make(map[string]*Session)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing sessions: %v", errs)
	}

	return nil
}

// KillAllSessions kills all active sessions.
func (m *Manager) KillAllSessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for id, session := range m.sessions {
		if err := session.Close(); err != nil {
			zap.L().Warn("Error closing session during kill all", zap.String("id", id), zap.Error(err))
		}
		count++
	}

	m.sessions = make(map[string]*Session)
	zap.L().Info("All terminal sessions killed", zap.Int("count", count))
	return count
}

// SessionCount returns the current number of active sessions.
func (m *Manager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}