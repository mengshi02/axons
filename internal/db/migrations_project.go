package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// ProjectSchemaVersion is the current schema version for project databases.
const ProjectSchemaVersion = 4

// MigrateProject runs migrations on a project database ({uuid}.db).
// Project DB contains all code-graph tables without project_id columns.
func MigrateProject(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS metadata (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create project metadata table: %w", err)
	}

	var currentVersion int
	row := db.QueryRow("SELECT CAST(value AS INTEGER) FROM metadata WHERE key = 'project_schema_version'")
	if err := row.Scan(&currentVersion); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check project schema version: %w", err)
	}

	if currentVersion < 1 {
		if err := migrateProjectV1(db); err != nil {
			return err
		}
	}

	if currentVersion < 2 {
		if err := migrateProjectV2(db); err != nil {
			return err
		}
	}

	if currentVersion < 3 {
		if err := migrateProjectV3(db); err != nil {
			return err
		}
	}

	if currentVersion < 4 {
		if err := migrateProjectV4(db); err != nil {
			return err
		}
	}

	if currentVersion < 5 {
		if err := migrateProjectV5(db); err != nil {
			return err
		}
	}

	return nil
}

// migrateProjectV1 creates the full project schema (no project_id columns).
func migrateProjectV1(db *sql.DB) error {
	// nodes table - no project_id
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS nodes (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			name           TEXT NOT NULL,
			kind           TEXT NOT NULL,
			file           TEXT NOT NULL,
			line           INTEGER NOT NULL,
			end_line        INTEGER,
			parent_id       INTEGER,
			exported        INTEGER DEFAULT 0,
			qualified_name  TEXT,
			scope           TEXT,
			visibility      TEXT,
			role            TEXT,
			file_hash       TEXT,
			docstring       TEXT,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create nodes table: %w", err)
	}
	nodeIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_kind ON nodes(kind)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_file ON nodes(file)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_parent_id ON nodes(parent_id)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_qualified_name ON nodes(qualified_name)",
		"CREATE INDEX IF NOT EXISTS idx_nodes_file_hash ON nodes(file_hash)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_unique ON nodes(name, kind, file, line)",
	}
	for _, idx := range nodeIndexes {
		if _, err := db.Exec(idx); err != nil {
			if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "UNIQUE constraint") {
				return fmt.Errorf("failed to create node index: %w", err)
			}
		}
	}

	// edges table - no project_id
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS edges (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id  INTEGER NOT NULL,
			target_id  INTEGER NOT NULL,
			kind       TEXT NOT NULL,
			confidence REAL DEFAULT 1.0,
			dynamic    INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE,
			FOREIGN KEY (target_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create edges table: %w", err)
	}
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

	// files table - no project_id (physically isolated)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			path      TEXT PRIMARY KEY,
			mtime     INTEGER NOT NULL,
			size      INTEGER NOT NULL,
			hash      TEXT NOT NULL,
			parsed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create files table: %w", err)
	}

	// embeddings table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS embeddings (
			node_id      INTEGER PRIMARY KEY,
			embedding    BLOB NOT NULL,
			model        TEXT NOT NULL,
			content_hash TEXT,
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create embeddings table: %w", err)
	}

	// vec_embeddings virtual table
	if _, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_embeddings USING vec0(
			embedding float[1536]
		)
	`); err != nil {
		return fmt.Errorf("failed to create vec_embeddings virtual table: %w", err)
	}

	// FTS5 virtual table
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
	// FTS triggers
	ftsTrigs := []string{
		`CREATE TRIGGER IF NOT EXISTS nodes_fts_ai AFTER INSERT ON nodes BEGIN
			INSERT INTO nodes_fts(rowid, name, qualified_name, docstring)
			VALUES (new.id, new.name, COALESCE(new.qualified_name, ''), COALESCE(new.docstring, ''));
		END`,
		`CREATE TRIGGER IF NOT EXISTS nodes_fts_ad AFTER DELETE ON nodes BEGIN
			INSERT INTO nodes_fts(nodes_fts, rowid, name, qualified_name, docstring)
			VALUES ('delete', old.id, old.name, COALESCE(old.qualified_name, ''), COALESCE(old.docstring, ''));
		END`,
		`CREATE TRIGGER IF NOT EXISTS nodes_fts_au AFTER UPDATE ON nodes BEGIN
			INSERT INTO nodes_fts(nodes_fts, rowid, name, qualified_name, docstring)
			VALUES ('delete', old.id, old.name, COALESCE(old.qualified_name, ''), COALESCE(old.docstring, ''));
			INSERT INTO nodes_fts(rowid, name, qualified_name, docstring)
			VALUES (new.id, new.name, COALESCE(new.qualified_name, ''), COALESCE(new.docstring, ''));
		END`,
	}
	for _, trig := range ftsTrigs {
		if _, err := db.Exec(trig); err != nil {
			return fmt.Errorf("failed to create fts trigger: %w", err)
		}
	}

	// function_complexity table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS function_complexity (
			node_id             INTEGER PRIMARY KEY,
			cyclomatic          INTEGER NOT NULL,
			cognitive           INTEGER NOT NULL,
			nesting             INTEGER NOT NULL,
			lines_of_code       INTEGER NOT NULL,
			halstead_volume     REAL,
			halstead_difficulty REAL,
			halstead_effort     REAL,
			halstead_time       REAL,
			halstead_bugs       REAL,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create function_complexity table: %w", err)
	}

	// cfg_blocks + cfg_edges
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cfg_blocks (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id    INTEGER NOT NULL,
			block_type TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line   INTEGER NOT NULL,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create cfg_blocks table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_cfg_blocks_function ON cfg_blocks(node_id)`); err != nil {
		return fmt.Errorf("failed to create cfg_blocks index: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cfg_edges (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			source_block_id  INTEGER NOT NULL,
			target_block_id  INTEGER NOT NULL,
			edge_type        TEXT NOT NULL,
			FOREIGN KEY (source_block_id) REFERENCES cfg_blocks(id) ON DELETE CASCADE,
			FOREIGN KEY (target_block_id) REFERENCES cfg_blocks(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create cfg_edges table: %w", err)
	}

	// dataflow table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS dataflow (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id   INTEGER NOT NULL,
			target_id   INTEGER NOT NULL,
			kind        TEXT NOT NULL,
			param_index INTEGER,
			expression  TEXT,
			line        INTEGER,
			confidence  REAL DEFAULT 1.0,
			FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE,
			FOREIGN KEY (target_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create dataflow table: %w", err)
	}
	for _, idx := range []string{
		"CREATE INDEX IF NOT EXISTS idx_dataflow_source ON dataflow(source_id)",
		"CREATE INDEX IF NOT EXISTS idx_dataflow_target ON dataflow(target_id)",
		"CREATE INDEX IF NOT EXISTS idx_dataflow_kind ON dataflow(kind)",
		"CREATE INDEX IF NOT EXISTS idx_dataflow_source_kind ON dataflow(source_id, kind)",
	} {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create dataflow index: %w", err)
		}
	}

	// ast_nodes table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS ast_nodes (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			file          TEXT NOT NULL,
			line          INTEGER NOT NULL,
			kind          TEXT NOT NULL,
			name          TEXT,
			text          TEXT,
			receiver      TEXT,
			parent_node_id INTEGER
		)
	`); err != nil {
		return fmt.Errorf("failed to create ast_nodes table: %w", err)
	}

	// co_changes + file_commit_counts + co_change_meta
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS co_changes (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			file_a           TEXT NOT NULL,
			file_b           TEXT NOT NULL,
			commit_count     INTEGER NOT NULL,
			jaccard          REAL NOT NULL,
			last_commit_epoch INTEGER,
			UNIQUE(file_a, file_b)
		)
	`); err != nil {
		return fmt.Errorf("failed to create co_changes table: %w", err)
	}
	for _, idx := range []string{
		"CREATE INDEX IF NOT EXISTS idx_co_changes_file_a ON co_changes(file_a)",
		"CREATE INDEX IF NOT EXISTS idx_co_changes_file_b ON co_changes(file_b)",
		"CREATE INDEX IF NOT EXISTS idx_co_changes_jaccard ON co_changes(jaccard DESC)",
	} {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create co_changes index: %w", err)
		}
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS file_commit_counts (
			file         TEXT PRIMARY KEY,
			commit_count INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		return fmt.Errorf("failed to create file_commit_counts table: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS co_change_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create co_change_meta table: %w", err)
	}

	// node_metrics table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS node_metrics (
			node_id      INTEGER PRIMARY KEY,
			line_count   INTEGER,
			symbol_count INTEGER,
			import_count INTEGER,
			export_count INTEGER,
			fan_in       INTEGER,
			fan_out      INTEGER,
			cohesion     REAL,
			file_count   INTEGER,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create node_metrics table: %w", err)
	}

	// arch_rules table - no project_id (physically isolated per project)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS arch_rules (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			name         TEXT NOT NULL,
			kind         TEXT NOT NULL CHECK(kind IN ('deny', 'allow')),
			from_pattern TEXT NOT NULL,
			to_pattern   TEXT NOT NULL,
			description  TEXT,
			enabled      INTEGER NOT NULL DEFAULT 1,
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create arch_rules table: %w", err)
	}

	// processes + process_steps - no project_id
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS processes (
			id             TEXT PRIMARY KEY,
			label          TEXT NOT NULL,
			process_type   TEXT NOT NULL CHECK(process_type IN ('intra_community','cross_community','unknown')),
			step_count     INTEGER NOT NULL DEFAULT 0,
			entry_point_id INTEGER REFERENCES nodes(id) ON DELETE CASCADE,
			terminal_id    INTEGER REFERENCES nodes(id) ON DELETE CASCADE,
			community_ids  TEXT NOT NULL DEFAULT '[]',
			created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create processes table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_processes_entry_point ON processes(entry_point_id)`); err != nil {
		return fmt.Errorf("failed to create processes index: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS process_steps (
			process_id TEXT    NOT NULL REFERENCES processes(id) ON DELETE CASCADE,
			node_id    INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
			step       INTEGER NOT NULL,
			PRIMARY KEY (process_id, step)
		)
	`); err != nil {
		return fmt.Errorf("failed to create process_steps table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_process_steps_node ON process_steps(node_id)`); err != nil {
		return fmt.Errorf("failed to create process_steps index: %w", err)
	}

	// change_history table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS change_history (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path   TEXT NOT NULL,
			change_type TEXT NOT NULL,
			change_time DATETIME DEFAULT CURRENT_TIMESTAMP,
			processed   INTEGER DEFAULT 0
		)
	`); err != nil {
		return fmt.Errorf("failed to create change_history table: %w", err)
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('project_schema_version', '1')`); err != nil {
		return fmt.Errorf("failed to update project schema version: %w", err)
	}
	return nil
}

