package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mengshi02/axons/pkg/types"
)

// GlobalRepository provides data access to the main (global) database.
// It manages projects, settings, and agent profiles.
type GlobalRepository struct {
	db *sql.DB
}

// NewGlobal creates a new GlobalRepository.
func NewGlobal(db *sql.DB) *GlobalRepository {
	return &GlobalRepository{db: db}
}

// DB returns the underlying main database connection.
func (g *GlobalRepository) DB() *sql.DB {
	return g.db
}

// ──────────────────────────────────────────────
// Project CRUD (id is TEXT UUID)
// ──────────────────────────────────────────────

// CreateProject creates a new project with a UUID id.
func (g *GlobalRepository) CreateProject(id, name, rootPath string) (*types.Project, error) {
	_, err := g.db.Exec(`
		INSERT INTO projects (id, name, root_path, watch_enabled, watch_status) VALUES (?, ?, ?, 0, '')
	`, id, name, rootPath)
	if err != nil {
		return nil, err
	}
	return g.GetProject(id)
}

// CreateProjectWithSource creates a new project with source tracking metadata.
func (g *GlobalRepository) CreateProjectWithSource(id, name, rootPath, source, provider, remoteURL, cloneMode string, managed bool, branch string) (*types.Project, error) {
	_, err := g.db.Exec(`
		INSERT INTO projects (id, name, root_path, watch_enabled, watch_status, source, provider, remote_url, clone_mode, managed, branch)
		VALUES (?, ?, ?, 0, '', ?, ?, ?, ?, ?, ?)
	`, id, name, rootPath, source, provider, remoteURL, cloneMode, managed, branch)
	if err != nil {
		return nil, err
	}
	return g.GetProject(id)
}

