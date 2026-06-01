package db

import (
	"database/sql"
	"fmt"
	"time"
)

// MainSchemaVersion is the current schema version for the main database.
const MainSchemaVersion = 7

// MigrateMain runs migrations on the main database (axons.db).
// Main DB contains: projects, settings, agent_profiles, agent_memory (sqlite_memory).
func MigrateMain(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS metadata (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create metadata table: %w", err)
	}

	var currentVersion int
	row := db.QueryRow("SELECT CAST(value AS INTEGER) FROM metadata WHERE key = 'main_schema_version'")
	if err := row.Scan(&currentVersion); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check main schema version: %w", err)
	}

	if currentVersion < 1 {
		if err := migrateMainV1(db); err != nil {
			return err
		}
	}

	if currentVersion < 2 {
		if err := migrateMainV2(db); err != nil {
			return err
		}
	}

	if currentVersion < 3 {
		if err := migrateMainV3(db); err != nil {
			return err
		}
	}

	if currentVersion < 4 {
		if err := migrateMainV4(db); err != nil {
			return err
		}
	}

	if currentVersion < 5 {
		if err := migrateMainV5(db); err != nil {
			return err
		}
	}

	if currentVersion < 6 {
		if err := migrateMainV6(db); err != nil {
			return err
		}
	}

	if currentVersion < 7 {
		if err := migrateMainV7(db); err != nil {
			return err
		}
	}

	return nil
}

func migrateMainV1(db *sql.DB) error {
	// projects table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL UNIQUE,
			root_path  TEXT NOT NULL,
			watch_enabled INTEGER DEFAULT 0,
			watch_status  TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create projects table: %w", err)
	}

	// settings table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS settings (
			key         TEXT PRIMARY KEY,
			value       TEXT NOT NULL,
			category    TEXT NOT NULL DEFAULT 'general',
			description TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create settings table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_settings_category ON settings(category)`); err != nil {
		return fmt.Errorf("failed to create settings index: %w", err)
	}

	// agent_profiles table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_profiles (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL,
			description   TEXT NOT NULL DEFAULT '',
			icon          TEXT NOT NULL DEFAULT 'bot',
			tools         TEXT NOT NULL DEFAULT '[]',
			system_prompt TEXT NOT NULL DEFAULT '',
			allow_write   INTEGER NOT NULL DEFAULT 0,
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create agent_profiles table: %w", err)
	}

	// agent_memory table (used by agent.SQLiteMemory)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_memory (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id TEXT,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create agent_memory table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_memory_session ON agent_memory(session_id)`); err != nil {
		return fmt.Errorf("failed to create agent_memory index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_memory_project ON agent_memory(project_id)`); err != nil {
		return fmt.Errorf("failed to create agent_memory project index: %w", err)
	}

	// Insert default settings
	now := time.Now().Format(time.RFC3339)
	defaultSettings := []struct{ key, value, category, description string }{
		{"embedding_enabled", "false", "embedding", "Enable embedding generation"},
		{"embedding_provider", "", "embedding", "Embedding provider: openai, ollama, jina"},
		{"embedding_api_key", "", "embedding", "API key for embedding provider"},
		{"embedding_base_url", "", "embedding", "Base URL for embedding provider (optional)"},
		{"embedding_model", "", "embedding", "Embedding model name"},
		{"embedding_batch_size", "50", "embedding", "Batch size for embedding API calls"},
		{"embedding_dimension", "1536", "embedding", "Embedding vector dimension"},
		{"embedding_max_context_tokens", "0", "embedding", "Max context tokens (n_ctx) for embedding model. 0=default(512)"},
		{"llm_provider", "", "llm", "LLM provider: openai, anthropic, gemini, ollama, openrouter"},
		{"llm_api_key", "", "llm", "API key for LLM provider"},
		{"llm_base_url", "", "llm", "Base URL for LLM provider (optional)"},
		{"llm_model", "", "llm", "LLM model name"},
		{"llm_temperature", "0.7", "llm", "LLM temperature"},
		{"llm_max_tokens","4096", "llm", "Max tokens for LLM response"},
		{"rerank_enabled", "false", "rerank", "Enable rerank step in RAG"},
		{"rerank_provider", "", "rerank", "Rerank provider: cohere, jina"},
		{"rerank_api_key", "", "rerank", "API key for rerank provider"},
		{"rerank_model", "", "rerank", "Rerank model name"},
		{"rerank_top_n", "20", "rerank", "Number of results to rerank"},
		{"rag_search_limit", "10", "rag", "Default search result limit"},
		{"rag_min_score", "0.2", "rag", "Minimum similarity score threshold"},
		{"search_mode", "hybrid", "search", "Search mode: keyword, semantic, hybrid"},
		{"search_limit", "10", "search", "Default search result limit"},
		{"search_min_score", "0.2", "search", "Minimum similarity score threshold"},
		{"rrf_k", "60", "search", "RRF fusion parameter k"},
	}
	for _, s := range defaultSettings {
		if _, err := db.Exec(`
			INSERT OR IGNORE INTO settings (key, value, category, description, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, s.key, s.value, s.category, s.description, now, now); err != nil {
			return fmt.Errorf("failed to insert default setting %s: %w", s.key, err)
		}
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('main_schema_version', '1')`); err != nil {
		return fmt.Errorf("failed to update main schema version: %w", err)
	}
	return nil
}

// migrateMainV2 adds language_stack column to projects table
func migrateMainV2(db *sql.DB) error {
	// Add language_stack column to projects table
	if _, err := db.Exec(`
		ALTER TABLE projects ADD COLUMN language_stack TEXT DEFAULT '[]'
	`); err != nil {
		// Column might already exist, which is fine
		fmt.Printf("Warning: could not add language_stack column: %v\n", err)
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('main_schema_version', '2')`); err != nil {
		return fmt.Errorf("failed to update main schema version: %w", err)
	}
	return nil
}

// migrateMainV3 adds remote repository tracking columns to projects table
func migrateMainV3(db *sql.DB) error {
	// Add source tracking columns
	columns := []struct {
		name string
		sql  string
	}{
		{"source", "TEXT DEFAULT 'local'"},
		{"provider", "TEXT DEFAULT ''"},
		{"remote_url", "TEXT DEFAULT ''"},
		{"clone_mode", "TEXT DEFAULT ''"},
		{"managed", "BOOLEAN DEFAULT 0"},
		{"branch", "TEXT DEFAULT 'main'"},
		{"cloned_at", "DATETIME"},
	}

	for _, col := range columns {
		if _, err := db.Exec(fmt.Sprintf("ALTER TABLE projects ADD COLUMN %s %s", col.name, col.sql)); err != nil {
			// Column might already exist, which is fine
			fmt.Printf("Warning: could not add %s column: %v\n", col.name, err)
		}
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('main_schema_version', '3')`); err != nil {
		return fmt.Errorf("failed to update main schema version: %w", err)
	}
	return nil
}

// migrateMainV4 adds project_id column to agent_memory table
func migrateMainV4(db *sql.DB) error {
	// Add project_id column to agent_memory table
	if _, err := db.Exec(`ALTER TABLE agent_memory ADD COLUMN project_id TEXT`); err != nil {
		// Column might already exist, which is fine
		fmt.Printf("Warning: could not add project_id column to agent_memory: %v\n", err)
	}

	// Add index for project_id
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_memory_project ON agent_memory(project_id)`); err != nil {
		fmt.Printf("Warning: could not create agent_memory project index: %v\n", err)
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('main_schema_version', '4')`); err != nil {
		return fmt.Errorf("failed to update main schema version: %w", err)
	}
	return nil
}

