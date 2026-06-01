package plugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PluginEntry represents a registered item in the plugin registry.
// It maps to the frontend's panel/command/skill rendering data.
type PluginEntry struct {
	PluginID  string          `json:"pluginId"`
	Type      string          `json:"type"`      // "panels" | "commands" | "skills"
	ID        string          `json:"id"`         // entry-unique ID within type
	Def       json.RawMessage `json:"def"`        // type-specific definition
	Endpoint  string          `json:"endpoint"`   // http://127.0.0.1:PORT (empty for pure-frontend)
	Status    string          `json:"status"`     // running | stopped | starting
	UpdatedAt time.Time       `json:"updatedAt"`
}

// PluginRegistry is the unified registry for plugin contributions.
// It supports both static declaration (from manifest.json) and
// dynamic discovery (runtime sync).
type PluginRegistry struct {
	mu     sync.RWMutex
	byType map[string][]PluginEntry // type → entries
	byID   map[string]*PluginEntry  // "type:id" → entry (fast lookup)
}

// NewPluginRegistry creates a new PluginRegistry.
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		byType: make(map[string][]PluginEntry),
		byID:   make(map[string]*PluginEntry),
	}
}

// Register adds an entry to the registry.
// If an entry with the same type:id already exists, it is skipped (first-wins).
func (r *PluginRegistry) Register(entry PluginEntry) error {
	if entry.Type == "" {
		return fmt.Errorf("registry: entry type is required")
	}
	if entry.ID == "" {
		return fmt.Errorf("registry: entry id is required")
	}
	if entry.PluginID == "" {
		return fmt.Errorf("registry: entry pluginId is required")
	}

	key := entry.Type + ":" + entry.ID

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byID[key]; exists {
		// First-registered wins, skip with warning
		return fmt.Errorf("registry: entry %q already registered, skipping", key)
	}

	entry.UpdatedAt = time.Now()
	r.byType[entry.Type] = append(r.byType[entry.Type], entry)
	r.byID[key] = &r.byType[entry.Type][len(r.byType[entry.Type])-1]

	return nil
}

// Unregister removes all entries for a given plugin.
func (r *PluginRegistry) UnregisterPlugin(pluginID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove from byID
	for key, entry := range r.byID {
		if entry.PluginID == pluginID {
			delete(r.byID, key)
		}
	}

	// Remove from byType
	for typ, entries := range r.byType {
		filtered := make([]PluginEntry, 0, len(entries))
		for _, e := range entries {
			if e.PluginID != pluginID {
				filtered = append(filtered, e)
			}
		}
		r.byType[typ] = filtered
	}
}

// GetByType returns all entries of a given type.
func (r *PluginRegistry) GetByType(entryType string) []PluginEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries, ok := r.byType[entryType]
	if !ok {
		return []PluginEntry{}
	}
	// Return a copy
	result := make([]PluginEntry, len(entries))
	copy(result, entries)
	return result
}

// Get returns a specific entry by type and id.
func (r *PluginRegistry) Get(entryType, id string) (*PluginEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	key := entryType + ":" + id
	entry, ok := r.byID[key]
	if !ok {
		return nil, false
	}
	// Return a copy
	cp := *entry
	return &cp, true
}

// UpdateStatus updates the status of all entries for a given plugin.
func (r *PluginRegistry) UpdateStatus(pluginID, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, entry := range r.byID {
		if entry.PluginID == pluginID {
			entry.Status = status
			entry.UpdatedAt = time.Now()
			// byID stores pointers to byType entries, so this mutates the slice element
			r.byID[key] = entry
		}
	}
}

// UpdateEndpoint updates the endpoint for all entries of a given plugin.
func (r *PluginRegistry) UpdateEndpoint(pluginID, endpoint string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, entry := range r.byID {
		if entry.PluginID == pluginID {
			entry.Endpoint = endpoint
			entry.UpdatedAt = time.Now()
			r.byID[key] = entry
		}
	}
}

// RegisterFromManifest registers all panels and commands from a manifest.
// This is called after a plugin starts successfully.
func (r *PluginRegistry) RegisterFromManifest(manifest *PluginManifest, endpoint string) {
	if manifest.Frontend == nil {
		return
	}

	// Register panels
	for _, panel := range manifest.Frontend.Panels {
		// Convert absolute icon path back to HTTP-accessible path.
		// LoadManifest resolves "ui/icon.svg" to "/home/.axons/plugins/id/ui/icon.svg",
		// but the frontend needs "/plugins/{id}/ui/icon.svg" (served by HandlePluginStaticFiles).
		p := panel // copy to avoid mutation
		if p.Icon != "" && manifest.Dir != "" && strings.HasPrefix(p.Icon, manifest.Dir) {
			rel := strings.TrimPrefix(p.Icon, manifest.Dir)
			p.Icon = "/plugins/" + manifest.ID + rel
		}
		defBytes, _ := json.Marshal(p)
		entry := PluginEntry{
			PluginID: manifest.ID,
			Type:     "panels",
			ID:       panel.ID,
			Def:      defBytes,
			Endpoint: endpoint,
			Status:   "running",
		}
		if err := r.Register(entry); err != nil {
			fmt.Printf("[plugin-registry] WARN: %v\n", err)
		}
	}

	// Register commands
	for _, cmd := range manifest.Frontend.Commands {
		defBytes, _ := json.Marshal(cmd)
		entry := PluginEntry{
			PluginID: manifest.ID,
			Type:     "commands",
			ID:       cmd.ID,
			Def:      defBytes,
			Endpoint: endpoint,
			Status:   "running",
		}
		if err := r.Register(entry); err != nil {
			fmt.Printf("[plugin-registry] WARN: %v\n", err)
		}
	}

	// Register skills
	for _, skillPath := range manifest.Frontend.Skills {
		skillID := manifest.ID + ":" + skillPath
		defBytes, _ := json.Marshal(map[string]string{
			"path": skillPath,
		})
		entry := PluginEntry{
			PluginID: manifest.ID,
			Type:     "skills",
			ID:       skillID,
			Def:      defBytes,
			Endpoint: endpoint,
			Status:   "running",
		}
		if err := r.Register(entry); err != nil {
			fmt.Printf("[plugin-registry] WARN: %v\n", err)
		}
	}
}

// AllEntries returns all registered entries across all types.
func (r *PluginRegistry) AllEntries() []PluginEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []PluginEntry
	for _, entries := range r.byType {
		all = append(all, entries...)
	}
	return all
}