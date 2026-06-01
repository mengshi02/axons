// Package db provides database migrations.
package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// SchemaVersion is the current schema version.
const SchemaVersion = 15

// DefaultEmbeddingDimensions is the default dimension for embeddings.
const DefaultEmbeddingDimensions = 1536

// Migrate runs database migrations.
func Migrate(db *sql.DB) error {
	// Create metadata table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create metadata table: %w", err)
	}

	// Check current version
	var currentVersion int
	row := db.QueryRow("SELECT CAST(value AS INTEGER) FROM metadata WHERE key = 'schema_version'")
	if err := row.Scan(&currentVersion); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check schema version: %w", err)
	}

	// Run migrations
	if currentVersion < 1 {
		if err := migrateV1(db); err != nil {
			return err
		}
	}

	if currentVersion < 2 {
		if err := migrateV2(db); err != nil {
			return err
		}
	}

	if currentVersion < 3 {
		if err := migrateV3(db); err != nil {
			return err
		}
	}

	if currentVersion < 4 {
		if err := migrateV4(db); err != nil {
			return err
		}
	}

	if currentVersion < 5 {
		if err := migrateV5(db); err != nil {
			return err
		}
	}

	if currentVersion < 6 {
		if err := migrateV6(db); err != nil {
			return err
		}
	}

	if currentVersion < 7 {
		if err := migrateV7(db); err != nil {
			return err
		}
	}

	if currentVersion < 8 {
		if err := migrateV8(db); err != nil {
			return err
		}
	}

	if currentVersion < 9 {
		if err := migrateV9(db); err != nil {
			return err
		}
	}

	if currentVersion < 10 {
		if err := migrateV10(db); err != nil {
			return err
		}
	}

	if currentVersion < 11 {
		if err := migrateV11(db); err != nil {
			return err
		}
	}

	if currentVersion < 12 {
		if err := migrateV12(db); err != nil {
			return err
		}
	}

	if currentVersion < 13 {
		if err := migrateV13(db); err != nil {
			return err
		}
	}

	if currentVersion < 14 {
		if err := migrateV14(db); err != nil {
			return err
		}
	}

	if currentVersion < 15 {
		if err := migrateV15(db); err != nil {
			return err
		}
	}

	return nil
}