// migrateMainV5 adds agent_id column and name column to agent_memory table
func migrateMainV5(db *sql.DB) error {
	// Add name column to agent_memory table (for tool role messages)
	if _, err := db.Exec(`ALTER TABLE agent_memory ADD COLUMN name TEXT`); err != nil {
		// Column might already exist, which is fine
		fmt.Printf("Warning: could not add name column to agent_memory: %v\n", err)
	}

	// Add agent_id column to agent_memory table (for agent isolation)
	if _, err := db.Exec(`ALTER TABLE agent_memory ADD COLUMN agent_id TEXT`); err != nil {
		// Column might already exist, which is fine
		fmt.Printf("Warning: could not add agent_id column to agent_memory: %v\n", err)
	}

	// Add index for agent_id
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_memory_agent ON agent_memory(agent_id)`); err != nil {
		fmt.Printf("Warning: could not create agent_memory agent index: %v\n", err)
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('main_schema_version', '5')`); err != nil {
		return fmt.Errorf("failed to update main schema version: %w", err)
	}
	return nil
}

// migrateMainV6 adds file_changes table for AI file change tracking
func migrateMainV6(db *sql.DB) error {
	// Create file_changes table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS file_changes (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT NOT NULL,
			project_id   TEXT NOT NULL,
			file_path    TEXT NOT NULL,
			change_type  TEXT NOT NULL,
			content_hash TEXT,
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			
			UNIQUE(session_id, file_path)
		)
	`); err != nil {
		return fmt.Errorf("failed to create file_changes table: %w", err)
	}

	// Create indexes
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_file_changes_session ON file_changes(session_id)`); err != nil {
		return fmt.Errorf("failed to create file_changes session index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_file_changes_project ON file_changes(project_id)`); err != nil {
		return fmt.Errorf("failed to create file_changes project index: %w", err)
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('main_schema_version', '6')`); err != nil {
		return fmt.Errorf("failed to update main schema version: %w", err)
	}
	return nil
}

// migrateMainV7 adds notifications table for the notification system
func migrateMainV7(db *sql.DB) error {
	// Create notifications table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS notifications (
			id         TEXT PRIMARY KEY,
			source     TEXT    NOT NULL,
			type       TEXT    NOT NULL DEFAULT 'info',
			title      TEXT    NOT NULL,
			message    TEXT    DEFAULT '',
			actions    TEXT    DEFAULT '[]',
			"group"    TEXT    DEFAULT '',
			read       INTEGER NOT NULL DEFAULT 0,
			timestamp  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create notifications table: %w", err)
	}

	// Create indexes
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications(read, timestamp DESC)`); err != nil {
		return fmt.Errorf("failed to create notifications unread index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_notifications_source ON notifications(source, timestamp DESC)`); err != nil {
		return fmt.Errorf("failed to create notifications source index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_notifications_group ON notifications("group", timestamp DESC)`); err != nil {
		return fmt.Errorf("failed to create notifications group index: %w", err)
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('main_schema_version', '7')`); err != nil {
		return fmt.Errorf("failed to update main schema version: %w", err)
	}
	return nil
}