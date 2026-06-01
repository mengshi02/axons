package agent

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// SQLiteMemory implements conversation memory using SQLite
type SQLiteMemory struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteMemory creates a SQLite Memory instance
// Note: Table migrations are handled in internal/db/migrations_main.go
func NewSQLiteMemory(db *sql.DB) (*SQLiteMemory, error) {
	return &SQLiteMemory{db: db}, nil
}

// Add adds a message
func (m *SQLiteMemory) Add(ctx context.Context, sessionID, role, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx,
		"INSERT INTO agent_memory (session_id, role, content) VALUES (?, ?, ?)",
		sessionID, role, content,
	)
	return err
}

// AddWithName adds a message with tool name (for tool role messages)
func (m *SQLiteMemory) AddWithName(ctx context.Context, sessionID, role, content, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx,
		"INSERT INTO agent_memory (session_id, role, content, name) VALUES (?, ?, ?, ?)",
		sessionID, role, content, name,
	)
	return err
}

// AddWithMeta adds a message with full metadata (projectID, agentID)
func (m *SQLiteMemory) AddWithMeta(ctx context.Context, sessionID, projectID, agentID, role, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx,
		"INSERT INTO agent_memory (session_id, project_id, agent_id, role, content) VALUES (?, ?, ?, ?, ?)",
		sessionID, projectID, agentID, role, content,
	)
	return err
}

