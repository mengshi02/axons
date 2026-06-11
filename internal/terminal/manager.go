package terminal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Manager manages terminal sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex

	// Configuration
	maxSessions int

	// PersistManager for snapshot persistence (P3 Revive)
	persistMgr *PersistManager

	// Revive mode: when to restore sessions on restart
	reviveMode ReviveProcessMode

	// Callbacks
	onSessionExit func(sessionID string, code int)

	// Orphan request channel: when a session needs orphan_req sent to frontend,
	// it writes the sessionID here. The API layer reads from this channel
	// and broadcasts orphan_req via WebSocket to all subscribers.
	orphanReqCh chan string

	// Detach broadcast: when a session is being killed by grace timer,
	// it writes the sessionID here. The API layer broadcasts detach to all subscribers.
	detachCh chan string

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// ManagerConfig holds manager configuration.
type ManagerConfig struct {
	MaxSessions int                // Max sessions total (default: 10)
	ReviveMode  ReviveProcessMode  // Revive mode (default: onExit)
	PersistDir  string             // Snapshot directory (default: ~/.axons/terminal-snapshots)
}

// NewManager creates a new terminal manager.
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 10
	}
	if cfg.ReviveMode == "" {
		cfg.ReviveMode = ReviveOnExit
	}

	ctx, cancel := context.WithCancel(context.Background())

	persistMgr := NewPersistManager(cfg.PersistDir)
	// Clean up stale snapshots older than 24h on startup (P3-4)
	persistMgr.CleanupStaleSnapshots(24 * time.Hour)

	m := &Manager{
		sessions:    make(map[string]*Session),
		maxSessions: cfg.MaxSessions,
		persistMgr:  persistMgr,
		reviveMode:  cfg.ReviveMode,
		orphanReqCh: make(chan string, 64),
		detachCh:    make(chan string, 64),
		ctx:         ctx,
		cancel:      cancel,
	}

	zap.L().Info("Terminal manager initialized",
		zap.Int("maxSessions", cfg.MaxSessions),
		zap.String("reviveMode", string(cfg.ReviveMode)))

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

	// Set orphan request callback — session will send sessionID to channel
	// when it needs orphan_req to be forwarded to frontend via WebSocket
	session.SetOnOrphanReq(func(sid string) {
		select {
		case m.orphanReqCh <- sid:
		default:
			zap.L().Warn("Orphan request channel full, dropping",
				zap.String("sessionID", sid))
		}
	})

	// Set detach callback — session will send sessionID to channel
	// before being killed by grace timer, so frontend gets detach message
	session.SetOnDetach(func(sid string) {
		select {
		case m.detachCh <- sid:
		default:
			zap.L().Warn("Detach channel full, dropping",
				zap.String("sessionID", sid))
		}
	})

	// Set persist manager for periodic snapshots and graceful shutdown
	session.SetPersistManager(m.persistMgr)

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

	// Delete snapshot from disk (session is intentionally killed)
	if m.persistMgr != nil {
		m.persistMgr.DeleteSnapshot(id)
	}

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

	// Persist all running sessions before closing (P3 graceful shutdown)
	m.mu.Lock()
	for id, session := range m.sessions {
		if session.isRunning() {
			if err := session.PersistTerminalState(); err != nil {
				zap.L().Warn("Failed to persist session state on shutdown",
					zap.String("id", id), zap.Error(err))
			}
		}
	}
	m.mu.Unlock()

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

// OrphanReqCh returns the channel that receives session IDs needing orphan_req.
// The API layer reads from this channel and broadcasts orphan_req via WebSocket.
func (m *Manager) OrphanReqCh() <-chan string {
	return m.orphanReqCh
}

// DetachCh returns the channel that receives session IDs being killed by grace timer.
// The API layer reads from this channel and broadcasts detach via WebSocket.
func (m *Manager) DetachCh() <-chan string {
	return m.detachCh
}

// BroadcastOrphanReq sends orphan_req to all output subscribers of the given session.
// This is called by the API layer when it receives a sessionID from OrphanReqCh.
func (m *Manager) BroadcastOrphanReq(sessionID string) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok || !session.isRunning() {
		return
	}

	// Build orphan_req message and send it as a special OutputEntry
	msg := NewOrphanReqMessage()
	data, err := EncodeMessage(msg)
	if err != nil {
		zap.L().Error("Failed to encode orphan_req", zap.Error(err))
		return
	}

	// Broadcast as output to all subscribers (they'll decode and recognize orphan_req type)
	session.broadcastOutput(data)
}

// PersistMgr returns the persist manager for external access (e.g. API layer).
func (m *Manager) PersistMgr() *PersistManager {
	return m.persistMgr
}

// ReviveSessions restores terminal sessions from disk snapshots.
// Aligns with IDE reviveTerminalProcesses.
// Returns a list of ReviveResult containing new session info + replay data.
func (m *Manager) ReviveSessions() []ReviveResult {
	if m.reviveMode == ReviveNever {
		return nil
	}

	snapshots, err := m.persistMgr.ReadAllSnapshots()
	if err != nil || len(snapshots) == 0 {
		return nil
	}

	var results []ReviveResult
	for _, snap := range snapshots {
		// Generate new session ID for the revived session
		newID := uuid.New().String()

		// Create new PTY + shell for the revived session
		session, err := NewSession(newID, snap.ShellLaunchConfig.Cwd, snap.ShellLaunchConfig.Executable, 120, 40)
		if err != nil {
			zap.L().Warn("Failed to revive session",
				zap.String("originalID", snap.ID),
				zap.Error(err))
			continue
		}

		// Store session
		m.mu.Lock()
		m.sessions[newID] = session
		m.mu.Unlock()

		// Set callbacks
		session.SetOnOrphanReq(func(sid string) {
			select {
			case m.orphanReqCh <- sid:
			default:
			}
		})
		session.SetOnDetach(func(sid string) {
			select {
			case m.detachCh <- sid:
			default:
			}
		})
		session.SetOnExit(func(code int) {
			m.handleSessionExit(newID, code)
		})
		session.SetPersistManager(m.persistMgr)

		// Delete old snapshot (session has been revived with new ID)
		m.persistMgr.DeleteSnapshot(snap.ID)

		zap.L().Info("Revived terminal session",
			zap.String("originalID", snap.ID),
			zap.String("newID", newID))

		results = append(results, ReviveResult{
			SessionID:   newID,
			ReplayEvent: snap.ReplayEvent,
			Source:      snap.Source,
		})
	}

	return results
}

// ReviveResult holds the result of reviving a terminal session.
type ReviveResult struct {
	SessionID   string      // New session ID
	ReplayEvent ReplayEvent // Serialized replay data for frontend
	Source      string      // "serialize" | "ringbuffer"
}

// BroadcastDetach sends detach message to all output subscribers of the given session.
// This is called when a session is being killed by grace timer.
func (m *Manager) BroadcastDetach(sessionID string) {
	m.mu.RLock()
	session, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok || !session.isRunning() {
		return
	}

	msg := NewDetachMessage()
	data, err := EncodeMessage(msg)
	if err != nil {
		zap.L().Error("Failed to encode detach", zap.Error(err))
		return
	}

	session.broadcastOutput(data)
}