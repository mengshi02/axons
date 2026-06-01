// Package service provides business logic services.
package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mengshi02/axons/internal/logger"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// FileChangeRecord represents a file change record from database
type FileChangeRecord struct {
	ID          int64
	SessionID   string
	ProjectID   string
	FilePath    string
	ChangeType  string // "create" or "modify"
	ContentHash string
	CreatedAt   time.Time
}

// DiffLine represents a single diff line
type DiffLine struct {
	Type string `json:"type"` // "equal", "insert", "delete"
	Text string `json:"text"`
}

// DiffStats represents diff statistics
type DiffStats struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// DiffResult represents the result of a diff operation
type DiffResult struct {
	FilePath        string     `json:"file_path"`
	ChangeType      string     `json:"change_type"`
	OriginalContent string     `json:"original_content"`
	CurrentContent  string     `json:"current_content"`
	Diff            []DiffLine `json:"diff"`
	Stats           DiffStats  `json:"stats"`
}

// SessionManifest represents the manifest file for a session
type SessionManifest struct {
	SessionID string        `json:"session_id"`
	ProjectID string        `json:"project_id"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Changes   []ChangeEntry `json:"changes"`
}

// ChangeEntry represents a single change entry in manifest
type ChangeEntry struct {
	FilePath    string    `json:"file_path"`
	ChangeType  string    `json:"change_type"`
	ContentHash string    `json:"content_hash"`
	Timestamp   time.Time `json:"timestamp"`
}

// BackupService manages file backups for AI modifications
type BackupService struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewBackupService creates a new BackupService
func NewBackupService(db *sql.DB) *BackupService {
	return &BackupService{db: db}
}

// Backup creates a backup of a file before modification
// This should be called BEFORE the file is written
func (s *BackupService) Backup(projectRoot, projectID, sessionID, filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Handle both absolute and relative paths
	// Normalize to relative path for storage
	var absPath string
	var relativePath string
	if filepath.IsAbs(filePath) {
		absPath = filePath
		// Convert absolute path to relative path for storage
		rel, err := filepath.Rel(projectRoot, filePath)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		relativePath = rel
	} else {
		absPath = filepath.Join(projectRoot, filePath)
		relativePath = filePath
	}
	backupDir := filepath.Join(projectRoot, ".axons", "backups")

	// Check if already backed up in this session
	var exists int
	err := s.db.QueryRow(
		"SELECT 1 FROM file_changes WHERE session_id = ? AND file_path = ?",
		sessionID, relativePath,
	).Scan(&exists)
	if err == nil {
		// Already backed up
		logger.S().Debugw("[BackupService] Already backed up", "session_id", sessionID, "path", relativePath)
		return nil
	}

	// Read current file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, this is a new file creation
			logger.S().Infow("[BackupService] New file creation", "session_id", sessionID, "path", relativePath)
			return s.recordCreate(projectRoot, projectID, sessionID, relativePath)
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Calculate content hash
	hash := sha256Hash(content)

	// Write content snapshot (if not exists)
	contentPath := filepath.Join(backupDir, "contents", hash)
	if _, err := os.Stat(contentPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(contentPath), 0755); err != nil {
			return fmt.Errorf("failed to create contents directory: %w", err)
		}
		if err := os.WriteFile(contentPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write content snapshot: %w", err)
		}
		logger.S().Infow("[BackupService] Created content snapshot", "hash", hash, "size", len(content))
	}

	// Record in database and manifest
	return s.recordModify(projectRoot, projectID, sessionID, relativePath, hash)
}

// recordCreate records a new file creation (no original content)
func (s *BackupService) recordCreate(projectRoot, projectID, sessionID, filePath string) error {
	now := time.Now()

	// Insert into database
	_, err := s.db.Exec(`
		INSERT INTO file_changes (session_id, project_id, file_path, change_type, content_hash, created_at)
		VALUES (?, ?, ?, 'create', NULL, ?)
	`, sessionID, projectID, filePath, now)
	if err != nil {
		return fmt.Errorf("failed to insert file_changes record: %w", err)
	}

	// Update manifest
	return s.updateManifest(projectRoot, projectID, sessionID, ChangeEntry{
		FilePath:   filePath,
		ChangeType: "create",
		Timestamp:  now,
	})
}

// recordModify records a file modification with original content hash
func (s *BackupService) recordModify(projectRoot, projectID, sessionID, filePath, contentHash string) error {
	now := time.Now()

	// Insert into database
	_, err := s.db.Exec(`
		INSERT INTO file_changes (session_id, project_id, file_path, change_type, content_hash, created_at)
		VALUES (?, ?, ?, 'modify', ?, ?)
	`, sessionID, projectID, filePath, contentHash, now)
	if err != nil {
		return fmt.Errorf("failed to insert file_changes record: %w", err)
	}

	// Update manifest
	return s.updateManifest(projectRoot, projectID, sessionID, ChangeEntry{
		FilePath:    filePath,
		ChangeType:  "modify",
		ContentHash: contentHash,
		Timestamp:   now,
	})
}

// updateManifest updates the session manifest file
func (s *BackupService) updateManifest(projectRoot, projectID, sessionID string, entry ChangeEntry) error {
	backupDir := filepath.Join(projectRoot, ".axons", "backups")
	manifestPath := filepath.Join(backupDir, "sessions", sessionID+".json")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return err
	}

	// Read existing manifest or create new
	manifest := SessionManifest{
		SessionID: sessionID,
		ProjectID: projectID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if data, err := os.ReadFile(manifestPath); err == nil {
		json.Unmarshal(data, &manifest)
	}

	// Add new entry
	manifest.Changes = append(manifest.Changes, entry)
	manifest.UpdatedAt = time.Now()

	// Write manifest
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(manifestPath, data, 0644)
}

// ListChanges returns all changes for a session
func (s *BackupService) ListChanges(sessionID string) ([]FileChangeRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, project_id, file_path, change_type, content_hash, created_at
		FROM file_changes
		WHERE session_id = ?
		ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []FileChangeRecord
	for rows.Next() {
		var c FileChangeRecord
		var contentHash sql.NullString
		if err := rows.Scan(&c.ID, &c.SessionID, &c.ProjectID, &c.FilePath, &c.ChangeType, &contentHash, &c.CreatedAt); err != nil {
			return nil, err
		}
		if contentHash.Valid {
			c.ContentHash = contentHash.String
		}
		changes = append(changes, c)
	}

	return changes, nil
}

// GetChange returns a single change record
func (s *BackupService) GetChange(sessionID, filePath string) (*FileChangeRecord, error) {
	var c FileChangeRecord
	var contentHash sql.NullString
	err := s.db.QueryRow(`
		SELECT id, session_id, project_id, file_path, change_type, content_hash, created_at
		FROM file_changes
		WHERE session_id = ? AND file_path = ?
	`, sessionID, filePath).Scan(&c.ID, &c.SessionID, &c.ProjectID, &c.FilePath, &c.ChangeType, &contentHash, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no change record found for session=%s path=%s", sessionID, filePath)
	}
	if err != nil {
		return nil, err
	}
	if contentHash.Valid {
		c.ContentHash = contentHash.String
	}
	return &c, nil
}

// GetDiff returns the diff for a file
func (s *BackupService) GetDiff(projectRoot, sessionID, filePath string) (*DiffResult, error) {
	// Normalize filePath to relative path for database lookup
	var relativePath string
	var absPath string
	if filepath.IsAbs(filePath) {
		absPath = filePath
		rel, err := filepath.Rel(projectRoot, filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to get relative path: %w", err)
		}
		relativePath = rel
	} else {
		relativePath = filePath
		absPath = filepath.Join(projectRoot, filePath)
	}

	change, err := s.GetChange(sessionID, relativePath)
	if err != nil {
		return nil, err
	}

	// Read current content
	currentContent, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			currentContent = []byte{}
		} else {
			return nil, fmt.Errorf("failed to read current file: %w", err)
		}
	}

	// Read original content from snapshot
	var originalContent []byte
	if change.ContentHash != "" {
		contentPath := filepath.Join(projectRoot, ".axons", "backups", "contents", change.ContentHash)
		originalContent, err = os.ReadFile(contentPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read original content: %w", err)
		}
	}

	// Compute diff
	diff := computeDiff(string(originalContent), string(currentContent))
	
	logger.S().Debugw("[GetDiff] Computed diff",
		"file_path", filePath,
		"original_len", len(originalContent),
		"current_len", len(currentContent),
		"diff_lines", len(diff.Diff),
		"stats", diff.Stats,
	)

	return &DiffResult{
		FilePath:        filePath,
		ChangeType:      change.ChangeType,
		OriginalContent: string(originalContent),
		CurrentContent:  string(currentContent),
		Diff:            diff.Diff,
		Stats:           diff.Stats,
	}, nil
}

// Revert reverts a single file to its original state
func (s *BackupService) Revert(projectRoot, sessionID, filePath string) error {
	// Normalize filePath to relative path for database lookup
	var relativePath string
	var absPath string
	if filepath.IsAbs(filePath) {
		absPath = filePath
		rel, err := filepath.Rel(projectRoot, filePath)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		relativePath = rel
	} else {
		relativePath = filePath
		absPath = filepath.Join(projectRoot, filePath)
	}

	change, err := s.GetChange(sessionID, relativePath)
	if err != nil {
		return err
	}

	if change.ChangeType == "create" {
		// Delete the newly created file
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete created file: %w", err)
		}
		logger.S().Infow("[BackupService] Deleted created file", "path", filePath)
	} else {
		// Restore from snapshot
		contentPath := filepath.Join(projectRoot, ".axons", "backups", "contents", change.ContentHash)
		content, err := os.ReadFile(contentPath)
		if err != nil {
			return fmt.Errorf("failed to read snapshot: %w", err)
		}

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		if err := os.WriteFile(absPath, content, 0644); err != nil {
			return fmt.Errorf("failed to restore file: %w", err)
		}
		logger.S().Infow("[BackupService] Restored file", "path", filePath)
	}

	// Remove the change record
	return s.removeChangeRecord(projectRoot, sessionID, relativePath)
}

// RevertAll reverts all changes in a session
func (s *BackupService) RevertAll(projectRoot, sessionID string) error {
	changes, err := s.ListChanges(sessionID)
	if err != nil {
		return err
	}

	var lastErr error
	for _, c := range changes {
		if err := s.Revert(projectRoot, sessionID, c.FilePath); err != nil {
			lastErr = err
			logger.S().Errorw("[BackupService] Failed to revert", "path", c.FilePath, "error", err)
		}
	}

	return lastErr
}

// ClearSession clears all backup records for a session (user confirms keeping changes)
func (s *BackupService) ClearSession(projectRoot, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	changes, err := s.ListChanges(sessionID)
	if err != nil {
		return err
	}

	// Try to delete content snapshots that are no longer referenced
	for _, c := range changes {
		if c.ContentHash != "" {
			s.tryDeleteContent(projectRoot, c.ContentHash)
		}
	}

	// Delete manifest file
	manifestPath := filepath.Join(projectRoot, ".axons", "backups", "sessions", sessionID+".json")
	os.Remove(manifestPath)

	// Delete database records
	_, err = s.db.Exec("DELETE FROM file_changes WHERE session_id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete file_changes records: %w", err)
	}

	logger.S().Infow("[BackupService] Cleared session", "session_id", sessionID, "changes_count", len(changes))
	return nil
}

// removeChangeRecord removes a single change record
func (s *BackupService) removeChangeRecord(projectRoot, sessionID, filePath string) error {
	// Get the change to find content_hash
	change, err := s.GetChange(sessionID, filePath)
	if err != nil {
		return err
	}

	// Delete from database
	_, err = s.db.Exec("DELETE FROM file_changes WHERE session_id = ? AND file_path = ?", sessionID, filePath)
	if err != nil {
		return err
	}

	// Try to delete content snapshot if no longer referenced
	if change.ContentHash != "" {
		s.tryDeleteContent(projectRoot, change.ContentHash)
	}

	// Update manifest
	manifestPath := filepath.Join(projectRoot, ".axons", "backups", "sessions", sessionID+".json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		var manifest SessionManifest
		if json.Unmarshal(data, &manifest) == nil {
			var newChanges []ChangeEntry
			for _, c := range manifest.Changes {
				if c.FilePath != filePath {
					newChanges = append(newChanges, c)
				}
			}
			manifest.Changes = newChanges
			manifest.UpdatedAt = time.Now()
			if newData, err := json.MarshalIndent(manifest, "", "  "); err == nil {
				os.WriteFile(manifestPath, newData, 0644)
			}
		}
	}

	return nil
}

// tryDeleteContent tries to delete a content snapshot if no longer referenced
func (s *BackupService) tryDeleteContent(projectRoot, contentHash string) {
	var count int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM file_changes WHERE content_hash = ?",
		contentHash,
	).Scan(&count)
	if err != nil {
		return
	}

	if count == 0 {
		contentPath := filepath.Join(projectRoot, ".axons", "backups", "contents", contentHash)
		os.Remove(contentPath)
		logger.S().Debugw("[BackupService] Deleted unreferenced content snapshot", "hash", contentHash)
	}
}

// GetProjectID returns the project_id for a session
func (s *BackupService) GetProjectID(sessionID string) (string, error) {
	var projectID string
	err := s.db.QueryRow(
		"SELECT project_id FROM file_changes WHERE session_id = ? LIMIT 1",
		sessionID,
	).Scan(&projectID)
	if err != nil {
		return "", err
	}
	return projectID, nil
}

// CleanExpiredSessions cleans up sessions older than retentionDays
func (s *BackupService) CleanExpiredSessions(projectRoot string, retentionDays int) (int, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	rows, err := s.db.Query(`
		SELECT DISTINCT session_id, project_id FROM file_changes
		WHERE created_at < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var sessions []struct {
		SessionID string
		ProjectID string
	}
	for rows.Next() {
		var sID, pID string
		if err := rows.Scan(&sID, &pID); err != nil {
			continue
		}
		sessions = append(sessions, struct {
			SessionID string
			ProjectID string
		}{sID, pID})
	}

	count := 0
	for _, sess := range sessions {
		if err := s.ClearSession(projectRoot, sess.SessionID); err != nil {
			logger.S().Warnw("[BackupService] Failed to clean expired session", "session_id", sess.SessionID, "error", err)
		} else {
			count++
		}
	}

	return count, nil
}

