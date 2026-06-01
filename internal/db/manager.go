// Package db provides database connection, migrations, and manager.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Manager manages main database and per-project databases.
// Main DB (axons.db): holds projects, settings, agent_profiles, agent_memory.
// Project DB ({uuid}.db): holds all code-graph tables for a single project.
type Manager struct {
	mainConn *Connection
	mainDB   *sql.DB

	mu       sync.RWMutex
	projects map[string]*Connection // key: project UUID (string form of projects.id)
	dbDir    string                 // directory where project DBs are stored
}

// NewManager opens the main database, runs main migrations, and returns a Manager.
func NewManager(mainPath string) (*Manager, error) {
	conn, err := Open(mainPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open main database: %w", err)
	}

	if err := MigrateMain(conn.DB()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run main migrations: %w", err)
	}

	m := &Manager{
		mainConn: conn,
		mainDB:   conn.DB(),
		projects: make(map[string]*Connection),
		dbDir:    filepath.Dir(mainPath),
	}
	return m, nil
}

// MainDB returns the main database connection.
func (m *Manager) MainDB() *sql.DB {
	return m.mainDB
}

// MainConn returns the main Connection.
func (m *Manager) MainConn() *Connection {
	return m.mainConn
}

// ProjectDB returns the *sql.DB for a project, opening it if necessary.
// projectID is the string UUID stored in projects.id.
func (m *Manager) ProjectDB(projectID string) (*sql.DB, error) {
	m.mu.RLock()
	conn, ok := m.projects[projectID]
	m.mu.RUnlock()
	if ok {
		return conn.DB(), nil
	}

	// Open (and migrate) the project database
	dbPath := filepath.Join(m.dbDir, projectID+".db")
	conn, err := Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open project database %s: %w", projectID, err)
	}
	if err := MigrateProject(conn.DB()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to migrate project database %s: %w", projectID, err)
	}

	m.mu.Lock()
	m.projects[projectID] = conn
	m.mu.Unlock()
	return conn.DB(), nil
}

// CloseProject closes the connection to a project database and removes it from cache.
// Call this when a project is deleted.
func (m *Manager) CloseProject(projectID string) error {
	m.mu.Lock()
	conn, ok := m.projects[projectID]
	if ok {
		delete(m.projects, projectID)
	}
	m.mu.Unlock()

	if ok {
		return conn.Close()
	}
	return nil
}

// DeleteProjectDB closes and removes the physical .db file for a project.
func (m *Manager) DeleteProjectDB(projectID string) error {
	if err := m.CloseProject(projectID); err != nil {
		return err
	}
	dbPath := filepath.Join(m.dbDir, projectID+".db")
	// Also remove WAL/SHM files
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := dbPath + suffix
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove project db file %s: %w", path, err)
		}
	}
	return nil
}

// Close closes all database connections managed by this Manager.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for id, conn := range m.projects {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close project db %s: %w", id, err)
		}
		delete(m.projects, id)
	}

	if err := m.mainConn.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("failed to close main db: %w", err)
	}

	return firstErr
}