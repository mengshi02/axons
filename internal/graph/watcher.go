// Package graph provides code graph building and analysis capabilities.
package graph

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mengshi02/axons/internal/utils"
)

// Watcher watches for file changes and records them to the journal.
type Watcher struct {
	rootDir          string
	journal          *Journal
	recorder         *ChangeRecorder
	fswatcher        *fsnotify.Watcher
	extensions       map[string]bool
	ignoreDirs       map[string]bool
	gitIgnoreMatcher *utils.GitIgnoreMatcher
	debounceMs       int
	pending          map[string]ChangeType
	pendingMu        sync.Mutex
	stopCh           chan struct{}
	stoppedCh        chan struct{}
	onChange         func(path string, changeType ChangeType)
	onChangeMu       sync.Mutex
}

// WatcherConfig holds configuration for the watcher.
type WatcherConfig struct {
	// DebounceMs is the debounce duration in milliseconds.
	DebounceMs int
	// MaxJournalBuffer is the maximum number of entries before auto-flush.
	MaxJournalBuffer int
	// IgnoreDirs is a list of directory names to ignore.
	IgnoreDirs []string
	// OnChange is called when a file change is detected.
	OnChange func(path string, changeType ChangeType)
}

// DefaultWatcherConfig returns the default watcher configuration.
func DefaultWatcherConfig() *WatcherConfig {
	return &WatcherConfig{
		DebounceMs:       300,
		MaxJournalBuffer: 100,
		IgnoreDirs: []string{
			"node_modules", "vendor", ".git", ".svn", ".hg",
			"dist", "build", "target", "bin", "out",
			"__pycache__", ".pytest_cache", ".mypy_cache",
			".idea", ".vscode", ".axons",
			// Decoy directories (portable runtimes, datasets, etc.)
			"python-portable", "node-portable",
			"eval-datasets", "eval-logs",
			"prebuild_binaries", "packbuild",
		},
	}
}

// NewWatcher creates a new file watcher.
func NewWatcher(rootDir string, extensions []string, config *WatcherConfig) (*Watcher, error) {
	if config == nil {
		config = DefaultWatcherConfig()
	}

	extMap := make(map[string]bool)
	for _, ext := range extensions {
		extMap[strings.ToLower(ext)] = true
	}

	ignoreMap := make(map[string]bool)
	for _, dir := range config.IgnoreDirs {
		ignoreMap[dir] = true
	}

	fswatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	journal := NewJournal(rootDir)

	w := &Watcher{
		rootDir:          rootDir,
		journal:          journal,
		recorder:         NewChangeRecorder(journal, config.MaxJournalBuffer),
		fswatcher:        fswatcher,
		extensions:       extMap,
		ignoreDirs:       ignoreMap,
		gitIgnoreMatcher: utils.NewGitIgnoreMatcher(rootDir),
		debounceMs:       config.DebounceMs,
		pending:          make(map[string]ChangeType),
		stopCh:           make(chan struct{}),
		stoppedCh:        make(chan struct{}),
		onChange:         config.OnChange,
	}

	return w, nil
}

// Start starts watching for file changes.
func (w *Watcher) Start(ctx context.Context) error {
	// Add root directory and subdirectories
	if err := w.addWatchDirs(); err != nil {
		return fmt.Errorf("failed to add watch directories: %w", err)
	}

	// Start event processing goroutine
	go w.processEvents(ctx)

	return nil
}

// Stop stops the watcher.
func (w *Watcher) Stop() error {
	close(w.stopCh)

	// Flush any pending changes
	w.pendingMu.Lock()
	if len(w.pending) > 0 {
		entries := make([]JournalEntry, 0, len(w.pending))
		for path, ct := range w.pending {
			entries = append(entries, JournalEntry{
				File:    path,
				Deleted: ct == ChangeDeleted,
			})
		}
		w.journal.Append(entries)
		w.pending = make(map[string]ChangeType)
	}
	w.pendingMu.Unlock()

	// Flush recorder
	w.recorder.Flush()

	return w.fswatcher.Close()
}

// addWatchDirs adds the root directory and all subdirectories to the watcher.
func (w *Watcher) addWatchDirs() error {
	return filepath.WalkDir(w.rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors (e.g. broken symlinks)
		}

		if !d.IsDir() {
			return nil
		}

		// Skip symlinked directories — they may point outside the project or be broken
		if d.Type()&os.ModeSymlink != 0 {
			return fs.SkipDir
		}

		// Check if directory should be ignored
		if w.shouldIgnoreDir(path) {
			return fs.SkipDir
		}

		// Add directory to watcher
		if err := w.fswatcher.Add(path); err != nil {
			// Log but don't fail
			fmt.Printf("Warning: failed to watch directory %s: %v\n", path, err)
		}

		return nil
	})
}

