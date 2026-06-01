// Package registry provides multi-repository registration and management.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// DefaultTTLDays is the default TTL for pruning entries (30 days).
const DefaultTTLDays = 30

// Entry represents a registered repository entry.
type Entry struct {
	Path           string    `json:"path"`
	DBPath         string    `json:"dbPath"`
	AddedAt        time.Time `json:"addedAt"`
	LastAccessedAt time.Time `json:"lastAccessedAt"`
}

// Repository represents a repository with its metadata.
type Repository struct {
	Name           string    `json:"name"`
	Path           string    `json:"path"`
	DBPath         string    `json:"dbPath"`
	AddedAt        time.Time `json:"addedAt"`
	LastAccessedAt time.Time `json:"lastAccessedAt"`
}

// Registry represents the multi-repo registry.
type Registry struct {
	path  string
	Repos map[string]*Entry `json:"repos"`
	mu    sync.RWMutex
}

// Config holds registry configuration.
type Config struct {
	// Path is the registry file path. Defaults to ~/.axons/registry.json
	Path string
}

// DefaultRegistryPath returns the default registry path.
func DefaultRegistryPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	return filepath.Join(homeDir, ".axons", "registry.json")
}

// New creates a new Registry instance.
func New(config *Config) *Registry {
	path := DefaultRegistryPath()
	if config != nil && config.Path != "" {
		path = config.Path
	}

	return &Registry{
		path:  path,
		Repos: make(map[string]*Entry),
	}
}