// migrateV1 creates the initial schema.
func migrateV1(db *sql.DB) error {
	// Nodes table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS nodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			file TEXT NOT NULL,
			line INTEGER NOT NULL,
			end_line INTEGER,
			parent_id INTEGER,
			exported INTEGER DEFAULT 0,
			qualified_name TEXT,
			scope TEXT,
			visibility TEXT,
			role TEXT,
			file_hash TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create nodes table: %w", err)
	}

	// Create indexes on nodes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_kind ON nodes(kind)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_file ON nodes(file)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_parent_id ON nodes(parent_id)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_qualified_name ON nodes(qualified_name)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_file_hash ON nodes(file_hash)",
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Edges table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id INTEGER NOT NULL,
			target_id INTEGER NOT NULL,
			kind TEXT NOT NULL,
			confidence REAL DEFAULT 1.0,
			dynamic INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE,
			FOREIGN KEY (target_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create edges table: %w", err)
	}

	// Create indexes on edges
	edgeIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_edges_source_id ON edges(source_id)",
		"CREATE INDEX IF NOT EXISTS idx_edges_target_id ON edges(target_id)",
		"CREATE INDEX IF NOT EXISTS idx_edges_kind ON edges(kind)",
		"CREATE INDEX IF NOT EXISTS idx_edges_source_target ON edges(source_id, target_id)",
	}
	for _, idx := range edgeIndexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create edge index: %w", err)
		}
	}

	// Files table (for incremental build)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			path TEXT PRIMARY KEY,
			mtime INTEGER NOT NULL,
			size INTEGER NOT NULL,
			hash TEXT NOT NULL,
			parsed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create files table: %w", err)
	}

	// Embeddings table (for semantic search)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS embeddings (
			node_id INTEGER PRIMARY KEY,
			embedding BLOB NOT NULL,
			model TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create embeddings table: %w", err)
	}

	// Complexity table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS function_complexity (
			node_id INTEGER PRIMARY KEY,
			cyclomatic INTEGER NOT NULL,
			cognitive INTEGER NOT NULL,
			nesting INTEGER NOT NULL,
			lines_of_code INTEGER NOT NULL,
			halstead_volume REAL,
			halstead_difficulty REAL,
			halstead_effort REAL,
			halstead_time REAL,
			halstead_bugs REAL,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create function_complexity table: %w", err)
	}

	// CFG tables
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cfg_blocks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id INTEGER NOT NULL,
			block_type TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create cfg_blocks table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cfg_edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_block_id INTEGER NOT NULL,
			target_block_id INTEGER NOT NULL,
			edge_type TEXT NOT NULL,
			FOREIGN KEY (source_block_id) REFERENCES cfg_blocks(id) ON DELETE CASCADE,
			FOREIGN KEY (target_block_id) REFERENCES cfg_blocks(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create cfg_edges table: %w", err)
	}

	// Dataflow table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS dataflow_edges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_node_id INTEGER NOT NULL,
			target_node_id INTEGER NOT NULL,
			flow_type TEXT NOT NULL,
			variable TEXT,
			line INTEGER,
			FOREIGN KEY (source_node_id) REFERENCES nodes(id) ON DELETE CASCADE,
			FOREIGN KEY (target_node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create dataflow_edges table: %w", err)
	}

	// AST nodes table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS ast_nodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id INTEGER,
			file TEXT NOT NULL,
			ast_type TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			start_column INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			end_column INTEGER NOT NULL,
			parent_ast_id INTEGER,
			text_content TEXT,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create ast_nodes table: %w", err)
	}

	// Co-change table (Git history analysis)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS co_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file1 TEXT NOT NULL,
			file2 TEXT NOT NULL,
			co_change_count INTEGER NOT NULL,
			last_co_change DATETIME,
			UNIQUE(file1, file2)
		)
	`); err != nil {
		return fmt.Errorf("failed to create co_changes table: %w", err)
	}

	// Journal table (for incremental build tracking)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS journal (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL,
			event_type TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create journal table: %w", err)
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '1')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV2 adds sqlite-vec virtual table for vector search.
func migrateV2(db *sql.DB) error {
	// Create vec0 virtual table for embeddings
	// We use float32 vectors with configurable dimensions (default 1536 for OpenAI)
	// The virtual table stores embeddings with node_id as the identifier
	if _, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_embeddings USING vec0(
			embedding float[1536]
		)
	`); err != nil {
		return fmt.Errorf("failed to create vec_embeddings virtual table: %w", err)
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '2')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV3 adds project support.
func migrateV3(db *sql.DB) error {
	// Create projects table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			root_path TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create projects table: %w", err)
	}

	// Add project_id to nodes table
	if _, err := db.Exec(`
		ALTER TABLE nodes ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE
	`); err != nil {
		// Column might already exist, ignore error
		if err.Error() != "duplicate column name: project_id" {
			return fmt.Errorf("failed to add project_id to nodes: %w", err)
		}
	}

	// Create index on project_id
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_nodes_project_id ON nodes(project_id)
	`); err != nil {
		return fmt.Errorf("failed to create index on project_id: %w", err)
	}

	// Add project_id to edges table
	if _, err := db.Exec(`
		ALTER TABLE edges ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE
	`); err != nil {
		// Column might already exist, ignore error
		if err.Error() != "duplicate column name: project_id" {
			return fmt.Errorf("failed to add project_id to edges: %w", err)
		}
	}

	// Create index on edges.project_id
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_edges_project_id ON edges(project_id)
	`); err != nil {
		return fmt.Errorf("failed to create index on edges.project_id: %w", err)
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '3')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV4 adds watch support to projects.
func migrateV4(db *sql.DB) error {
	// Add watch_enabled column to projects table
	if _, err := db.Exec(`
		ALTER TABLE projects ADD COLUMN watch_enabled INTEGER DEFAULT 0
	`); err != nil {
		// Column might already exist, ignore error
		if !strings.Contains(err.Error(), "duplicate column name: watch_enabled") {
			return fmt.Errorf("failed to add watch_enabled to projects: %w", err)
		}
	}

	// Add watch_status column to projects table
	if _, err := db.Exec(`
		ALTER TABLE projects ADD COLUMN watch_status TEXT DEFAULT ''
	`); err != nil {
		// Column might already exist, ignore error
		if !strings.Contains(err.Error(), "duplicate column name: watch_status") {
			return fmt.Errorf("failed to add watch_status to projects: %w", err)
		}
	}

	// Create change_history table for tracking file changes
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS change_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			file_path TEXT NOT NULL,
			change_type TEXT NOT NULL,
			change_time DATETIME DEFAULT CURRENT_TIMESTAMP,
			processed INTEGER DEFAULT 0,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create change_history table: %w", err)
	}

	// Create index on change_history
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_change_history_project_id ON change_history(project_id)
	`); err != nil {
		return fmt.Errorf("failed to create index on change_history: %w", err)
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '4')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV5 adds settings table for configuration storage.
func migrateV5(db *sql.DB) error {
	// Create settings table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT 'general',
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create settings table: %w", err)
	}

	// Create index on category
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_settings_category ON settings(category)
	`); err != nil {
		return fmt.Errorf("failed to create index on settings: %w", err)
	}

	// Add content_hash column to embeddings table for incremental updates
	if _, err := db.Exec(`
		ALTER TABLE embeddings ADD COLUMN content_hash TEXT
	`); err != nil {
		// Column might already exist, ignore error
		if err.Error() != "duplicate column name: content_hash" {
			return fmt.Errorf("failed to add content_hash to embeddings: %w", err)
		}
	}

	// Add updated_at column to embeddings table
	if _, err := db.Exec(`
		ALTER TABLE embeddings ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	`); err != nil {
		// Column might already exist, ignore error
		if err.Error() != "duplicate column name: updated_at" {
			return fmt.Errorf("failed to add updated_at to embeddings: %w", err)
		}
	}

	// Insert default settings
	defaultSettings := []struct {
		key, value, category, description string
	}{
		// Embedding settings
		{"embedding_enabled", "false", "embedding", "Enable embedding generation"},
		{"embedding_provider", "", "embedding", "Embedding provider: openai, ollama, jina"},
		{"embedding_api_key", "", "embedding", "API key for embedding provider"},
		{"embedding_base_url", "", "embedding", "Base URL for embedding provider (optional)"},
		{"embedding_model", "", "embedding", "Embedding model name"},
		{"embedding_batch_size", "50", "embedding", "Batch size for embedding API calls"},
		{"embedding_dimension", "1536", "embedding", "Embedding vector dimension"},
		{"embedding_max_context_tokens", "0", "embedding", "Max context tokens (n_ctx) for embedding model. 0=default(512)"},

		// LLM settings
		{"llm_provider", "", "llm", "LLM provider: openai, anthropic, gemini, ollama, openrouter"},
		{"llm_api_key", "", "llm", "API key for LLM provider"},
		{"llm_base_url", "", "llm", "Base URL for LLM provider (optional)"},
		{"llm_model", "", "llm", "LLM model name"},
		{"llm_temperature", "0.7", "llm", "LLM temperature"},
		{"llm_max_tokens", "4096", "llm", "Max tokens for LLM response"},

		// Rerank settings
		{"rerank_enabled", "false", "rerank", "Enable rerank step in RAG"},
		{"rerank_provider", "", "rerank", "Rerank provider: cohere, jina"},
		{"rerank_api_key", "", "rerank", "API key for rerank provider"},
		{"rerank_model", "", "rerank", "Rerank model name"},
		{"rerank_top_n", "20", "rerank", "Number of results to rerank"},

		// RAG settings
		{"rag_search_limit", "10", "rag", "Default search result limit"},
		{"rag_min_score", "0.2", "rag", "Minimum similarity score threshold"},
	}

	for _, s := range defaultSettings {
		if _, err := db.Exec(`
			INSERT OR IGNORE INTO settings (key, value, category, description)
			VALUES (?, ?, ?, ?)
		`, s.key, s.value, s.category, s.description); err != nil {
			return fmt.Errorf("failed to insert default setting %s: %w", s.key, err)
		}
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '5')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV6 adds FTS5 full-text search support and docstring field.
func migrateV6(db *sql.DB) error {
	// Add docstring column to nodes table if not exists
	if _, err := db.Exec(`
		ALTER TABLE nodes ADD COLUMN docstring TEXT
	`); err != nil {
		// Column might already exist, ignore error
		if !isDuplicateColumnError(err) {
			return fmt.Errorf("failed to add docstring column: %w", err)
		}
	}

	// Create FTS5 virtual table for full-text search
	// Using content='nodes' to link with the nodes table
	if _, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
			name,
			qualified_name,
			docstring,
			content='nodes',
			content_rowid='id',
			tokenize='porter unicode61'
		)
	`); err != nil {
		return fmt.Errorf("failed to create nodes_fts virtual table: %w", err)
	}

	// Create triggers to keep FTS5 index in sync with nodes table

	// INSERT trigger
	if _, err := db.Exec(`
		CREATE TRIGGER IF NOT EXISTS nodes_fts_ai AFTER INSERT ON nodes BEGIN
			INSERT INTO nodes_fts(rowid, name, qualified_name, docstring)
			VALUES (new.id, new.name, COALESCE(new.qualified_name, ''), COALESCE(new.docstring, ''));
		END
	`); err != nil {
		return fmt.Errorf("failed to create nodes_fts_ai trigger: %w", err)
	}

	// DELETE trigger
	if _, err := db.Exec(`
		CREATE TRIGGER IF NOT EXISTS nodes_fts_ad AFTER DELETE ON nodes BEGIN
			INSERT INTO nodes_fts(nodes_fts, rowid, name, qualified_name, docstring)
			VALUES ('delete', old.id, old.name, COALESCE(old.qualified_name, ''), COALESCE(old.docstring, ''));
		END
	`); err != nil {
		return fmt.Errorf("failed to create nodes_fts_ad trigger: %w", err)
	}

	// UPDATE trigger
	if _, err := db.Exec(`
		CREATE TRIGGER IF NOT EXISTS nodes_fts_au AFTER UPDATE ON nodes BEGIN
			INSERT INTO nodes_fts(nodes_fts, rowid, name, qualified_name, docstring)
			VALUES ('delete', old.id, old.name, COALESCE(old.qualified_name, ''), COALESCE(old.docstring, ''));
			INSERT INTO nodes_fts(rowid, name, qualified_name, docstring)
			VALUES (new.id, new.name, COALESCE(new.qualified_name, ''), COALESCE(new.docstring, ''));
		END
	`); err != nil {
		return fmt.Errorf("failed to create nodes_fts_au trigger: %w", err)
	}

	// Populate FTS5 index with existing data
	if _, err := db.Exec(`
		INSERT INTO nodes_fts(rowid, name, qualified_name, docstring)
		SELECT id, name, COALESCE(qualified_name, ''), COALESCE(docstring, '')
		FROM nodes
	`); err != nil {
		// Ignore error if table is empty or already populated
		if err.Error() != "UNIQUE constraint failed: nodes_fts.rowid" {
			// Log but don't fail - the index might already be populated
		}
	}

	// Add search-related settings
	searchSettings := []struct {
		key, value, category, description string
	}{
		{"search_mode", "hybrid", "search", "Search mode: keyword, semantic, hybrid"},
		{"search_limit", "10", "search", "Default search result limit"},
		{"search_min_score", "0.2", "search", "Minimum similarity score threshold"},
		{"rrf_k", "60", "search", "RRF fusion parameter k"},
		{"search_index_updated", "false", "search", "Whether FTS5 index has been updated"},
	}

	for _, s := range searchSettings {
		if _, err := db.Exec(`
			INSERT OR IGNORE INTO settings (key, value, category, description)
			VALUES (?, ?, ?, ?)
		`, s.key, s.value, s.category, s.description); err != nil {
			return fmt.Errorf("failed to insert search setting %s: %w", s.key, err)
		}
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '6')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV12 adds unique constraint on edges table to prevent duplicate edges.
func migrateV12(db *sql.DB) error {
	// Create unique index on edges (source_id, target_id, kind)
	// This prevents duplicate edges between the same nodes with the same kind
	if _, err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_edges_unique ON edges(source_id, target_id, kind)
	`); err != nil {
		// Ignore if index already exists or if there are duplicate rows
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "UNIQUE constraint") {
			return fmt.Errorf("failed to create unique index on edges: %w", err)
		}
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '12')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV7 adds dataflow, AST nodes, co-change analysis, and improves constraints.
func migrateV7(db *sql.DB) error {
	// 1. Create dataflow table for data flow analysis
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS dataflow (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id INTEGER NOT NULL,
			target_id INTEGER NOT NULL,
			kind TEXT NOT NULL,
			param_index INTEGER,
			expression TEXT,
			line INTEGER,
			confidence REAL DEFAULT 1.0,
			FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE,
			FOREIGN KEY (target_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create dataflow table: %w", err)
	}

	// Create indexes on dataflow
	dataflowIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_dataflow_source ON dataflow(source_id)",
		"CREATE INDEX IF NOT EXISTS idx_dataflow_target ON dataflow(target_id)",
		"CREATE INDEX IF NOT EXISTS idx_dataflow_kind ON dataflow(kind)",
		"CREATE INDEX IF NOT EXISTS idx_dataflow_source_kind ON dataflow(source_id, kind)",
	}
	for _, idx := range dataflowIndexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create dataflow index: %w", err)
		}
	}

	// 2. Migrate co_changes table if needed (v3 had file1/file2 columns)
	// Check if co_changes table exists and its column structure
	var coChangesColName string
	err := db.QueryRow(`SELECT name FROM pragma_table_info('co_changes') WHERE name IN ('file_a', 'file1') LIMIT 1`).Scan(&coChangesColName)
	if err == sql.ErrNoRows {
		// Table doesn't exist, create it with new schema
		if _, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS co_changes (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				file_a TEXT NOT NULL,
				file_b TEXT NOT NULL,
				commit_count INTEGER NOT NULL,
				jaccard REAL NOT NULL,
				last_commit_epoch INTEGER,
				UNIQUE(file_a, file_b)
			)
		`); err != nil {
			return fmt.Errorf("failed to create co_changes table: %w", err)
		}

		// Create indexes on co_changes
		coChangeIndexes := []string{
			"CREATE INDEX IF NOT EXISTS idx_co_changes_file_a ON co_changes(file_a)",
			"CREATE INDEX IF NOT EXISTS idx_co_changes_file_b ON co_changes(file_b)",
			"CREATE INDEX IF NOT EXISTS idx_co_changes_jaccard ON co_changes(jaccard DESC)",
		}
		for _, idx := range coChangeIndexes {
			if _, err := db.Exec(idx); err != nil {
				return fmt.Errorf("failed to create co_changes index: %w", err)
			}
		}
	} else if err == nil && coChangesColName == "file1" {
		// Old schema exists with file1/file2, need to migrate
		// SQLite doesn't support ALTER TABLE ... RENAME COLUMN in older versions
		// So we recreate the table
		if _, err := db.Exec(`
			DROP TABLE IF EXISTS co_changes_old;
			ALTER TABLE co_changes RENAME TO co_changes_old;
			CREATE TABLE co_changes (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				file_a TEXT NOT NULL,
				file_b TEXT NOT NULL,
				commit_count INTEGER NOT NULL,
				jaccard REAL NOT NULL DEFAULT 0.0,
				last_commit_epoch INTEGER,
				UNIQUE(file_a, file_b)
			);
			INSERT INTO co_changes (file_a, file_b, commit_count, jaccard, last_commit_epoch)
			SELECT file1, file2, co_change_count, 0.0,
				CASE WHEN last_co_change IS NOT NULL THEN strftime('%s', last_co_change) ELSE NULL END
			FROM co_changes_old;
			DROP TABLE co_changes_old;
		`); err != nil {
			// If migration fails, try to continue - the old table still works
			// Just create indexes on the old column names
			coChangeIndexesOld := []string{
				"CREATE INDEX IF NOT EXISTS idx_co_changes_file1 ON co_changes(file1)",
				"CREATE INDEX IF NOT EXISTS idx_co_changes_file2 ON co_changes(file2)",
				"CREATE INDEX IF NOT EXISTS idx_co_changes_count ON co_changes(co_change_count DESC)",
			}
			for _, idx := range coChangeIndexesOld {
				if _, err := db.Exec(idx); err != nil {
					// Ignore errors for existing indexes
					if !strings.Contains(err.Error(), "already exists") {
						return fmt.Errorf("failed to create co_changes index: %w", err)
					}
				}
			}
		} else {
			// Migration successful, create new indexes
			coChangeIndexes := []string{
				"CREATE INDEX IF NOT EXISTS idx_co_changes_file_a ON co_changes(file_a)",
				"CREATE INDEX IF NOT EXISTS idx_co_changes_file_b ON co_changes(file_b)",
				"CREATE INDEX IF NOT EXISTS idx_co_changes_jaccard ON co_changes(jaccard DESC)",
			}
			for _, idx := range coChangeIndexes {
				if _, err := db.Exec(idx); err != nil {
					return fmt.Errorf("failed to create co_changes index: %w", err)
				}
			}
		}
	} else if err != nil {
		return fmt.Errorf("failed to check co_changes table structure: %w", err)
	}

	// 3. Create file_commit_counts table for git history analysis
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS file_commit_counts (
			file TEXT PRIMARY KEY,
			commit_count INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		return fmt.Errorf("failed to create file_commit_counts table: %w", err)
	}

	// 4. Create co_change_meta table for metadata
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS co_change_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create co_change_meta table: %w", err)
	}

	// 5. Enhance CFG tables (add function_node_id index if not exists)
	cfgIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_cfg_blocks_function ON cfg_blocks(node_id)",
		"CREATE INDEX IF NOT EXISTS idx_cfg_edges_function ON cfg_edges(id)",
	}
	for _, idx := range cfgIndexes {
		if _, err := db.Exec(idx); err != nil {
			// Ignore errors for existing indexes
			if !strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("failed to create cfg index: %w", err)
			}
		}
	}

	// 6. Try to add unique constraint on nodes table
	// SQLite doesn't support adding constraints, so we create a unique index instead
	if _, err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_unique ON nodes(name, kind, file, line)
	`); err != nil {
		// Ignore if index already exists or if there are duplicate rows
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "UNIQUE constraint") {
			return fmt.Errorf("failed to create unique index on nodes: %w", err)
		}
	}

	// 7. Add node_metrics table for metrics like fan-in, fan-out, etc.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS node_metrics (
			node_id INTEGER PRIMARY KEY,
			line_count INTEGER,
			symbol_count INTEGER,
			import_count INTEGER,
			export_count INTEGER,
			fan_in INTEGER,
			fan_out INTEGER,
			cohesion REAL,
			file_count INTEGER,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create node_metrics table: %w", err)
	}

	// Create index on node_metrics
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_node_metrics_node ON node_metrics(node_id)
	`); err != nil {
		return fmt.Errorf("failed to create node_metrics index: %w", err)
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '7')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV8 adds project_id to files table for multi-project support.
func migrateV8(db *sql.DB) error {
	// 1. Add project_id column to files table
	if _, err := db.Exec(`ALTER TABLE files ADD COLUMN project_id INTEGER`); err != nil {
		if !isDuplicateColumnError(err) {
			return fmt.Errorf("failed to add project_id to files table: %w", err)
		}
	}

	// 2. Create index on project_id for files table
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_files_project ON files(project_id)`); err != nil {
		return fmt.Errorf("failed to create files project index: %w", err)
	}

	// 3. Drop the old primary key constraint by recreating the table
	// SQLite doesn't support dropping primary key, so we need to recreate
	// Note: We keep the old path as unique for backward compatibility during migration
	// but new inserts will use (path, project_id) as the logical key

	// 4. Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '8')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV10 adds processes and process_steps tables for execution flow materialization.
