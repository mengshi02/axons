// Package db provides database connection and operations.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

// Connection manages the database connection.
type Connection struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// Open opens a database connection.
func Open(path string) (*Connection, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set pragmas for performance
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-64000", // 64MB
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma: %w", err)
		}
	}

	return &Connection{db: db, path: path}, nil
}

// DB returns the underlying database connection.
func (c *Connection) DB() *sql.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.db
}

// Path returns the database path.
func (c *Connection) Path() string {
	return c.path
}

// Close closes the database connection.
func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// BeginTx starts a transaction.
func (c *Connection) BeginTx() (*sql.Tx, error) {
	return c.db.Begin()
}

// Exec executes a query.
func (c *Connection) Exec(query string, args ...any) (sql.Result, error) {
	return c.db.Exec(query, args...)
}

// Query executes a query and returns rows.
func (c *Connection) Query(query string, args ...any) (*sql.Rows, error) {
	return c.db.Query(query, args...)
}

// QueryRow executes a query and returns a single row.
func (c *Connection) QueryRow(query string, args ...any) *sql.Row {
	return c.db.QueryRow(query, args...)
}

// NewConnection creates a new database connection.
// This is an alias for Open for convenience.
func NewConnection(path string) (*sql.DB, error) {
	conn, err := Open(path)
	if err != nil {
		return nil, err
	}
	return conn.db, nil
}

// RunMigrations runs database migrations on the given connection.
func RunMigrations(db *sql.DB) error {
	return Migrate(db)
}
