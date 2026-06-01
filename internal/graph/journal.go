// Package graph provides code graph building and analysis capabilities.
package graph

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// JournalFilename is the name of the journal file.
	JournalFilename = "changes.journal"
	// JournalHeaderPrefix is the prefix for the journal header.
	JournalHeaderPrefix = "# axons-journal v1 "
)

// JournalEntry represents a single journal entry.
type JournalEntry struct {
	File    string
	Deleted bool
}

// Journal manages the change journal for Tier 0 incremental detection.
type Journal struct {
	rootDir     string
	journalPath string
	mu          sync.Mutex
}

// NewJournal creates a new Journal instance.
func NewJournal(rootDir string) *Journal {
	journalDir := filepath.Join(rootDir, ".axons")
	return &Journal{
		rootDir:     rootDir,
		journalPath: filepath.Join(journalDir, JournalFilename),
	}
}

// Read reads and validates the change journal.
// Returns valid=true if journal exists and is properly formatted.
func (j *Journal) Read() (*JournalData, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	content, err := os.ReadFile(j.journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &JournalData{Valid: false}, nil
		}
		return nil, fmt.Errorf("failed to read journal: %w", err)
	}

	return j.parseContent(string(content))
}

// parseContent parses the journal file content.
func (j *Journal) parseContent(content string) (*JournalData, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], JournalHeaderPrefix) {
		return &JournalData{Valid: false}, nil
	}

	// Parse timestamp from header
	timestampStr := strings.TrimPrefix(lines[0], JournalHeaderPrefix)
	timestampStr = strings.TrimSpace(timestampStr)
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil || timestamp <= 0 {
		return &JournalData{Valid: false}, nil
	}

	data := &JournalData{
		Valid:     true,
		Timestamp: timestamp,
		Changed:   make([]string, 0),
		Removed:   make([]string, 0),
	}

	seenChanged := make(map[string]bool)
	seenRemoved := make(map[string]bool)

	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "DELETED ") {
			filePath := strings.TrimPrefix(line, "DELETED ")
			filePath = strings.TrimSpace(filePath)
			if filePath != "" && !seenRemoved[filePath] {
				seenRemoved[filePath] = true
				data.Removed = append(data.Removed, filePath)
			}
		} else {
			filePath := strings.TrimSpace(line)
			if filePath != "" && !seenChanged[filePath] {
				seenChanged[filePath] = true
				data.Changed = append(data.Changed, filePath)
			}
		}
	}

	return data, nil
}

// Append appends entries to the journal file.
func (j *Journal) Append(entries []JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	// Ensure journal directory exists
	journalDir := filepath.Dir(j.journalPath)
	if err := os.MkdirAll(journalDir, 0755); err != nil {
		return fmt.Errorf("failed to create journal directory: %w", err)
	}

	// Create journal file with header if it doesn't exist
	if _, err := os.Stat(j.journalPath); os.IsNotExist(err) {
		if err := j.writeHeader(0); err != nil {
			return err
		}
	}

	// Open file for appending
	f, err := os.OpenFile(j.journalPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open journal for appending: %w", err)
	}
	defer f.Close()

	for _, entry := range entries {
		var line string
		if entry.Deleted {
			line = fmt.Sprintf("DELETED %s\n", entry.File)
		} else {
			line = fmt.Sprintf("%s\n", entry.File)
		}
		if _, err := f.WriteString(line); err != nil {
			return fmt.Errorf("failed to write journal entry: %w", err)
		}
	}

	return nil
}

// AppendChange appends a single file change to the journal.
func (j *Journal) AppendChange(file string, deleted bool) error {
	return j.Append([]JournalEntry{{File: file, Deleted: deleted}})
}

// WriteHeader writes a fresh journal header after a successful build.
// Uses atomic write (write to temp file then rename).
func (j *Journal) WriteHeader(timestamp int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.writeHeader(timestamp)
}

// writeHeader is the internal implementation without locking.
func (j *Journal) writeHeader(timestamp int64) error {
	journalDir := filepath.Dir(j.journalPath)
	if err := os.MkdirAll(journalDir, 0755); err != nil {
		return fmt.Errorf("failed to create journal directory: %w", err)
	}

	tmpPath := j.journalPath + ".tmp"
	content := fmt.Sprintf("%s%d\n", JournalHeaderPrefix, timestamp)

	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write journal header: %w", err)
	}

	if err := os.Rename(tmpPath, j.journalPath); err != nil {
		// Clean up temp file if rename failed
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename journal file: %w", err)
	}

	return nil
}