func migrateV10(db *sql.DB) error {
	// processes: materialized execution flow entry points
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS processes (
			id TEXT PRIMARY KEY,
			label TEXT NOT NULL,
			process_type TEXT NOT NULL CHECK(process_type IN ('intra_community','cross_community','unknown')),
			step_count INTEGER NOT NULL DEFAULT 0,
			entry_point_id INTEGER REFERENCES nodes(id) ON DELETE CASCADE,
			terminal_id INTEGER REFERENCES nodes(id) ON DELETE CASCADE,
			community_ids TEXT NOT NULL DEFAULT '[]',
			project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create processes table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_processes_entry_point ON processes(entry_point_id)
	`); err != nil {
		return fmt.Errorf("failed to create processes entry_point index: %w", err)
	}

	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_processes_project ON processes(project_id)
	`); err != nil {
		return fmt.Errorf("failed to create processes project index: %w", err)
	}

	// process_steps: ordered steps within a process
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS process_steps (
			process_id TEXT NOT NULL REFERENCES processes(id) ON DELETE CASCADE,
			node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			step INTEGER NOT NULL,
			PRIMARY KEY (process_id, step)
		)
	`); err != nil {
		return fmt.Errorf("failed to create process_steps table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_process_steps_node ON process_steps(node_id)
	`); err != nil {
		return fmt.Errorf("failed to create process_steps node index: %w", err)
	}

	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '10')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}