// computeDiff computes diff between two strings at line level
func computeDiff(oldContent, newContent string) *DiffResult {
	logger.S().Debugw("[computeDiff] Starting diff computation",
		"old_content_len", len(oldContent),
		"new_content_len", len(newContent),
	)

	// Split content into lines
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Remove trailing empty line if content ends with newline
	if len(oldLines) > 0 && oldLines[len(oldLines)-1] == "" {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}

	logger.S().Debugw("[computeDiff] Line counts",
		"old_lines", len(oldLines),
		"new_lines", len(newLines),
	)

	// Use diffmatchpatch with line-level diff
	// Convert lines to unique tokens for diffmatchpatch
	dmp := diffmatchpatch.New()
	
	// Create a mapping from line to unique rune
	lineToRune := make(map[string]rune)
	nextRune := rune(0x10000) // Start from a high Unicode value to avoid conflicts
	
	// Encode old lines
	oldRunes := make([]rune, len(oldLines))
	for i, line := range oldLines {
		if r, exists := lineToRune[line]; exists {
			oldRunes[i] = r
		} else {
			lineToRune[line] = nextRune
			oldRunes[i] = nextRune
			nextRune++
		}
	}
	
	// Encode new lines
	newRunes := make([]rune, len(newLines))
	for i, line := range newLines {
		if r, exists := lineToRune[line]; exists {
			newRunes[i] = r
		} else {
			lineToRune[line] = nextRune
			newRunes[i] = nextRune
			nextRune++
		}
	}
	
	// Create reverse mapping
	runeToLine := make(map[rune]string)
	for line, r := range lineToRune {
		runeToLine[r] = line
	}
	
	// Compute diff on encoded runes
	diffs := dmp.DiffMain(string(oldRunes), string(newRunes), true)
	dmp.DiffCleanupSemantic(diffs)

	logger.S().Debugw("[computeDiff] Diff computed", "diff_count", len(diffs))

	// Convert back to lines
	var added, removed int
	resultLines := make([]DiffLine, 0)

	for _, d := range diffs {
		// Each rune in the diff represents a line
		for _, r := range d.Text {
			line := runeToLine[r]
			resultLines = append(resultLines, DiffLine{
				Type: diffTypeToString(d.Type),
				Text: line,
			})

			if d.Type == diffmatchpatch.DiffInsert {
				added++
			} else if d.Type == diffmatchpatch.DiffDelete {
				removed++
			}
		}
	}

	logger.S().Debugw("[computeDiff] Result", "result_lines", len(resultLines), "added", added, "removed", removed)

	return &DiffResult{
		FilePath:        "",
		ChangeType:      "",
		OriginalContent: "",
		CurrentContent:  "",
		Diff:            resultLines,
		Stats:           DiffStats{Added: added, Removed: removed},
	}
}

func diffTypeToString(t diffmatchpatch.Operation) string {
	switch t {
	case diffmatchpatch.DiffInsert:
		return "insert"
	case diffmatchpatch.DiffDelete:
		return "delete"
	default:
		return "equal"
	}
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}