// Package repository provides data access layer.
package repository

import (
	"database/sql"
	"time"
)

// Setting represents a setting record.
type Setting struct {
	Key         string
	Value       string
	Category    string
	Description string
	CreatedAt   string
	UpdatedAt   string
}

// GetSetting gets a setting value by key.
func (r *Repository) GetSetting(key string) (string, error) {
	var value string
	err := r.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetSetting sets a setting value.
func (r *Repository) SetSetting(key, value string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := r.db.Exec(`
		INSERT INTO settings (key, value, category, description, created_at, updated_at)
		VALUES (?, ?, 'general', '', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
	`, key, value, now, now, value, now)
	return err
}

// SetSettingWithMeta sets a setting value with category and description.
func (r *Repository) SetSettingWithMeta(key, value, category, description string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := r.db.Exec(`
		INSERT INTO settings (key, value, category, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, category = ?, description = ?, updated_at = ?
	`, key, value, category, description, now, now, value, category, description, now)
	return err
}

// GetSettingWithMeta gets a setting with all metadata.
func (r *Repository) GetSettingWithMeta(key string) (*Setting, error) {
	setting := &Setting{}
	err := r.db.QueryRow(`
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
func (r *Repository) GetSettingsByCategory(category string) (map[string]string, error) {
	rows, err := r.db.Query("SELECT key, value FROM settings WHERE category = ?", category)
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
func (r *Repository) GetAllSettings() (map[string]*Setting, error) {
	rows, err := r.db.Query("SELECT key, value, category, description, created_at, updated_at FROM settings")
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
func (r *Repository) UpdateSettings(settings map[string]string) error {
	tx, err := r.db.Begin()
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
func (r *Repository) UpdateSettingsByCategory(category string, settings map[string]string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Format(time.RFC3339)
	stmt, err := tx.Prepare(`
		INSERT INTO settings (key, value, category, description, created_at, updated_at)
		VALUES (?, ?, ?, '', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?, category = ?, updated_at = ?
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for key, value := range settings {
		if _, err := stmt.Exec(key, value, category, now, now, value, category, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteSetting deletes a setting.
func (r *Repository) DeleteSetting(key string) error {
	_, err := r.db.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

// IsEmbeddingConfigured checks if embedding is properly configured.
func (r *Repository) IsEmbeddingConfigured() (bool, error) {
	provider, err := r.GetSetting("embedding_provider")
	if err != nil {
		return false, err
	}
	if provider == "" {
		return false, nil
	}

	apiKey, err := r.GetSetting("embedding_api_key")
	if err != nil {
		return false, err
	}
	// Ollama doesn't require API key
	if provider == "ollama" {
		return true, nil
	}

	// Custom provider requires base_url instead of api_key
	if provider == "custom" {
		baseURL, err := r.GetSetting("embedding_base_url")
		if err != nil {
			return false, err
		}
		return baseURL != "", nil
	}

	return apiKey != "", nil
}

// GetEmbeddingConfig gets embedding configuration.
func (r *Repository) GetEmbeddingConfig() (map[string]string, error) {
	return r.GetSettingsByCategory("embedding")
}

// GetLLMConfig gets LLM configuration.
func (r *Repository) GetLLMConfig() (map[string]string, error) {
	return r.GetSettingsByCategory("llm")
}

// GetRerankConfig gets rerank configuration.
func (r *Repository) GetRerankConfig() (map[string]string, error) {
	return r.GetSettingsByCategory("rerank")
}

// GetRAGConfig gets RAG configuration.
func (r *Repository) GetRAGConfig() (map[string]string, error) {
	return r.GetSettingsByCategory("rag")
}