// shouldIgnoreDir checks if a directory should be ignored.
func (w *Watcher) shouldIgnoreDir(path string) bool {
	// Check each component of the path
	relPath, err := filepath.Rel(w.rootDir, path)
	if err != nil {
		return false
	}

	parts := strings.Split(relPath, string(filepath.Separator))
	for _, part := range parts {
		if w.ignoreDirs[part] {
			return true
		}
		// Ignore hidden directories
		if strings.HasPrefix(part, ".") && part != "." {
			return true
		}
	}

	// Check .gitignore rules
	if w.gitIgnoreMatcher != nil && w.gitIgnoreMatcher.Match(path, true) {
		return true
	}

	// Check decoy directory heuristics (catches patterns like *-portable, *-datasets)
	if utils.IsDecoyDir(path) {
		return true
	}

	return false
}

// shouldIgnoreFile checks if a file should be ignored.
// Note: This no longer filters by extension - all non-ignored-directory files
// are passed through so that onChange can decide whether to trigger incremental build.
// The extension filter is applied at the handler level instead.
func (w *Watcher) shouldIgnoreFile(path string) bool {
	// Check if any parent directory should be ignored
	dir := filepath.Dir(path)
	return w.shouldIgnoreDir(dir)
}

// processEvents processes filesystem events.
func (w *Watcher) processEvents(ctx context.Context) {
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	defer debounceTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case event, ok := <-w.fswatcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)

			// Reset debounce timer
			if !debounceTimer.Stop() {
				select {
				case <-debounceTimer.C:
				default:
				}
			}
			debounceTimer.Reset(time.Duration(w.debounceMs) * time.Millisecond)

		case <-debounceTimer.C:
			// Process pending changes
			w.flushPending()

		case err, ok := <-w.fswatcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("Watcher error: %v\n", err)
		}
	}
}

// handleEvent handles a single filesystem event.
func (w *Watcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	// Get relative path
	relPath, err := filepath.Rel(w.rootDir, path)
	if err != nil {
		return
	}

	// Handle directory creation - add to watcher
	if event.Op&fsnotify.Create == fsnotify.Create {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			if !w.shouldIgnoreDir(path) {
				w.fswatcher.Add(path)
			}
			return
		}
	}

	// Check if file should be ignored
	if w.shouldIgnoreFile(path) {
		return
	}

	// Determine change type
	var changeType ChangeType
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		changeType = ChangeAdded
	case event.Op&fsnotify.Write == fsnotify.Write:
		changeType = ChangeModified
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		changeType = ChangeDeleted
	case event.Op&fsnotify.Rename == fsnotify.Rename:
		// Rename is treated as deletion (the new name will trigger a Create)
		changeType = ChangeDeleted
	default:
		return
	}

	w.pendingMu.Lock()
	// If file was already pending as deleted, don't override
	if existing, exists := w.pending[relPath]; exists && existing == ChangeDeleted {
		w.pendingMu.Unlock()
		return
	}
	w.pending[relPath] = changeType
	w.pendingMu.Unlock()
}

// flushPending flushes pending changes to the journal.
func (w *Watcher) flushPending() {
	w.pendingMu.Lock()
	if len(w.pending) == 0 {
		w.pendingMu.Unlock()
		return
	}

	// Copy and clear pending
	pending := w.pending
	w.pending = make(map[string]ChangeType)
	w.pendingMu.Unlock()

	// Record changes
	for path, changeType := range pending {
		if err := w.recorder.Record(path, changeType == ChangeDeleted); err != nil {
			fmt.Printf("Warning: failed to record change for %s: %v\n", path, err)
		}

		// Call onChange callback if set
		w.onChangeMu.Lock()
		onChange := w.onChange
		w.onChangeMu.Unlock()

		if onChange != nil {
			onChange(path, changeType)
		}
	}

	// Flush recorder
	if err := w.recorder.Flush(); err != nil {
		fmt.Printf("Warning: failed to flush journal: %v\n", err)
	}
}

// SetOnChange sets the onChange callback.
func (w *Watcher) SetOnChange(fn func(path string, changeType ChangeType)) {
	w.onChangeMu.Lock()
	defer w.onChangeMu.Unlock()
	w.onChange = fn
}

