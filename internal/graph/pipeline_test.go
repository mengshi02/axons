package graph

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
	_ "modernc.org/sqlite"
)

// getTestdataPath returns the path to the testdata directory
func getTestdataPath() string {
	_, filename, _, _ := runtime.Caller(0)
	// Go up one level from internal/graph to internal, then into extractors/testdata
	return filepath.Join(filepath.Dir(filename), "..", "extractors", "testdata")
}

func TestPipelineParsing(t *testing.T) {
	// Use the testdata directory instead of hard-coded path
	testdataPath := getTestdataPath()

	// Check if testdata exists
	if _, err := os.Stat(testdataPath); os.IsNotExist(err) {
		t.Skipf("Testdata path does not exist: %s", testdataPath)
	}

	// Create a temporary database
	dbPath := "/tmp/test_pipeline_" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".db"
	defer os.Remove(dbPath)

	// Use the actual axons database driver
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Skipf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Initialize the database schema (mimicking what axons does)
	schema := `
	CREATE TABLE IF NOT EXISTS nodes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER,
		name TEXT,
		kind TEXT,
		file TEXT,
		line INTEGER,
		end_line INTEGER,
		qualified_name TEXT,
		exported INTEGER,
		visibility TEXT,
		scope TEXT,
		file_hash TEXT,
		created_at INTEGER DEFAULT (strftime('%s', 'now'))
	);
	CREATE INDEX IF NOT EXISTS idx_nodes_project_file ON nodes(project_id, file);
	CREATE INDEX IF NOT EXISTS idx_nodes_qualified ON nodes(project_id, qualified_name);
	
	CREATE TABLE IF NOT EXISTS edges (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER,
		source_id INTEGER,
		target_id INTEGER,
		kind TEXT,
		confidence REAL,
		dynamic INTEGER,
		created_at INTEGER DEFAULT (strftime('%s', 'now'))
	);
	CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
	CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
	
	CREATE TABLE IF NOT EXISTS file_hashes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id INTEGER,
		path TEXT,
		hash TEXT,
		size INTEGER,
		mtime INTEGER,
		created_at INTEGER DEFAULT (strftime('%s', 'now')),
		UNIQUE(project_id, path)
	);
	
	CREATE TABLE IF NOT EXISTS projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		root_dir TEXT,
		created_at INTEGER DEFAULT (strftime('%s', 'now'))
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Skipf("Failed to create schema: %v", err)
	}

	// Create repository
	repo := repository.New(db)

	// Create build options
	opts := &types.BuildOptions{
		RootDir:   testdataPath,
		FullBuild: true,
	}

	// Create and run pipeline (same as what API does)
	pipeline := NewPipeline(repo, opts)

	ctx := context.Background()

	// Run multiple times to try to trigger panic
	var panicCount int
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					panicCount++
					mu.Unlock()
					t.Errorf("Run %d: Pipeline panic recovered: %v", i, r)
				}
			}()

			result, err := pipeline.Build(ctx)
			if err != nil {
				t.Logf("Run %d: Pipeline error: %v", i, err)
			} else if result != nil {
				t.Logf("Run %d: Parsed %d files, created %d nodes, %d edges",
					i, result.FilesParsed, result.NodesCreated, result.EdgesCreated)
			}
		}()
	}

	t.Logf("Pipeline test completed, panic count: %d", panicCount)
	if panicCount > 0 {
		t.Errorf("Found %d panics during pipeline execution", panicCount)
	}
}