// Load loads the registry from disk.
func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			r.Repos = make(map[string]*Entry)
			return nil
		}
		return fmt.Errorf("failed to read registry: %w", err)
	}

	var raw struct {
		Repos map[string]*Entry `json:"repos"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		// Corrupt file, start fresh
		r.Repos = make(map[string]*Entry)
		return nil
	}

	r.Repos = raw.Repos
	if r.Repos == nil {
		r.Repos = make(map[string]*Entry)
	}

	return nil
}

// Save persists the registry to disk (atomic write via temp + rename).
// Note: This method assumes the caller has already acquired the lock.
func (r *Registry) Save() error {
	data := struct {
		Repos map[string]*Entry `json:"repos"`
	}{
		Repos: r.Repos,
	}

	// Ensure directory exists
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	// Marshal to JSON
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	// Write to temp file then rename (atomic)
	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}

	if err := os.Rename(tmpPath, r.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to save registry: %w", err)
	}

	return nil
}

// Register registers a project directory. Idempotent.
// Name defaults to filepath.Base(rootDir).
// When no explicit name is provided and the basename already exists
// pointing to a different path, auto-suffixes (api → api-2, api-3, ...).
func (r *Registry) Register(rootDir, name string) (*Repository, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check if directory exists
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		return nil, fmt.Errorf("directory does not exist: %s", absRoot)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	baseName := name
	if baseName == "" {
		baseName = filepath.Base(absRoot)
	}

	repoName := baseName

	// Auto-suffix only when no explicit name was provided
	if name == "" {
		if existing, exists := r.Repos[baseName]; exists {
			existingAbs, _ := filepath.Abs(existing.Path)
			if existingAbs != absRoot {
				// Basename collision with a different path — find next available suffix
				suffix := 2
				for {
					candidate := fmt.Sprintf("%s-%d", baseName, suffix)
					entry, exists := r.Repos[candidate]
					if !exists {
						repoName = candidate
						break
					}
					entryAbs, _ := filepath.Abs(entry.Path)
					if entryAbs == absRoot {
						// Already registered under this suffixed name
						repoName = candidate
						break
					}
					suffix++
				}
			}
		}
	}

	now := time.Now()
	dbPath := filepath.Join(absRoot, ".axons", "axons.db")

	// Preserve addedAt if re-registering same path
	existing, exists := r.Repos[repoName]
	addedAt := now
	if exists {
		addedAt = existing.AddedAt
	}

	r.Repos[repoName] = &Entry{
		Path:           absRoot,
		DBPath:         dbPath,
		AddedAt:        addedAt,
		LastAccessedAt: now,
	}

	// Save to disk
	if err := r.Save(); err != nil {
		delete(r.Repos, repoName)
		return nil, err
	}

	return &Repository{
		Name:           repoName,
		Path:           absRoot,
		DBPath:         dbPath,
		AddedAt:        addedAt,
		LastAccessedAt: now,
	}, nil
}

// Unregister removes a repo from the registry.
// Returns false if not found.
func (r *Registry) Unregister(name string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.Repos[name]; !exists {
		return false, nil
	}

	delete(r.Repos, name)
	return true, r.Save()
}

// List returns all registered repos, sorted by name.
func (r *Registry) List() []*Repository {
	r.mu.RLock()
	defer r.mu.RUnlock()

	repos := make([]*Repository, 0, len(r.Repos))
	for name, entry := range r.Repos {
		repos = append(repos, &Repository{
			Name:           name,
			Path:           entry.Path,
			DBPath:         entry.DBPath,
			AddedAt:        entry.AddedAt,
			LastAccessedAt: entry.LastAccessedAt,
		})
	}

	// Sort by name
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].Name < repos[j].Name
	})

	return repos
}

// Resolve resolves a repo name to its database path.
// Returns error if not found or database file is missing.
func (r *Registry) Resolve(name string) (*Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.Repos[name]
	if !exists {
		return nil, fmt.Errorf("repository %q not found in registry", name)
	}

	// Check if database exists
	if _, err := os.Stat(entry.DBPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("database missing for %q at %s", name, entry.DBPath)
	}

	// Touch lastAccessedAt
	entry.LastAccessedAt = time.Now()
	r.Save()

	return &Repository{
		Name:           name,
		Path:           entry.Path,
		DBPath:         entry.DBPath,
		AddedAt:        entry.AddedAt,
		LastAccessedAt: entry.LastAccessedAt,
	}, nil
}

// PruneResult represents a pruned entry.
type PruneResult struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Reason string `json:"reason"` // "missing" or "expired"
}

// Prune removes stale registry entries.
// Entries are removed if their directory no longer exists or they haven't been accessed within ttlDays.
func (r *Registry) Prune(ttlDays int, excludeNames []string, dryRun bool) ([]*PruneResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -ttlDays)
	excludeSet := make(map[string]bool)
	for _, name := range excludeNames {
		excludeSet[name] = true
	}

	pruned := make([]*PruneResult, 0)
	toDelete := make([]string, 0)

	for name, entry := range r.Repos {
		if excludeSet[name] {
			continue
		}

		// Check if path exists
		if _, err := os.Stat(entry.Path); os.IsNotExist(err) {
			pruned = append(pruned, &PruneResult{
				Name:   name,
				Path:   entry.Path,
				Reason: "missing",
			})
			toDelete = append(toDelete, name)
			continue
		}

		// Check TTL
		if entry.LastAccessedAt.Before(cutoff) {
			pruned = append(pruned, &PruneResult{
				Name:   name,
				Path:   entry.Path,
				Reason: "expired",
			})
			toDelete = append(toDelete, name)
		}
	}

	// Delete entries if not dry run
	if !dryRun && len(toDelete) > 0 {
		for _, name := range toDelete {
			delete(r.Repos, name)
		}
		if err := r.Save(); err != nil {
			return pruned, err
		}
	}

	return pruned, nil
}

// Get returns a repository entry by name without updating lastAccessedAt.
func (r *Registry) Get(name string) *Repository {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.Repos[name]
	if !exists {
		return nil
	}

	return &Repository{
		Name:           name,
		Path:           entry.Path,
		DBPath:         entry.DBPath,
		AddedAt:        entry.AddedAt,
		LastAccessedAt: entry.LastAccessedAt,
	}
}

// Path returns the registry file path.
func (r *Registry) Path() string {
	return r.path
}

// Count returns the number of registered repos.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Repos)
}