// migrateProjectV2 adds unique constraint on edges table to prevent duplicate edges.
func migrateProjectV2(db *sql.DB) error {
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

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('project_schema_version', '2')`); err != nil {
		return fmt.Errorf("failed to update project schema version: %w", err)
	}
	return nil
}

// migrateProjectV3 adds CCE (Cognitive Context Engine) tables for bimodal embeddings.
func migrateProjectV3(db *sql.DB) error {
	// code_chunks: stores extracted source code snippets for code-mode embeddings
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS code_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id INTEGER NOT NULL UNIQUE,
			file TEXT NOT NULL,
			start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL,
			content TEXT NOT NULL,
			language TEXT NOT NULL DEFAULT '',
			content_hash TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create code_chunks table: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_code_chunks_node ON code_chunks(node_id)`); err != nil {
		return fmt.Errorf("failed to create code_chunks node index: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_code_chunks_file ON code_chunks(file)`); err != nil {
		return fmt.Errorf("failed to create code_chunks file index: %w", err)
	}

	// code_embeddings: stores code-mode embedding metadata
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS code_embeddings (
			node_id INTEGER PRIMARY KEY,
			embedding BLOB NOT NULL,
			model TEXT NOT NULL,
			text TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create code_embeddings table: %w", err)
	}

	// vec_code_embeddings will be created lazily on first code embedding batch,
	// similar to how vec_embeddings is handled. The dimension is determined by
	// the embedding model at runtime.

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('project_schema_version', '3')`); err != nil {
		return fmt.Errorf("failed to update project schema version: %w", err)
	}
	return nil
}

// migrateProjectV4 updates code_embeddings to support multi-chunk embeddings.
// Previously, each node could only have one code embedding (node_id PRIMARY KEY).
// Now, each node can have multiple chunks, identified by (node_id, chunk_index).
// The vec_code_embeddings table is recreated with an auto-increment rowid
// instead of using node_id directly.
func migrateProjectV4(db *sql.DB) error {
	// Drop vec_code_embeddings if it exists (will be recreated lazily on next embed)
	db.Exec(`DROP TABLE IF EXISTS vec_code_embeddings`)

	// Recreate code_embeddings with (node_id, chunk_index) as composite primary key.
	// We need to recreate the table since SQLite doesn't support ALTER PRIMARY KEY.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS code_embeddings_new (
			node_id INTEGER NOT NULL,
			chunk_index INTEGER NOT NULL DEFAULT 0,
			embedding BLOB NOT NULL,
			model TEXT NOT NULL,
			text TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (node_id, chunk_index),
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("failed to create code_embeddings_new table: %w", err)
	}

	// Copy existing data (all existing rows get chunk_index = 0)
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO code_embeddings_new (node_id, chunk_index, embedding, model, text, created_at)
		SELECT node_id, 0, embedding, model, text, created_at FROM code_embeddings
	`); err != nil {
		return fmt.Errorf("failed to copy code_embeddings data: %w", err)
	}

	// Swap tables
	if _, err := db.Exec(`DROP TABLE code_embeddings`); err != nil {
		return fmt.Errorf("failed to drop old code_embeddings table: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE code_embeddings_new RENAME TO code_embeddings`); err != nil {
		return fmt.Errorf("failed to rename code_embeddings_new: %w", err)
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('project_schema_version', '4')`); err != nil {
		return fmt.Errorf("failed to update project schema version: %w", err)
	}
	return nil
}

// migrateProjectV5 adds community_id column to nodes for Louvain community detection.
func migrateProjectV5(db *sql.DB) error {
	// Add community_id column to nodes table (if not exists — SQLite doesn't support IF NOT EXISTS for ALTER TABLE)
	// Use a safe approach: try to add, ignore error if column already exists
	_, _ = db.Exec(`ALTER TABLE nodes ADD COLUMN community_id INTEGER DEFAULT NULL`)

	// Create index for community lookups
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_nodes_community_id ON nodes(community_id)`); err != nil {
		return fmt.Errorf("failed to create community_id index: %w", err)
	}

	if _, err := db.Exec(`INSERT OR REPLACE INTO metadata (key, value) VALUES ('project_schema_version', '5')`); err != nil {
		return fmt.Errorf("failed to update project schema version: %w", err)
	}
	return nil
}