// GetProject retrieves a project by its UUID string id.
func (g *GlobalRepository) GetProject(id string) (*types.Project, error) {
	p := &types.Project{}
	var watchEnabled int
	var watchStatus sql.NullString
	var langStack sql.NullString
	var source, provider, remoteURL, cloneMode, branch sql.NullString
	var managed sql.NullBool
	var clonedAt sql.NullTime
	err := g.db.QueryRow(`
		SELECT id, name, root_path, watch_enabled, watch_status, language_stack, created_at, updated_at,
		       source, provider, remote_url, clone_mode, managed, branch, cloned_at
		FROM projects WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &p.RootPath, &watchEnabled, &watchStatus, &langStack, &p.CreatedAt, &p.UpdatedAt,
		&source, &provider, &remoteURL, &cloneMode, &managed, &branch, &clonedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.WatchEnabled = watchEnabled != 0
	if watchStatus.Valid {
		p.WatchStatus = watchStatus.String
	}
	if langStack.Valid && langStack.String != "" {
		json.Unmarshal([]byte(langStack.String), &p.LanguageStack)
	}
	// Read new fields
	if source.Valid {
		p.Source = source.String
	}
	if provider.Valid {
		p.Provider = provider.String
	}
	if remoteURL.Valid {
		p.RemoteURL = remoteURL.String
	}
	if cloneMode.Valid {
		p.CloneMode = cloneMode.String
	}
	if managed.Valid {
		p.Managed = managed.Bool
	}
	if branch.Valid {
		p.Branch = branch.String
	}
	if clonedAt.Valid {
		p.ClonedAt = clonedAt.Time
	}
	return p, nil
}

// ListProjects returns all projects.
func (g *GlobalRepository) ListProjects() ([]*types.Project, error) {
	rows, err := g.db.Query(`
		SELECT id, name, root_path, watch_enabled, watch_status, language_stack, created_at, updated_at,
		       source, provider, remote_url, clone_mode, managed, branch, cloned_at
		FROM projects ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*types.Project
	for rows.Next() {
		p := &types.Project{}
		var watchEnabled int
		var watchStatus sql.NullString
		var langStack sql.NullString
		var source, provider, remoteURL, cloneMode, branch sql.NullString
		var managed sql.NullBool
		var clonedAt sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &p.RootPath, &watchEnabled, &watchStatus, &langStack, &p.CreatedAt, &p.UpdatedAt,
			&source, &provider, &remoteURL, &cloneMode, &managed, &branch, &clonedAt); err != nil {
			return nil, err
		}
		p.WatchEnabled = watchEnabled != 0
		if watchStatus.Valid {
			p.WatchStatus = watchStatus.String
		}
		if langStack.Valid && langStack.String != "" {
			json.Unmarshal([]byte(langStack.String), &p.LanguageStack)
		}
		// Read new fields
		if source.Valid {
			p.Source = source.String
		}
		if provider.Valid {
			p.Provider = provider.String
		}
		if remoteURL.Valid {
			p.RemoteURL = remoteURL.String
		}
		if cloneMode.Valid {
			p.CloneMode = cloneMode.String
		}
		if managed.Valid {
			p.Managed = managed.Bool
		}
		if branch.Valid {
			p.Branch = branch.String
		}
		if clonedAt.Valid {
			p.ClonedAt = clonedAt.Time
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// UpdateProject updates project fields.
func (g *GlobalRepository) UpdateProject(id, name, rootPath string) error {
	_, err := g.db.Exec(`
		UPDATE projects SET name = ?, root_path = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, name, rootPath, id)
	return err
}

// UpdateProjectWatchStatus updates the watch status of a project.
func (g *GlobalRepository) UpdateProjectWatchStatus(id, status string) error {
	_, err := g.db.Exec(`
		UPDATE projects SET watch_status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, status, id)
	return err
}

// SetProjectWatchEnabled enables or disables watching for a project.
func (g *GlobalRepository) SetProjectWatchEnabled(id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := g.db.Exec(`
		UPDATE projects SET watch_enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, v, id)
	return err
}

// GetProjectsWithWatchEnabled returns all projects that have watch enabled.
func (g *GlobalRepository) GetProjectsWithWatchEnabled() ([]*types.Project, error) {
	rows, err := g.db.Query(`
		SELECT id, name, root_path, watch_enabled, watch_status, language_stack, created_at, updated_at,
		       source, provider, remote_url, clone_mode, managed, branch, cloned_at
		FROM projects WHERE watch_enabled = 1 ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*types.Project
	for rows.Next() {
		p := &types.Project{}
		var watchEnabled int
		var watchStatus sql.NullString
		var langStack sql.NullString
		var source, provider, remoteURL, cloneMode, branch sql.NullString
		var managed sql.NullBool
		var clonedAt sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &p.RootPath, &watchEnabled, &watchStatus, &langStack, &p.CreatedAt, &p.UpdatedAt,
			&source, &provider, &remoteURL, &cloneMode, &managed, &branch, &clonedAt); err != nil {
			return nil, err
		}
		p.WatchEnabled = watchEnabled != 0
		if watchStatus.Valid {
			p.WatchStatus = watchStatus.String
		}
		if langStack.Valid && langStack.String != "" {
			json.Unmarshal([]byte(langStack.String), &p.LanguageStack)
		}
		// Read new fields
		if source.Valid {
			p.Source = source.String
		}
		if provider.Valid {
			p.Provider = provider.String
		}
		if remoteURL.Valid {
			p.RemoteURL = remoteURL.String
		}
		if cloneMode.Valid {
			p.CloneMode = cloneMode.String
		}
		if managed.Valid {
			p.Managed = managed.Bool
		}
		if branch.Valid {
			p.Branch = branch.String
		}
		if clonedAt.Valid {
			p.ClonedAt = clonedAt.Time
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// DeleteProject deletes a project record from the main database.
// Caller is responsible for deleting the project's physical .db file via db.Manager.DeleteProjectDB.
func (g *GlobalRepository) DeleteProject(id string) error {
	_, err := g.db.Exec("DELETE FROM projects WHERE id = ?", id)
	return err
}

// UpdateProjectLanguageStack updates the language stack of a project.
func (g *GlobalRepository) UpdateProjectLanguageStack(id string, languages []string) error {
	data, err := json.Marshal(languages)
	if err != nil {
		return err
	}
	_, err = g.db.Exec(`
		UPDATE projects SET language_stack = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, string(data), id)
	return err
}

// ──────────────────────────────────────────────
// Settings (global)
// ──────────────────────────────────────────────

// GetSetting gets a setting value by key.
func (g *GlobalRepository) GetSetting(key string) (string, error) {
	var value string
	err := g.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetSetting sets a setting value.
func (g *GlobalRepository) SetSetting(key, value string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := g.db.Exec(`
		INSERT INTO settings (key, value, category, description, created_at, updated_at)
		VALUES (?, ?, 'general', '', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, value, now, now, value, now)
	return err
}

// SetSettingWithMeta sets a setting value with category and description.
func (g *GlobalRepository) SetSettingWithMeta(key, value, category, description string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := g.db.Exec(`
		INSERT INTO settings (key, value, category, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, category = ?, description = ?, updated_at = ?
	`, key, value, category, description, now, now, value, category, description, now)
	return err
}

// GetSettingWithMeta gets a setting with all metadata.
func (g *GlobalRepository) GetSettingWithMeta(key string) (*Setting, error) {
	setting := &Setting{}
	err := g.db.QueryRow(`
		SELECT key, value, category, description, created_at, updated_at
		FROM settings WHERE key = ?
	`, key).Scan(&setting.Key, &setting.Value, &setting.Category, &setting.Description, &setting.CreatedAt, &setting.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return setting, nil
}

// GetSettingsByCategory gets all settings in a category.
func (g *GlobalRepository) GetSettingsByCategory(category string) (map[string]string, error) {
	rows, err := g.db.Query("SELECT key, value FROM settings WHERE category = ?", category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

// GetAllSettings gets all settings.
func (g *GlobalRepository) GetAllSettings() (map[string]*Setting, error) {
	rows, err := g.db.Query("SELECT key, value, category, description, created_at, updated_at FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]*Setting)
	for rows.Next() {
		setting := &Setting{}
		if err := rows.Scan(&setting.Key, &setting.Value, &setting.Category, &setting.Description, &setting.CreatedAt, &setting.UpdatedAt); err != nil {
			return nil, err
		}
		settings[setting.Key] = setting
	}
	return settings, rows.Err()
}

// UpdateSettings updates multiple settings at once.
func (g *GlobalRepository) UpdateSettings(settings map[string]string) error {
	tx, err := g.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Format(time.RFC3339)
	stmt, err := tx.Prepare(`
		INSERT INTO settings (key, value, category, description, created_at, updated_at)
		VALUES (?, ?, 'general', '', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for key, value := range settings {
		if _, err := stmt.Exec(key, value, now, now, value, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UpdateSettingsByCategory updates multiple settings in a category at once.
func (g *GlobalRepository) UpdateSettingsByCategory(category string, settings map[string]string) error {
	tx, err := g.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Format(time.RFC3339)
	stmt, err := tx.Prepare(`
		INSERT INTO settings (key, value, category, description, created_at, updated_at)
		VALUES (?, ?, ?, '', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for key, value := range settings {
		if _, err := stmt.Exec(key, value, category, now, now, value, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteSetting deletes a setting.
func (g *GlobalRepository) DeleteSetting(key string) error {
	_, err := g.db.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

// IsEmbeddingConfigured checks if embedding is properly configured.
func (g *GlobalRepository) IsEmbeddingConfigured() (bool, error) {
	provider, err := g.GetSetting("embedding_provider")
	if err != nil {
		return false, err
	}
	if provider == "" {
		return false, nil
	}

	if provider == "ollama" {
		return true, nil
	}

	if provider == "custom" {
		baseURL, err := g.GetSetting("embedding_base_url")
		if err != nil {
			return false, err
		}
		return baseURL != "", nil
	}

	apiKey, err := g.GetSetting("embedding_api_key")
	if err != nil {
		return false, err
	}
	return apiKey != "", nil
}

// GetEmbeddingConfig gets embedding configuration.
func (g *GlobalRepository) GetEmbeddingConfig() (map[string]string, error) {
	return g.GetSettingsByCategory("embedding")
}

// GetLLMConfig gets LLM configuration.
func (g *GlobalRepository) GetLLMConfig() (map[string]string, error) {
	return g.GetSettingsByCategory("llm")
}

// GetRerankConfig gets rerank configuration.
func (g *GlobalRepository) GetRerankConfig() (map[string]string, error) {
	return g.GetSettingsByCategory("rerank")
}

// GetRAGConfig gets RAG configuration.
func (g *GlobalRepository) GetRAGConfig() (map[string]string, error) {
	return g.GetSettingsByCategory("rag")
}

// ──────────────────────────────────────────────
// Agent Profiles (global)
// ──────────────────────────────────────────────

// ListAgentProfiles returns all custom agent profiles.
func (g *GlobalRepository) ListAgentProfiles() ([]AgentProfileRow, error) {
	rows, err := g.db.Query(`
		SELECT id, name, description, icon, tools, system_prompt, allow_write, created_at, updated_at
		FROM agent_profiles
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent profiles: %w", err)
	}
	defer rows.Close()

	var profiles []AgentProfileRow
	for rows.Next() {
		var p AgentProfileRow
		var toolsJSON string
		var allowWrite int
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Icon, &toolsJSON, &p.SystemPrompt, &allowWrite, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan agent profile: %w", err)
		}
		if err := json.Unmarshal([]byte(toolsJSON), &p.Tools); err != nil {
			p.Tools = []string{}
		}
		p.AllowWrite = allowWrite == 1
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// GetAgentProfile returns a single custom agent profile by ID.
func (g *GlobalRepository) GetAgentProfile(id string) (*AgentProfileRow, error) {
	var p AgentProfileRow
	var toolsJSON string
	var allowWrite int
	err := g.db.QueryRow(`
		SELECT id, name, description, icon, tools, system_prompt, allow_write, created_at, updated_at
		FROM agent_profiles WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &p.Description, &p.Icon, &toolsJSON, &p.SystemPrompt, &allowWrite, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent profile: %w", err)
	}
	if err := json.Unmarshal([]byte(toolsJSON), &p.Tools); err != nil {
		p.Tools = []string{}
	}
	p.AllowWrite = allowWrite == 1
	return &p, nil
}

// CreateAgentProfile creates a new custom agent profile.
func (g *GlobalRepository) CreateAgentProfile(p *AgentProfileRow) error {
	toolsJSON, err := json.Marshal(p.Tools)
	if err != nil {
		return fmt.Errorf("failed to marshal tools: %w", err)
	}
	allowWrite := 0
	if p.AllowWrite {
		allowWrite = 1
	}
	_, err = g.db.Exec(`
		INSERT INTO agent_profiles (id, name, description, icon, tools, system_prompt, allow_write)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, p.ID, p.Name, p.Description, p.Icon, string(toolsJSON), p.SystemPrompt, allowWrite)
	if err != nil {
		return fmt.Errorf("failed to create agent profile: %w", err)
	}
	return nil
}

// UpdateAgentProfile updates an existing custom agent profile.
func (g *GlobalRepository) UpdateAgentProfile(p *AgentProfileRow) error {
	toolsJSON, err := json.Marshal(p.Tools)
	if err != nil {
		return fmt.Errorf("failed to marshal tools: %w", err)
	}
	allowWrite := 0
	if p.AllowWrite {
		allowWrite = 1
	}
	result, err := g.db.Exec(`
		UPDATE agent_profiles
		SET name = ?, description = ?, icon = ?, tools = ?, system_prompt = ?, allow_write = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, p.Name, p.Description, p.Icon, string(toolsJSON), p.SystemPrompt, allowWrite, p.ID)
	if err != nil {
		return fmt.Errorf("failed to update agent profile: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent profile not found: %s", p.ID)
	}
	return nil
}

// DeleteAgentProfile deletes a custom agent profile by ID.
func (g *GlobalRepository) DeleteAgentProfile(id string) error {
	result, err := g.db.Exec(`DELETE FROM agent_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent profile: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent profile not found: %s", id)
	}
	return nil
}