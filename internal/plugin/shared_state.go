package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SharedState provides a persistent key-value store for cross-plugin communication.
// State keys follow the pattern: "pluginId/key" for plugin-scoped state,
// and "system/key" for system-wide state.
// Data is persisted to ~/.axons/plugins/shared-state.json.
type SharedState struct {
	mu       sync.RWMutex
	data     map[string]json.RawMessage // "pluginId/key" → JSON value
	filePath string
}

// StateEntry represents a single state entry for serialization.
type StateEntry struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// sharedStateFile represents the on-disk format.
type sharedStateFile struct {
	Version int           `json:"version"`
	Entries []StateEntry  `json:"entries"`
}

// NewSharedState creates a new SharedState with persistence at the given path.
func NewSharedState(pluginsDir string) *SharedState {
	filePath := filepath.Join(pluginsDir, "shared-state.json")
	ss := &SharedState{
		data:     make(map[string]json.RawMessage),
		filePath: filePath,
	}
	ss.load()
	return ss
}

// Get retrieves a value by key. Returns the value and whether it existed.
func (ss *SharedState) Get(key string) (json.RawMessage, bool) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	val, ok := ss.data[key]
	return val, ok
}

// Set stores a value by key and persists to disk.
func (ss *SharedState) Set(key string, value json.RawMessage) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	ss.data[key] = value
	return ss.save()
}

// Delete removes all state entries for a given pluginID.
// It removes all keys with the prefix "pluginID/".
func (ss *SharedState) DeleteByPlugin(pluginID string) int {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	prefix := pluginID + "/"
	count := 0
	for k := range ss.data {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(ss.data, k)
			count++
		}
	}
	if count > 0 {
		ss.save()
	}
	return count
}

// GetByPrefix returns all entries whose keys start with the given prefix.
func (ss *SharedState) GetByPrefix(prefix string) map[string]json.RawMessage {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	result := make(map[string]json.RawMessage)
	for k, v := range ss.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			result[k] = v
		}
	}
	return result
}

// All returns all state entries (for system-state endpoint).
func (ss *SharedState) All() map[string]json.RawMessage {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	result := make(map[string]json.RawMessage, len(ss.data))
	for k, v := range ss.data {
		result[k] = v
	}
	return result
}

// load reads the shared state from disk.
func (ss *SharedState) load() {
	data, err := os.ReadFile(ss.filePath)
	if err != nil {
		return // File doesn't exist yet — start empty
	}

	var file sharedStateFile
	if err := json.Unmarshal(data, &file); err != nil {
		return // Corrupted — start empty
	}

	for _, entry := range file.Entries {
		ss.data[entry.Key] = entry.Value
	}
}

// save persists the shared state to disk.
func (ss *SharedState) save() error {
	entries := make([]StateEntry, 0, len(ss.data))
	for k, v := range ss.data {
		entries = append(entries, StateEntry{
			Key:       k,
			Value:     v,
			UpdatedAt: time.Now(),
		})
	}

	file := sharedStateFile{
		Version: 1,
		Entries: entries,
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(ss.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(ss.filePath, data, 0644)
}