// Clear clears the journal by writing a fresh header.
func (j *Journal) Clear() error {
	return j.WriteHeader(time.Now().Unix())
}

// Exists checks if the journal file exists.
func (j *Journal) Exists() bool {
	_, err := os.Stat(j.journalPath)
	return err == nil
}

// JournalData represents the parsed journal data.
type JournalData struct {
	Valid     bool
	Timestamp int64
	Changed   []string
	Removed   []string
}

// HasChanges returns true if there are any changes in the journal.
func (jd *JournalData) HasChanges() bool {
	return len(jd.Changed) > 0 || len(jd.Removed) > 0
}

// TotalChanges returns the total number of changes.
func (jd *JournalData) TotalChanges() int {
	return len(jd.Changed) + len(jd.Removed)
}

// IsFresh returns true if the journal was created recently (within maxAge).
func (jd *JournalData) IsFresh(maxAge time.Duration) bool {
	if !jd.Valid {
		return false
	}
	journalTime := time.Unix(jd.Timestamp, 0)
	return time.Since(journalTime) <= maxAge
}

// ChangeRecorder provides a buffered way to record changes.
type ChangeRecorder struct {
	journal   *Journal
	entries   []JournalEntry
	maxSize   int
	flushMu   sync.Mutex
	autoFlush bool
}

// NewChangeRecorder creates a new ChangeRecorder.
func NewChangeRecorder(journal *Journal, maxSize int) *ChangeRecorder {
	return &ChangeRecorder{
		journal:   journal,
		entries:   make([]JournalEntry, 0),
		maxSize:   maxSize,
		autoFlush: true,
	}
}

// Record records a file change.
func (cr *ChangeRecorder) Record(file string, deleted bool) error {
	cr.entries = append(cr.entries, JournalEntry{File: file, Deleted: deleted})

	if cr.autoFlush && len(cr.entries) >= cr.maxSize {
		return cr.Flush()
	}
	return nil
}

// Flush flushes all recorded entries to the journal.
func (cr *ChangeRecorder) Flush() error {
	cr.flushMu.Lock()
	defer cr.flushMu.Unlock()

	if len(cr.entries) == 0 {
		return nil
	}

	err := cr.journal.Append(cr.entries)
	cr.entries = cr.entries[:0] // Clear entries
	return err
}

// SetAutoFlush enables or disables auto-flush.
func (cr *ChangeRecorder) SetAutoFlush(enabled bool) {
	cr.autoFlush = enabled
}

// BufferedWriter provides buffered journal writing for high-throughput scenarios.
type BufferedWriter struct {
	journal    *Journal
	buffer     *bufio.Writer
	file       *os.File
	bufferSize int
	mu         sync.Mutex
}

// NewBufferedWriter creates a new BufferedWriter.
func NewBufferedWriter(journal *Journal, bufferSize int) (*BufferedWriter, error) {
	bw := &BufferedWriter{
		journal:    journal,
		bufferSize: bufferSize,
	}

	if err := bw.init(); err != nil {
		return nil, err
	}

	return bw, nil
}

func (bw *BufferedWriter) init() error {
	journalDir := filepath.Dir(bw.journal.journalPath)
	if err := os.MkdirAll(journalDir, 0755); err != nil {
		return fmt.Errorf("failed to create journal directory: %w", err)
	}

	// Create journal file with header if it doesn't exist
	if _, err := os.Stat(bw.journal.journalPath); os.IsNotExist(err) {
		if err := bw.journal.writeHeader(0); err != nil {
			return err
		}
	}

	// Open file for appending
	f, err := os.OpenFile(bw.journal.journalPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open journal: %w", err)
	}

	bw.file = f
	bw.buffer = bufio.NewWriterSize(f, bw.bufferSize)
	return nil
}

// Write writes a single entry to the buffer.
func (bw *BufferedWriter) Write(entry JournalEntry) error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	var line string
	if entry.Deleted {
		line = fmt.Sprintf("DELETED %s\n", entry.File)
	} else {
		line = fmt.Sprintf("%s\n", entry.File)
	}

	_, err := bw.buffer.WriteString(line)
	return err
}

// Flush flushes the buffer to disk.
func (bw *BufferedWriter) Flush() error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	if err := bw.buffer.Flush(); err != nil {
		return err
	}
	return bw.file.Sync()
}

// Close closes the buffered writer.
func (bw *BufferedWriter) Close() error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	if err := bw.buffer.Flush(); err != nil {
		bw.file.Close()
		return err
	}
	return bw.file.Close()
}