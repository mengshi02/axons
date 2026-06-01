// Package graph provides code graph building and analysis capabilities.
package graph

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mengshi02/axons/internal/utils"
)

// ChangeType represents the type of file change.
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeModified ChangeType = "modified"
	ChangeDeleted  ChangeType = "deleted"
)

// DetectedChange represents a detected file change with full metadata.
type DetectedChange struct {
	Path       string     `json:"path"`
	ChangeType ChangeType `json:"change_type"`
	OldHash    string     `json:"old_hash,omitempty"`
	NewHash    string     `json:"new_hash,omitempty"`
	OldModTime time.Time  `json:"old_mod_time,omitempty"`
	NewModTime time.Time  `json:"new_mod_time,omitempty"`
	OldSize    int64      `json:"old_size,omitempty"`
	NewSize    int64      `json:"new_size,omitempty"`
	Content    []byte     `json:"content,omitempty"`
}

// IncrementalBuilder handles incremental build detection.
type IncrementalBuilder struct {
	db                *sql.DB
	rootDir           string
	extensions        map[string]bool
	journal           *Journal                  // Tier 0 journal support
	gitIgnoreMatcher  *utils.GitIgnoreMatcher   // Layer 1: .gitignore rules
}

// NewIncrementalBuilder creates a new IncrementalBuilder.
func NewIncrementalBuilder(db *sql.DB, rootDir string, extensions []string) *IncrementalBuilder {
	extMap := make(map[string]bool)
	for _, ext := range extensions {
		extMap[ext] = true
	}
	return &IncrementalBuilder{
		db:               db,
		rootDir:          rootDir,
		extensions:       extMap,
		journal:          NewJournal(rootDir),
		gitIgnoreMatcher: utils.NewGitIgnoreMatcher(rootDir),
	}
}

// DetectChanges detects file changes using three-tier detection.
// Tier 0: Journal-based (fastest, requires watch mode)
// Tier 1: mtime + size (fast, O(n) stats)
// Tier 2: Content hash (accurate, O(changed) reads)
func (ib *IncrementalBuilder) DetectChanges() (*ChangeSet, error) {
	changeSet := &ChangeSet{
		Added:    make([]*DetectedChange, 0),
		Modified: make([]*DetectedChange, 0),
		Deleted:  make([]*DetectedChange, 0),
		IsFull:   false,
	}

	// Tier 0: Check journal for recorded changes (fastest path)
	journalData, err := ib.journal.Read()
	if err == nil && journalData.Valid && journalData.HasChanges() {
		// Process journal entries
		return ib.processJournalData(journalData)
	}

	// Tier 1: mtime + size comparison
	dbFiles, err := ib.getDBFiles()
	if err != nil {
		// No database records, full build needed
		changeSet.IsFull = true
		return changeSet, nil
	}

	fsFiles, err := ib.getFSFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan filesystem: %w", err)
	}

	// Detect deleted files
	for path, info := range dbFiles {
		if _, exists := fsFiles[path]; !exists {
			changeSet.Deleted = append(changeSet.Deleted, &DetectedChange{
				Path:       path,
				ChangeType: ChangeDeleted,
				OldHash:    info.Hash,
				OldModTime: info.ModTime,
				OldSize:    info.Size,
			})
		}
	}

	// Detect added and modified files
	for path, info := range fsFiles {
		dbInfo, exists := dbFiles[path]
		if !exists {
			// New file
			changeSet.Added = append(changeSet.Added, &DetectedChange{
				Path:       path,
				ChangeType: ChangeAdded,
				NewModTime: info.ModTime,
				NewSize:    info.Size,
			})
		} else {
			// Check mtime and size
			if info.ModTime.After(dbInfo.ModTime) || info.Size != dbInfo.Size {
				// Tier 2: Verify with content hash
				content, err := os.ReadFile(filepath.Join(ib.rootDir, path))
				if err != nil {
					continue
				}
				newHash := ib.computeHash(content)
				if newHash != dbInfo.Hash {
					changeSet.Modified = append(changeSet.Modified, &DetectedChange{
						Path:       path,
						ChangeType: ChangeModified,
						OldHash:    dbInfo.Hash,
						NewHash:    newHash,
						OldModTime: dbInfo.ModTime,
						NewModTime: info.ModTime,
						OldSize:    dbInfo.Size,
						NewSize:    info.Size,
						Content:    content,
					})
				}
			}
		}
	}

	return changeSet, nil
}

