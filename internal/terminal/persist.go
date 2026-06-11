// Package terminal provides PTY-based terminal sessions for web terminal feature.
// persist.go implements session persistence to disk for cross-restart Revive,
// aligning with IDE's serializeTerminalState/reviveTerminalProcesses.
package terminal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ReviveProcessMode controls when terminal sessions are revived on restart.
// Aligns with IDE terminal.integrated.persistentSessionReviveProcess.
type ReviveProcessMode string

const (
	ReviveOnExit              ReviveProcessMode = "onExit"                // Only revive on backend exit (default, IDE default)
	ReviveOnExitAndWindowClose ReviveProcessMode = "onExitAndWindowClose" // Also revive on window close
	ReviveNever               ReviveProcessMode = "never"                // Never revive
)

// ShellLaunchConfig stores the shell configuration for session revival.
// Aligns with IDE IShellLaunchConfig.
type ShellLaunchConfig struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
	Env        []string `json:"env,omitempty"`
	Cwd        string   `json:"cwd"`
}

// ReplayEventEntry represents one event in a serialized replay.
// Aligns with IDE IPersistedTerminalState.replayEvent.events.
type ReplayEventEntry struct {
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Data string `json:"data"` // xterm-compatible ANSI sequence stream
}

// ReplayEvent represents the full replay data for a session.
type ReplayEvent struct {
	Events   []ReplayEventEntry `json:"events"`
	Commands []SerializedCommand `json:"commands,omitempty"`
}

// SessionSnapshot holds the full serialized state of a terminal session.
// Aligns with IDE ISerializedTerminalState.
type SessionSnapshot struct {
	ID                string            `json:"id"`
	ShellLaunchConfig ShellLaunchConfig `json:"shellLaunchConfig"`
	ReplayEvent       ReplayEvent       `json:"replayEvent"`
	Timestamp         int64             `json:"timestamp"` // Unix ms
	Source            string            `json:"source"`    // "serialize" | "ringbuffer"
	UnicodeVersion    string            `json:"unicodeVersion,omitempty"`
}

// PersistDir returns the default directory for terminal session snapshots.
func PersistDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".axons", "terminal-snapshots")
}

// PersistManager manages disk persistence of terminal session snapshots.
type PersistManager struct {
	dir string
	mu  sync.Mutex
}

// NewPersistManager creates a new persistence manager.
func NewPersistManager(dir string) *PersistManager {
	if dir == "" {
		dir = PersistDir()
	}
	return &PersistManager{dir: dir}
}

// Dir returns the snapshot directory path.
func (pm *PersistManager) Dir() string {
	return pm.dir
}

// EnsureDir creates the snapshot directory if it doesn't exist.
func (pm *PersistManager) EnsureDir() error {
	return os.MkdirAll(pm.dir, 0700)
}

// WriteSnapshot writes a session snapshot to disk.
func (pm *PersistManager) WriteSnapshot(snap *SessionSnapshot) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := pm.EnsureDir(); err != nil {
		return fmt.Errorf("persist: failed to create dir: %w", err)
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("persist: failed to marshal snapshot: %w", err)
	}

	path := pm.snapshotPath(snap.ID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("persist: failed to write snapshot: %w", err)
	}

	zap.L().Debug("Persisted session snapshot",
		zap.String("id", snap.ID),
		zap.String("source", snap.Source),
		zap.Int("bytes", len(data)))

	return nil
}

// ReadSnapshot reads a session snapshot from disk.
func (pm *PersistManager) ReadSnapshot(id string) (*SessionSnapshot, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	path := pm.snapshotPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No snapshot is not an error
		}
		return nil, fmt.Errorf("persist: failed to read snapshot: %w", err)
	}

	var snap SessionSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("persist: failed to unmarshal snapshot: %w", err)
	}

	return &snap, nil
}

// ReadAllSnapshots reads all session snapshots from disk.
func (pm *PersistManager) ReadAllSnapshots() ([]*SessionSnapshot, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if err := pm.EnsureDir(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(pm.dir)
	if err != nil {
		return nil, fmt.Errorf("persist: failed to read dir: %w", err)
	}

	var snapshots []*SessionSnapshot
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(pm.dir, entry.Name()))
		if err != nil {
			zap.L().Warn("persist: failed to read snapshot file",
				zap.String("file", entry.Name()), zap.Error(err))
			continue
		}

		var snap SessionSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			zap.L().Warn("persist: failed to unmarshal snapshot",
				zap.String("file", entry.Name()), zap.Error(err))
			continue
		}

		snapshots = append(snapshots, &snap)
	}

	return snapshots, nil
}

// DeleteSnapshot removes a session snapshot from disk.
func (pm *PersistManager) DeleteSnapshot(id string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	path := pm.snapshotPath(id)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("persist: failed to delete snapshot: %w", err)
	}
	return nil
}

// CleanupStaleSnapshots removes snapshots older than maxAge.
// Called on backend startup to clean up stale snapshots (P3-4).
// Aligns with IDE: clean up on revive.
func (pm *PersistManager) CleanupStaleSnapshots(maxAge time.Duration) int {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	snapshots, err := pm.ReadAllSnapshotsUnlocked()
	if err != nil {
		return 0
	}

	cutoff := time.Now().Add(-maxAge).UnixMilli()
	count := 0
	for _, snap := range snapshots {
		if snap.Timestamp < cutoff {
			path := pm.snapshotPath(snap.ID)
			if err := os.Remove(path); err == nil {
				count++
				zap.L().Debug("Cleaned up stale snapshot",
					zap.String("id", snap.ID),
					zap.Int64("timestamp", snap.Timestamp))
			}
		}
	}

	if count > 0 {
		zap.L().Info("Cleaned up stale terminal snapshots",
			zap.Int("count", count),
			zap.Duration("maxAge", maxAge))
	}

	return count
}

// ReadAllSnapshotsUnlocked reads all snapshots without locking (internal use).
func (pm *PersistManager) ReadAllSnapshotsUnlocked() ([]*SessionSnapshot, error) {
	if err := pm.EnsureDir(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(pm.dir)
	if err != nil {
		return nil, err
	}

	var snapshots []*SessionSnapshot
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pm.dir, entry.Name()))
		if err != nil {
			continue
		}
		var snap SessionSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}
		snapshots = append(snapshots, &snap)
	}
	return snapshots, nil
}

// snapshotPath returns the file path for a session snapshot.
func (pm *PersistManager) snapshotPath(id string) string {
	return filepath.Join(pm.dir, id+".json")
}