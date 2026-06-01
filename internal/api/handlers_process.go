package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/internal/i18n"
)

// GET /v1/processes?project_id=xxx&limit=
func (s *Server) handleListProcesses(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectIDStr := r.URL.Query().Get("project_id")
	if projectIDStr == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "project_id is required")
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	// Get project-specific repository
	repo, err := s.projectRepo(projectIDStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to get project database: %v", err))
		return
	}

	procs, err := repo.ListProcesses(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_ERROR", fmt.Sprintf("Failed to list processes: %v", err))
		return
	}

	count, _ := repo.CountProcesses()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"processes": procs,
		"count":     count,
	})
}

// GET /v1/processes/:id?project_id=xxx
func (s *Server) handleGetProcess(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectIDStr := r.URL.Query().Get("project_id")
	if projectIDStr == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "project_id is required")
		return
	}

	id := ps.ByName("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.missingProcessId"))
		return
	}

	// Get project-specific repository
	repo, err := s.projectRepo(projectIDStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to get project database: %v", err))
		return
	}

	proc, steps, err := repo.GetProcess(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Process not found: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"process": proc,
		"steps":   steps,
	})
}

// POST /v1/processes/detect?project_id=xxx — manually trigger process detection
func (s *Server) handleDetectProcesses(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
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

	go func() {
		detector := graph.NewProcessDetector(repo)
		_ = detector.DetectAndSave()
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "detecting",
		"message": "Process detection started in background",
	})
}