// migrateV11 adds agent_profiles table for custom agent roles.
func migrateV11(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			icon TEXT NOT NULL DEFAULT 'bot',
			tools TEXT NOT NULL DEFAULT '[]',
			system_prompt TEXT NOT NULL DEFAULT '',
			allow_write INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create agent_profiles table: %w", err)
	}

	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '11')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate column name")
}

// migrateV13 adds CCE (Cognitive Context Engine) settings.
// Note: code_chunks and code_embeddings tables are created in project databases
// via migrateProjectV3, since PCE operates on project-specific data.
func migrateV13(db *sql.DB) error {
	// Add PCE settings
	cceSettings := []struct {
		key, value, category, description string
	}{
		{"cce_enabled", "false", "cce", "Enable Cognitive Context Engine"},
		{"cce_mode", "dual", "cce", "PCE embedding mode: description, code, dual"},
		{"cce_default_template", "general", "cce", "Default context template"},
		{"cce_max_context_tokens", "4000", "cce", "Default max tokens for assembled context"},
		{"cce_graph_depth", "1", "cce", "Default graph traversal depth for context retrieval"},
	}

	for _, s := range cceSettings {
		if _, err := db.Exec(`
			INSERT OR IGNORE INTO settings (key, value, category, description)
			VALUES (?, ?, ?, ?)
		`, s.key, s.value, s.category, s.description); err != nil {
			return fmt.Errorf("failed to insert CCE setting %s: %w", s.key, err)
		}
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '13')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV14 adds community_id column to nodes for Louvain community detection.
func migrateV14(db *sql.DB) error {
	// Add community_id column to nodes table (safe: ignore error if column already exists)
	_, _ = db.Exec(`ALTER TABLE nodes ADD COLUMN community_id INTEGER DEFAULT NULL`)

	// Create index for community lookups
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_nodes_community_id ON nodes(community_id)`); err != nil {
		return fmt.Errorf("failed to create community_id index: %w", err)
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '14')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// migrateV15 adds unique constraint on nodes table for correct INSERT OR IGNORE dedup.
// This is critical for BatchInsertNodesWithIDs to work correctly — without it,
// INSERT OR IGNORE cannot detect duplicates and will insert duplicate nodes.
func migrateV15(db *sql.DB) error {
	// Create unique index on nodes (name, kind, file, line)
	if _, err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_unique ON nodes(name, kind, file, line)
	`); err != nil {
		// Ignore if index already exists or if there are duplicate rows
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "UNIQUE constraint") {
			return fmt.Errorf("failed to create unique index on nodes: %w", err)
		}
	}

	// Also add a composite index on edges (source_id, target_id, kind) if not exists
	// (migrateV12 adds this, but ensure it's present for existing DBs)
	if _, err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_edges_unique ON edges(source_id, target_id, kind)
	`); err != nil {
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "UNIQUE constraint") {
			return fmt.Errorf("failed to create unique index on edges: %w", err)
		}
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '15')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

func migrateV9(db *sql.DB) error {
	// arch_rules: stores architecture dependency rules (allow/deny)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS arch_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('deny', 'allow')),
			from_pattern TEXT NOT NULL,
			to_pattern TEXT NOT NULL,
			description TEXT,
			enabled INTEGER NOT NULL DEFAULT 1,
			project_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create arch_rules table: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_arch_rules_project ON arch_rules(project_id)`); err != nil {
		return fmt.Errorf("failed to create arch_rules index: %w", err)
	}

	// Update schema version
	if _, err := db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', '9')
	`); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}