// GetJournal returns the journal instance.
func (w *Watcher) GetJournal() *Journal {
	return w.journal
}

// WatchProject starts watching a project directory and returns the watcher.
func WatchProject(ctx context.Context, rootDir string, extensions []string, config *WatcherConfig) (*Watcher, error) {
	watcher, err := NewWatcher(rootDir, extensions, config)
	if err != nil {
		return nil, err
	}

	if err := watcher.Start(ctx); err != nil {
		watcher.Stop()
		return nil, err
	}

	return watcher, nil
}

// WatchAndBuild watches for changes and triggers rebuilds.
type WatchAndBuild struct {
	watcher   *Watcher
	builder   *IncrementalBuilder
	debounce  time.Duration
	rebuildCh chan struct{}
	stopCh    chan struct{}
}

// NewWatchAndBuild creates a new WatchAndBuild instance.
func NewWatchAndBuild(rootDir string, extensions []string, config *WatcherConfig) (*WatchAndBuild, error) {
	watcher, err := NewWatcher(rootDir, extensions, config)
	if err != nil {
		return nil, err
	}

	return &WatchAndBuild{
		watcher:   watcher,
		debounce:  time.Duration(config.DebounceMs) * time.Millisecond,
		rebuildCh: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
	}, nil
}

// Start starts watching and building.
func (wb *WatchAndBuild) Start(ctx context.Context, onRebuild func(result *ChangeSet)) error {
	// Set up change callback to trigger rebuild
	wb.watcher.SetOnChange(func(path string, changeType ChangeType) {
		select {
		case wb.rebuildCh <- struct{}{}:
		default:
			// Already a rebuild pending
		}
	})

	// Start watcher
	if err := wb.watcher.Start(ctx); err != nil {
		return err
	}

	// Start rebuild loop
	go wb.rebuildLoop(ctx, onRebuild)

	return nil
}

// rebuildLoop handles debounced rebuilds.
func (wb *WatchAndBuild) rebuildLoop(ctx context.Context, onRebuild func(result *ChangeSet)) {
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	defer debounceTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-wb.stopCh:
			return
		case <-wb.rebuildCh:
			// Reset timer for debounce
			if !debounceTimer.Stop() {
				select {
				case <-debounceTimer.C:
				default:
				}
			}
			debounceTimer.Reset(wb.debounce)

		case <-debounceTimer.C:
			// Read journal changes
			journal := wb.watcher.GetJournal()
			data, err := journal.Read()
			if err != nil {
				fmt.Printf("Error reading journal: %v\n", err)
				continue
			}

			if !data.Valid || !data.HasChanges() {
				continue
			}

			// Build change set
			changeSet := &ChangeSet{
				Added:    make([]*DetectedChange, 0),
				Modified: make([]*DetectedChange, 0),
				Deleted:  make([]*DetectedChange, 0),
				IsFull:   false,
			}

			// Process changed files
			for _, path := range data.Changed {
				fullPath := filepath.Join(wb.watcher.rootDir, path)
				info, err := os.Stat(fullPath)
				if err != nil {
					// File might have been deleted
					changeSet.Deleted = append(changeSet.Deleted, &DetectedChange{
						Path:       path,
						ChangeType: ChangeDeleted,
					})
					continue
				}

				// Check if it's a new file
				if wb.builder != nil {
					dbFiles, _ := wb.builder.getDBFiles()
					if _, exists := dbFiles[path]; !exists {
						changeSet.Added = append(changeSet.Added, &DetectedChange{
							Path:       path,
							ChangeType: ChangeAdded,
							NewModTime: info.ModTime(),
							NewSize:    info.Size(),
						})
					} else {
						changeSet.Modified = append(changeSet.Modified, &DetectedChange{
							Path:       path,
							ChangeType: ChangeModified,
							NewModTime: info.ModTime(),
							NewSize:    info.Size(),
						})
					}
				}
			}

			// Process deleted files
			for _, path := range data.Removed {
				changeSet.Deleted = append(changeSet.Deleted, &DetectedChange{
					Path:       path,
					ChangeType: ChangeDeleted,
				})
			}

			// Clear journal after processing
			journal.Clear()

			// Call rebuild callback
			if onRebuild != nil {
				onRebuild(changeSet)
			}
		}
	}
}

// Stop stops the watcher and builder.
func (wb *WatchAndBuild) Stop() error {
	close(wb.stopCh)
	return wb.watcher.Stop()
}