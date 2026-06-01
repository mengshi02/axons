// Package api provides architecture rules engine handlers.
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/i18n"
)

// ArchRule represents an architecture dependency rule.
type ArchRule struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"` // "deny" or "allow"
	FromPattern string `json:"from_pattern"`
	ToPattern   string `json:"to_pattern"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	ProjectID   *int64 `json:"project_id,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// ArchRuleViolation represents a rule violation.
type ArchRuleViolation struct {
	RuleID      int64  `json:"rule_id"`
	RuleName    string `json:"rule_name"`
	Kind        string `json:"kind"`
	FromPattern string `json:"from_pattern"`
	ToPattern   string `json:"to_pattern"`
	SourceFile  string `json:"source_file"`
	TargetFile  string `json:"target_file"`
	SourceName  string `json:"source_name"`
	TargetName  string `json:"target_name"`
	EdgeKind    string `json:"edge_kind"`
}

// handleListArchRules lists all architecture rules.
// GET /v1/arch/rules?project_id=xxx
func (s *Server) handleListArchRules(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectIDStr := r.URL.Query().Get("project_id")
	if projectIDStr == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "project_id is required")
		return
	}

	// Get project-specific repository
	repo, err := s.projectRepo(projectIDStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to get project database: %v", err))
		return
	}

	// arch_rules table in project DB doesn't have project_id column (physically isolated)
	rows, err := repo.DB().Query(`
		SELECT id, name, kind, from_pattern, to_pattern, description, enabled, created_at
		FROM arch_rules
		ORDER BY id
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to list rules: %v", err))
		return
	}
	defer rows.Close()

	rules := make([]ArchRule, 0)
	for rows.Next() {
		var rule ArchRule
		var enabled int
		var desc, createdAt *string
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Kind, &rule.FromPattern, &rule.ToPattern, &desc, &enabled, &createdAt); err != nil {
			continue
		}
		rule.Enabled = enabled == 1
		if desc != nil {
			rule.Description = *desc
		}
		if createdAt != nil {
			rule.CreatedAt = *createdAt
		}
		rules = append(rules, rule)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules": rules,
		"count": len(rules),
	})
}

// handleCreateArchRule creates a new architecture rule.
// POST /v1/arch/rules
func (s *Server) handleCreateArchRule(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`
		FromPattern string `json:"from_pattern"`
		ToPattern   string `json:"to_pattern"`
		Description string `json:"description,omitempty"`
		ProjectID   string `json:"project_id"`
	}
	if err := jsonDecode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Name == "" || req.FromPattern == "" || req.ToPattern == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.ruleFieldsRequired"))
		return
	}
	if req.Kind != "deny" && req.Kind != "allow" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.ruleKindInvalid"))
		return
	}
	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "project_id is required")
		return
	}

	// Get project-specific repository
	repo, err := s.projectRepo(req.ProjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to get project database: %v", err))
		return
	}

	result, err := repo.DB().Exec(`
		INSERT INTO arch_rules (name, kind, from_pattern, to_pattern, description, enabled)
		VALUES (?, ?, ?, ?, ?, 1)
	`, req.Name, req.Kind, req.FromPattern, req.ToPattern, req.Description)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to create rule: %v", err))
		return
	}

	id, _ := result.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":     id,
		"status": "created",
	})
}

// handleDeleteArchRule deletes an architecture rule.
// DELETE /v1/arch/rules/:id?project_id=xxx
func (s *Server) handleDeleteArchRule(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectIDStr := r.URL.Query().Get("project_id")
	if projectIDStr == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "project_id is required")
		return
	}

	idStr := ps.ByName("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRuleId"))
		return
	}

	// Get project-specific repository
	repo, err := s.projectRepo(projectIDStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to get project database: %v", err))
		return
	}

	if _, err := repo.DB().Exec("DELETE FROM arch_rules WHERE id = ?", id); err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to delete rule: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleValidateArchRules validates the codebase against architecture rules.
// POST /v1/arch/validate?project_id=xxx
func (s *Server) handleValidateArchRules(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectIDStr := r.URL.Query().Get("project_id")
	if projectIDStr == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "project_id is required")
		return
	}

	// Get project-specific repository
	repo, err := s.projectRepo(projectIDStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to get project database: %v", err))
		return
	}

	// Load enabled deny rules
	rows, err := repo.DB().Query(`
		SELECT id, name, kind, from_pattern, to_pattern
		FROM arch_rules WHERE enabled = 1 AND kind = 'deny'
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", i18n.T("api.error.failedLoadRules"))
		return
	}
	defer rows.Close()

	type denyRule struct {
		id                int64
		name, kind, from, to string
	}
	var denyRules []denyRule
	for rows.Next() {
		var dr denyRule
		rows.Scan(&dr.id, &dr.name, &dr.kind, &dr.from, &dr.to)
		denyRules = append(denyRules, dr)
	}
	rows.Close()

	if len(denyRules) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"violations": []ArchRuleViolation{},
			"count":      0,
			"message":    "No deny rules configured",
		})
		return
	}

	// Find edges that match deny rules via file pattern matching
	violations := make([]ArchRuleViolation, 0)
	for _, rule := range denyRules {
		edgeRows, err := repo.DB().Query(`
			SELECT e.kind,
				   sn.file, sn.name,
				   tn.file, tn.name
			FROM edges e
			JOIN nodes sn ON e.source_id = sn.id
			JOIN nodes tn ON e.target_id = tn.id
			WHERE sn.file LIKE ? AND tn.file LIKE ?
			LIMIT 100
		`, "%"+rule.from+"%", "%"+rule.to+"%")
		if err != nil {
			continue
		}
		for edgeRows.Next() {
			var v ArchRuleViolation
			v.RuleID = rule.id
			v.RuleName = rule.name
			v.Kind = rule.kind
			v.FromPattern = rule.from
			v.ToPattern = rule.to
			edgeRows.Scan(&v.EdgeKind, &v.SourceFile, &v.SourceName, &v.TargetFile, &v.TargetName)
			// Confirm pattern match
			if strings.Contains(v.SourceFile, rule.from) && strings.Contains(v.TargetFile, rule.to) {
				violations = append(violations, v)
			}
		}
		edgeRows.Close()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"violations":    violations,
		"count":         len(violations),
		"rules_checked": len(denyRules),
	})
}