// GetHistory retrieves historical messages, filtered by projectID and agentID if provided
func (m *SQLiteMemory) GetHistory(ctx context.Context, sessionID, projectID, agentID string, limit int) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}

	var rows *sql.Rows
	var err error

	// Build query based on filters
	if projectID != "" && agentID != "" {
		// Filter by both projectID and agentID
		rows, err = m.db.QueryContext(ctx,
			"SELECT role, content, name FROM agent_memory WHERE session_id = ? AND (project_id = ? OR project_id IS NULL OR project_id = '') AND agent_id = ? ORDER BY created_at ASC LIMIT ?",
			sessionID, projectID, agentID, limit,
		)
	} else if agentID != "" {
		// Filter by agentID only
		rows, err = m.db.QueryContext(ctx,
			"SELECT role, content, name FROM agent_memory WHERE session_id = ? AND agent_id = ? ORDER BY created_at ASC LIMIT ?",
			sessionID, agentID, limit,
		)
	} else if projectID != "" {
		// Filter by projectID only (backward compatibility)
		rows, err = m.db.QueryContext(ctx,
			"SELECT role, content, name FROM agent_memory WHERE session_id = ? AND (project_id = ? OR project_id IS NULL OR project_id = '') ORDER BY created_at ASC LIMIT ?",
			sessionID, projectID, limit,
		)
	} else {
		// No filters
		rows, err = m.db.QueryContext(ctx,
			"SELECT role, content, name FROM agent_memory WHERE session_id = ? ORDER BY created_at ASC LIMIT ?",
			sessionID, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var name sql.NullString
		if err := rows.Scan(&msg.Role, &msg.Content, &name); err != nil {
			return nil, err
		}
		if name.Valid {
			msg.Name = name.String
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// Clear clears session history and its delegated sub-sessions
// When clearing a main session (e.g., conv-xxx), also clears delegated sessions (e.g., conv-xxx#architect)
func (m *SQLiteMemory) Clear(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Delete main session + all delegated sub-sessions (session_id LIKE sessionID#%)
	_, err := m.db.ExecContext(ctx,
		"DELETE FROM agent_memory WHERE session_id = ? OR session_id LIKE ?",
		sessionID, sessionID+"#%",
	)
	return err
}

// AddWithProject adds a message with project_id
func (m *SQLiteMemory) AddWithProject(ctx context.Context, sessionID, projectID, role, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx,
		"INSERT INTO agent_memory (session_id, project_id, role, content) VALUES (?, ?, ?, ?)",
		sessionID, projectID, role, content,
	)
	return err
}

// AddWithNameAndProject adds a message with tool name and project_id
func (m *SQLiteMemory) AddWithNameAndProject(ctx context.Context, sessionID, projectID, role, content, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.db.ExecContext(ctx,
		"INSERT INTO agent_memory (session_id, project_id, role, content, name) VALUES (?, ?, ?, ?, ?)",
		sessionID, projectID, role, content, name,
	)
	return err
}

// ListSessions lists all sessions for a project
func (m *SQLiteMemory) ListSessions(ctx context.Context, projectID string) ([]SessionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var query string
	var rows *sql.Rows
	var err error

	if projectID != "" {
		// Filter by specific project_id, exclude delegated sessions (containing #)
		// Use agent_id from the most recent non-empty agent_id
		query = `
			SELECT session_id,
			       (SELECT agent_id FROM agent_memory a2 WHERE a2.session_id = a.session_id AND agent_id IS NOT NULL AND agent_id != '' ORDER BY created_at DESC LIMIT 1) as agent_id,
			       COUNT(*) as message_count,
			       MIN(created_at) as created_at,
			       MAX(created_at) as updated_at
			FROM agent_memory a
			WHERE project_id = ? AND session_id NOT LIKE '%#%'
			GROUP BY session_id
			ORDER BY MAX(created_at) DESC
		`
		rows, err = m.db.QueryContext(ctx, query, projectID)
	} else {
		// Get all sessions (for backward compatibility, include sessions without project_id)
		// Exclude delegated sessions (containing #)
		query = `
			SELECT session_id,
			       (SELECT agent_id FROM agent_memory a2 WHERE a2.session_id = a.session_id AND agent_id IS NOT NULL AND agent_id != '' ORDER BY created_at DESC LIMIT 1) as agent_id,
			       COUNT(*) as message_count,
			       MIN(created_at) as created_at,
			       MAX(created_at) as updated_at
			FROM agent_memory a
			WHERE (project_id IS NULL OR project_id = '') AND session_id NOT LIKE '%#%'
			GROUP BY session_id
			ORDER BY MAX(created_at) DESC
		`
		rows, err = m.db.QueryContext(ctx, query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionInfo
	for rows.Next() {
		var info SessionInfo
		var agentID sql.NullString
		if err := rows.Scan(&info.SessionID, &agentID, &info.MessageCount, &info.CreatedAt, &info.UpdatedAt); err != nil {
			return nil, err
		}
		if agentID.Valid {
			info.AgentID = agentID.String
		} else {
			info.AgentID = "default" // Default for old records
		}
		// Convert SQLite timestamp to ISO 8601 format with timezone
		// SQLite stores timestamps as UTC, so we parse and format as RFC3339
		if info.CreatedAt != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", info.CreatedAt); err == nil {
				info.CreatedAt = t.UTC().Format(time.RFC3339)
			}
		}
		if info.UpdatedAt != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", info.UpdatedAt); err == nil {
				info.UpdatedAt = t.UTC().Format(time.RFC3339)
			}
		}
		sessions = append(sessions, info)
	}
	return sessions, rows.Err()
}

// GetSessionHistory retrieves all messages for a session
func (m *SQLiteMemory) GetSessionHistory(ctx context.Context, sessionID string) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rows, err := m.db.QueryContext(ctx,
		"SELECT role, content, name, created_at FROM agent_memory WHERE session_id = ? ORDER BY created_at ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var name sql.NullString
		var createdAt sql.NullString
		if err := rows.Scan(&msg.Role, &msg.Content, &name, &createdAt); err != nil {
			return nil, err
		}
		if name.Valid {
			msg.Name = name.String
		}
		if createdAt.Valid {
			// Convert SQLite timestamp to ISO 8601 format with timezone
			if t, err := time.Parse("2006-01-02 15:04:05", createdAt.String); err == nil {
				msg.CreatedAt = t.UTC().Format(time.RFC3339)
			} else {
				msg.CreatedAt = createdAt.String
			}
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// Close releases resources
func (m *SQLiteMemory) Close() error {
	// SQLite connection is managed externally
	return nil
}

// InMemoryMemory implements conversation memory in memory (for testing)
type InMemoryMemory struct {
	messages map[string][]Message
	mu       sync.RWMutex
}

// NewInMemoryMemory creates an in-memory Memory instance
func NewInMemoryMemory() *InMemoryMemory {
	return &InMemoryMemory{
		messages: make(map[string][]Message),
	}
}

// Add adds a message
func (m *InMemoryMemory) Add(ctx context.Context, sessionID, role, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages[sessionID] = append(m.messages[sessionID], Message{
		Role:    role,
		Content: content,
	})
	return nil
}

// AddWithName adds a message with tool name
func (m *InMemoryMemory) AddWithName(ctx context.Context, sessionID, role, content, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages[sessionID] = append(m.messages[sessionID], Message{
		Role:    role,
		Content: content,
		Name:    name,
	})
	return nil
}

// AddWithMeta adds a message with full metadata (projectID, agentID) - for InMemoryMemory, just stores the message
func (m *InMemoryMemory) AddWithMeta(ctx context.Context, sessionID, projectID, agentID, role, content string) error {
	return m.Add(ctx, sessionID, role, content)
}

// GetHistory retrieves historical messages, filtered by projectID and agentID if provided
func (m *InMemoryMemory) GetHistory(ctx context.Context, sessionID, projectID, agentID string, limit int) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	msgs := m.messages[sessionID]
	if limit <= 0 || limit > len(msgs) {
		limit = len(msgs)
	}
	if len(msgs) <= limit {
		return msgs, nil
	}
	return msgs[len(msgs)-limit:], nil
}

// Clear clears session history and its delegated sub-sessions
func (m *InMemoryMemory) Clear(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Delete main session
	delete(m.messages, sessionID)
	// Delete all delegated sub-sessions
	for sid := range m.messages {
		if len(sid) > len(sessionID) && sid[:len(sessionID)] == sessionID && sid[len(sessionID)] == '#' {
			delete(m.messages, sid)
		}
	}
	return nil
}

// Close releases resources
func (m *InMemoryMemory) Close() error {
	return nil
}