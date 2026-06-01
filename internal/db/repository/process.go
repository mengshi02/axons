package repository

import (
	"database/sql"
	"encoding/json"
)

// ProcessRecord represents a stored process.
type ProcessRecord struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	ProcessType  string  `json:"process_type"`
	StepCount    int     `json:"step_count"`
	EntryPointID int64   `json:"entry_point_id"`
	TerminalID   int64   `json:"terminal_id"`
	CommunityIDs []int64 `json:"community_ids"`
	CreatedAt    string  `json:"created_at,omitempty"`
}

// ProcessStepRecord represents one step in a process.
type ProcessStepRecord struct {
	ProcessID string `json:"process_id"`
	NodeID    int64  `json:"node_id"`
	Step      int    `json:"step"`
	// Denormalized fields (joined from nodes)
	NodeName string `json:"node_name,omitempty"`
	NodeKind string `json:"node_kind,omitempty"`
	NodeFile string `json:"node_file,omitempty"`
	NodeLine int    `json:"node_line,omitempty"`
}

// ListProcesses lists processes ordered by step_count desc.
func (r *Repository) ListProcesses(limit int) ([]*ProcessRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(`
		SELECT id, label, process_type, step_count, entry_point_id, terminal_id, community_ids, created_at
		FROM processes
		ORDER BY step_count DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var procs []*ProcessRecord
	for rows.Next() {
		p, err := scanProcess(rows)
		if err != nil {
			return nil, err
		}
		procs = append(procs, p)
	}
	return procs, nil
}

// GetProcess gets a single process by ID, with its steps.
func (r *Repository) GetProcess(id string) (*ProcessRecord, []*ProcessStepRecord, error) {
	row := r.db.QueryRow(`
		SELECT id, label, process_type, step_count, entry_point_id, terminal_id, community_ids, created_at
		FROM processes WHERE id = ?
	`, id)

	proc, err := scanProcessRow(row)
	if err != nil {
		return nil, nil, err
	}

	// Load steps with node info
	rows, err := r.db.Query(`
		SELECT ps.process_id, ps.node_id, ps.step,
		       n.name, n.kind, n.file, n.line
		FROM process_steps ps
		JOIN nodes n ON ps.node_id = n.id
		WHERE ps.process_id = ?
		ORDER BY ps.step
	`, id)
	if err != nil {
		return proc, nil, err
	}
	defer rows.Close()

	var steps []*ProcessStepRecord
	for rows.Next() {
		s := &ProcessStepRecord{}
		if err := rows.Scan(&s.ProcessID, &s.NodeID, &s.Step,
			&s.NodeName, &s.NodeKind, &s.NodeFile, &s.NodeLine); err != nil {
			return proc, nil, err
		}
		steps = append(steps, s)
	}
	return proc, steps, nil
}

// CountProcesses returns total process count.
func (r *Repository) CountProcesses() (int64, error) {
	var count int64
	err := r.db.QueryRow(`SELECT COUNT(*) FROM processes`).Scan(&count)
	return count, err
}

// -- helpers --

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanProcess(rows *sql.Rows) (*ProcessRecord, error) {
	p := &ProcessRecord{}
	var communityJSON string
	if err := rows.Scan(&p.ID, &p.Label, &p.ProcessType, &p.StepCount,
		&p.EntryPointID, &p.TerminalID, &communityJSON, &p.CreatedAt); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(communityJSON), &p.CommunityIDs) //nolint
	return p, nil
}

func scanProcessRow(row *sql.Row) (*ProcessRecord, error) {
	p := &ProcessRecord{}
	var communityJSON string
	if err := row.Scan(&p.ID, &p.Label, &p.ProcessType, &p.StepCount,
		&p.EntryPointID, &p.TerminalID, &communityJSON, &p.CreatedAt); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(communityJSON), &p.CommunityIDs) //nolint
	return p, nil
}