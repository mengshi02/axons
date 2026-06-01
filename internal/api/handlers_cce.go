package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/cce"
	"github.com/mengshi02/axons/internal/logger"
)

// handleCCEGetContext handles POST /v1/cce/context
// Retrieves and assembles code context for a query.
func (s *Server) handleCCEGetContext(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		Query      string             `json:"query"`
		Template   cce.ContextTemplate `json:"template"`
		MaxTokens  int                `json:"max_tokens"`
		MaxResults int                `json:"max_results"`
		MinScore   float32            `json:"min_score"`
		NoTests    bool               `json:"no_tests"`
		FileFilter string             `json:"file_filter"`
		Anchors    []cce.Anchor       `json:"anchors"`
		ProjectID  string             `json:"project_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "MISSING_QUERY", "query is required")
		return
	}

	if req.Template == "" {
		req.Template = cce.TemplateGeneral
	}

	// Create CCE engine for the project
	engine, err := s.createCCEEngine(req.ProjectID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "CCE_NOT_AVAILABLE", err.Error())
		return
	}

	query := &cce.RetrievalQuery{
		Query:      req.Query,
		Template:   req.Template,
		MaxTokens:  req.MaxTokens,
		MaxResults: req.MaxResults,
		MinScore:   req.MinScore,
		NoTests:    req.NoTests,
		FileFilter: req.FileFilter,
		Anchors:    req.Anchors,
	}

	ctx := r.Context()
	result, err := engine.GetContext(ctx, query)
	if err != nil {
		logger.S().Errorw("[CCE] GetContext failed", "error", err)
		writeError(w, http.StatusInternalServerError, "CCE_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleCCEEmbed handles POST /v1/cce/embed
// Generates CCE bimodal embeddings.
func (s *Server) handleCCEEmbed(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		ProjectID string          `json:"project_id"`
		Force     bool            `json:"force"`
		Mode      cce.EmbeddingMode `json:"mode"`
		Kinds     []string        `json:"kinds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	if req.Mode == "" {
		req.Mode = cce.ModeDual
	}

	engine, err := s.createCCEEngine(req.ProjectID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "CCE_NOT_AVAILABLE", err.Error())
		return
	}

	ctx := r.Context()
	progress, err := engine.GenerateEmbeddings(ctx, req.Force, req.Mode, req.Kinds, nil)
	if err != nil {
		logger.S().Errorw("[CCE] Embedding generation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "CCE_EMBED_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, progress)
}

// handleCCEStatus handles GET /v1/cce/status
// Returns CCE engine status and statistics.
func (s *Server) handleCCEStatus(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	engine, err := s.createCCEEngine(projectID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"status":    "not_configured",
		})
		return
	}

	stats, err := engine.GetStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CCE_STATS_ERROR", err.Error())
		return
	}

	stats["available"] = true
	writeJSON(w, http.StatusOK, stats)
}

// handleCCETemplates handles GET /v1/cce/templates
// Returns available context templates.
func (s *Server) handleCCETemplates(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	templates := cce.BuiltinTemplates()
	writeJSON(w, http.StatusOK, templates)
}

// createCCEEngine creates a new CCE engine for a project.
// It resolves the project-specific repository and embedder from current configuration.
func (s *Server) createCCEEngine(projectID string) (*cce.Engine, error) {
	embedder := s.createEmbedder()
	if embedder == nil {
		return nil, fmt.Errorf("embedding model not configured")
	}

	// Read max context tokens from DB settings
	var maxCtxTokens int
	if cfg, err := s.globalRepo.GetEmbeddingConfig(); err == nil && cfg != nil {
		if val, ok := cfg["embedding_max_context_tokens"]; ok && val != "" {
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				maxCtxTokens = n
			}
		}
	}

	// Use project-specific repository
	if projectID != "" {
		pRepo, err := s.projectRepo(projectID)
		if err != nil {
			return nil, fmt.Errorf("project not found: %w", err)
		}
		// Get project root path for source code resolution
		var projectRootPath string
		if project, pErr := s.globalRepo.GetProject(projectID); pErr == nil && project != nil {
			projectRootPath = project.RootPath
		}
		engine := cce.NewEngine(pRepo, embedder, projectRootPath)
		if maxCtxTokens > 0 {
			engine.SetMaxContextTokens(maxCtxTokens)
		}
		return engine, nil
	}

	// Fallback to the main repository
	if s.repo != nil {
		engine := cce.NewEngine(s.repo, embedder, "")
		if maxCtxTokens > 0 {
			engine.SetMaxContextTokens(maxCtxTokens)
		}
		return engine, nil
	}

	return nil, fmt.Errorf("no repository available")
}