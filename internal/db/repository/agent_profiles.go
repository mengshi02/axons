package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// AgentProfileRow represents a custom agent profile stored in the database.
type AgentProfileRow struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Icon         string    `json:"icon"`
	Tools        []string  `json:"tools"`
	SystemPrompt string    `json:"system_prompt"`
	AllowWrite   bool      `json:"allow_write"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ListAgentProfiles returns all custom agent profiles.
func (r *Repository) ListAgentProfiles() ([]AgentProfileRow, error) {
	rows, err :=r.db.Query(`
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
func (r *Repository) GetAgentProfile(id string) (*AgentProfileRow, error) {
	var p AgentProfileRow
	var toolsJSON string
	var allowWrite int
	err := r.db.QueryRow(`
		SELECT id, name, description, icon, tools, system_prompt, allow_write, created_at, updated_at
		FROM agent_profiles WHERE id = ?
	`,id).Scan(&p.ID, &p.Name, &p.Description, &p.Icon, &toolsJSON, &p.SystemPrompt, &allowWrite, &p.CreatedAt, &p.UpdatedAt)
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
func (r *Repository) CreateAgentProfile(p *AgentProfileRow) error {
	toolsJSON, err := json.Marshal(p.Tools)
	if err != nil {
		return fmt.Errorf("failed to marshal tools: %w", err)
	}
	allowWrite := 0
	if p.AllowWrite {
		allowWrite = 1
	}
	_, err = r.db.Exec(`
		INSERT INTO agent_profiles (id, name, description, icon, tools, system_prompt, allow_write)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, p.ID, p.Name, p.Description, p.Icon, string(toolsJSON), p.SystemPrompt, allowWrite)
	if err != nil {
		return fmt.Errorf("failed to create agent profile: %w", err)
	}
	return nil
}

// UpdateAgentProfile updates an existing custom agent profile.
func (r *Repository) UpdateAgentProfile(p *AgentProfileRow) error {
	toolsJSON, err := json.Marshal(p.Tools)
	if err != nil {
		return fmt.Errorf("failed to marshal tools: %w", err)
	}
	allowWrite := 0
	if p.AllowWrite {
		allowWrite = 1
	}
	result, err := r.db.Exec(`
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
func (r *Repository) DeleteAgentProfile(id string) error {
	result, err := r.db.Exec(`DELETE FROM agent_profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent profile: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent profile not found: %s", id)
	}
	return nil
}