// ChangeSet represents a set of detected changes.
type ChangeSet struct {
	Added    []*DetectedChange `json:"added"`
	Modified []*DetectedChange `json:"modified"`
	Deleted  []*DetectedChange `json:"deleted"`
	IsFull   bool              `json:"is_full"`
}

// HasChanges returns true if there are any changes.
func (cs *ChangeSet) HasChanges() bool {
	return len(cs.Added) > 0 || len(cs.Modified) > 0 || len(cs.Deleted) > 0
}

// TotalChanges returns the total number of changes.
func (cs *ChangeSet) TotalChanges() int {
	return len(cs.Added) + len(cs.Modified) + len(cs.Deleted)
}

// FileInfo represents file metadata stored in database.
type FileInfo struct {
	Path    string
	Hash    string
	ModTime time.Time
	Size    int64
}

// processJournalData processes changes from JournalData (Tier 0).
func (ib *IncrementalBuilder) processJournalData(journalData *JournalData) (*ChangeSet, error) {
	changeSet := &ChangeSet{
		Added:    make([]*DetectedChange, 0),
		Modified: make([]*DetectedChange, 0),
		Deleted:  make([]*DetectedChange, 0),
		IsFull:   false,
	}

	// Process changed files
	for _, path := range journalData.Changed {
		fullPath := filepath.Join(ib.rootDir, path)
		info, err := os.Stat(fullPath)
		if err != nil {
			// File might have been deleted, skip it
			continue
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		newHash := ib.computeHash(content)

		// Check if it's a new file or modified
		dbFiles, _ := ib.getDBFiles()
		if dbInfo, exists := dbFiles[path]; exists {
			// Modified file
			changeSet.Modified = append(changeSet.Modified, &DetectedChange{
				Path:       path,
				ChangeType: ChangeModified,
				OldHash:    dbInfo.Hash,
				NewHash:    newHash,
				OldModTime: dbInfo.ModTime,
				NewModTime: info.ModTime(),
				OldSize:    dbInfo.Size,
				NewSize:    info.Size(),
				Content:    content,
			})
		} else {
			// New file
			changeSet.Added = append(changeSet.Added, &DetectedChange{
				Path:       path,
				ChangeType: ChangeAdded,
				NewHash:    newHash,
				NewModTime: info.ModTime(),
				NewSize:    info.Size(),
				Content:    content,
			})
		}
	}

	// Process deleted files
	for _, path := range journalData.Removed {
		oldInfo, _ := ib.getFileInfoFromDB(path)
		change := &DetectedChange{
			Path:       path,
			ChangeType: ChangeDeleted,
		}
		if oldInfo != nil {
			change.OldHash = oldInfo.Hash
			change.OldSize = oldInfo.Size
			change.OldModTime = oldInfo.ModTime
		}
		changeSet.Deleted = append(changeSet.Deleted, change)
	}

	// Clear journal after processing
	ib.journal.Clear()

	return changeSet, nil
}

// processJournalChanges processes changes from the journal table (fallback for DB journal).
func (ib *IncrementalBuilder) processJournalChanges(journalChanges []*DetectedChange) (*ChangeSet, error) {
	changeSet := &ChangeSet{
		Added:    make([]*DetectedChange, 0),
		Modified: make([]*DetectedChange, 0),
		Deleted:  make([]*DetectedChange, 0),
		IsFull:   false,
	}

	for _, change := range journalChanges {
		switch change.ChangeType {
		case ChangeAdded, "create":
			// Read file content
			content, err := os.ReadFile(filepath.Join(ib.rootDir, change.Path))
			if err != nil {
				continue
			}
			change.Content = content
			change.NewHash = ib.computeHash(content)
			info, _ := os.Stat(filepath.Join(ib.rootDir, change.Path))
			if info != nil {
				change.NewSize = info.Size()
				change.NewModTime = info.ModTime()
			}
			changeSet.Added = append(changeSet.Added, change)
		case ChangeModified, "update":
			content, err := os.ReadFile(filepath.Join(ib.rootDir, change.Path))
			if err != nil {
				continue
			}
			change.Content = content
			change.NewHash = ib.computeHash(content)
			info, _ := os.Stat(filepath.Join(ib.rootDir, change.Path))
			if info != nil {
				change.NewSize = info.Size()
				change.NewModTime = info.ModTime()
			}
			// Get old hash from database
			oldInfo, _ := ib.getFileInfoFromDB(change.Path)
			if oldInfo != nil {
				change.OldHash = oldInfo.Hash
				change.OldSize = oldInfo.Size
				change.OldModTime = oldInfo.ModTime
			}
			changeSet.Modified = append(changeSet.Modified, change)
		case ChangeDeleted, "remove":
			oldInfo, _ := ib.getFileInfoFromDB(change.Path)
			if oldInfo != nil {
				change.OldHash = oldInfo.Hash
				change.OldSize = oldInfo.Size
				change.OldModTime = oldInfo.ModTime
			}
			changeSet.Deleted = append(changeSet.Deleted, change)
		}
	}

	return changeSet, nil
}

// getDBFiles gets file info from database.
func (ib *IncrementalBuilder) getDBFiles() (map[string]*FileInfo, error) {
	files := make(map[string]*FileInfo)

	rows, err := ib.db.Query(`
		SELECT DISTINCT file, file_hash FROM nodes WHERE file != ''
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			continue
		}

		// Get file stats from files table if available
		var modTime time.Time
		var size int64
		statRow := ib.db.QueryRow(`
			SELECT mod_time, size FROM files WHERE path = ?
		`, path)
		if statRow != nil {
			_ = statRow.Scan(&modTime, &size)
		}

		files[path] = &FileInfo{
			Path:    path,
			Hash:    hash,
			ModTime: modTime,
			Size:    size,
		}
	}

	return files, nil
}

// getFSFiles gets file info from filesystem.
func (ib *IncrementalBuilder) getFSFiles() (map[string]*FileInfo, error) {
	files := make(map[string]*FileInfo)

	err := filepath.Walk(ib.rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			// Layer 1: Skip directories excluded by .gitignore
			if ib.gitIgnoreMatcher != nil && ib.gitIgnoreMatcher.Match(path, true) {
				return filepath.SkipDir
			}
			// Layer 2: Skip decoy directories
			if utils.IsDecoyDir(path) {
				return filepath.SkipDir
			}
			return nil
		}

		// Check extension
		ext := strings.ToLower(filepath.Ext(path))
		if !ib.extensions[ext] {
			return nil
		}

		// Skip hidden files and common ignore patterns
		if ib.shouldIgnore(path) {
			return nil
		}

		relPath, err := filepath.Rel(ib.rootDir, path)
		if err != nil {
			return nil
		}

		files[relPath] = &FileInfo{
			Path:    relPath,
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}
		return nil
	})

	return files, err
}

// getFileInfoFromDB gets file info from database.
func (ib *IncrementalBuilder) getFileInfoFromDB(path string) (*FileInfo, error) {
	info := &FileInfo{Path: path}

	row := ib.db.QueryRow(`
		SELECT file_hash FROM nodes WHERE file = ? LIMIT 1
	`, path)
	if err := row.Scan(&info.Hash); err != nil {
		return nil, err
	}

	statRow := ib.db.QueryRow(`
		SELECT mod_time, size FROM files WHERE path = ?
	`, path)
	if statRow != nil {
		_ = statRow.Scan(&info.ModTime, &info.Size)
	}

	return info, nil
}

// computeHash computes MD5 hash of content.
// Delegates to utils.ComputeMD5.
func (ib *IncrementalBuilder) computeHash(content []byte) string {
	return utils.ComputeMD5(content)
}

// shouldIgnore checks if a file should be ignored.
// Uses .gitignore rules, then falls back to common ignore patterns.
func (ib *IncrementalBuilder) shouldIgnore(path string) bool {
	// Layer 1: Check .gitignore
	if ib.gitIgnoreMatcher != nil && ib.gitIgnoreMatcher.Match(path, false) {
		return true
	}
	// Fallback: common ignore patterns
	return utils.ShouldIgnorePath(path)
}

// RecordChange records a change to the journal.
func (ib *IncrementalBuilder) RecordChange(path string, changeType ChangeType) error {
	_, err := ib.db.Exec(`
		INSERT INTO journal (file_path, event_type, timestamp)
		VALUES (?, ?, ?)
	`, path, string(changeType), time.Now())
	return err
}

// UpdateFileHash updates the file hash in the database.
func (ib *IncrementalBuilder) UpdateFileHash(path string, hash string, size int64) error {
	// Update or insert into files table
	_, err := ib.db.Exec(`
		INSERT OR REPLACE INTO files (path, hash, mod_time, size)
		VALUES (?, ?, ?, ?)
	`, path, hash, time.Now(), size)
	return err
}

// DeleteFileHash removes the file hash from the database.
func (ib *IncrementalBuilder) DeleteFileHash(path string) error {
	_, err := ib.db.Exec("DELETE FROM files WHERE path = ?", path)
	return err
}

// GetChangeSummary returns a human-readable summary of changes.
func (cs *ChangeSet) GetChangeSummary() string {
	if cs.IsFull {
		return "Full build required"
	}

	if !cs.HasChanges() {
		return "No changes detected"
	}

	summary := fmt.Sprintf("%d files changed: ", cs.TotalChanges())
	parts := make([]string, 0)

	if len(cs.Added) > 0 {
		parts = append(parts, fmt.Sprintf("%d added", len(cs.Added)))
	}
	if len(cs.Modified) > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", len(cs.Modified)))
	}
	if len(cs.Deleted) > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", len(cs.Deleted)))
	}

	return summary + strings.Join(parts, ", ")
}

// GetChangedFiles returns all changed file paths.
func (cs *ChangeSet) GetChangedFiles() []string {
	files := make([]string, 0)

	for _, c := range cs.Added {
		files = append(files, c.Path)
	}
	for _, c := range cs.Modified {
		files = append(files, c.Path)
	}
	for _, c := range cs.Deleted {
		files = append(files, c.Path)
	}

	return files
}

// FilterByExtension filters changes by file extension.
func (cs *ChangeSet) FilterByExtension(extensions map[string]bool) *ChangeSet {
	filtered := &ChangeSet{
		Added:    make([]*DetectedChange, 0),
		Modified: make([]*DetectedChange, 0),
		Deleted:  make([]*DetectedChange, 0),
		IsFull:   cs.IsFull,
	}

	for _, c := range cs.Added {
		ext := strings.ToLower(filepath.Ext(c.Path))
		if extensions[ext] {
			filtered.Added = append(filtered.Added, c)
		}
	}
	for _, c := range cs.Modified {
		ext := strings.ToLower(filepath.Ext(c.Path))
		if extensions[ext] {
			filtered.Modified = append(filtered.Modified, c)
		}
	}
	for _, c := range cs.Deleted {
		ext := strings.ToLower(filepath.Ext(c.Path))
		if extensions[ext] {
			filtered.Deleted = append(filtered.Deleted, c)
		}
	}

	return filtered
}
