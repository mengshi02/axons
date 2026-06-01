package api

import (
	"encoding/json"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/logger"
)

// handleListChanges GET /api/changes - List file changes for a session
func (s *Server) handleListChanges(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.fieldRequired", map[string]string{"field": "session_id"}))
		return
	}

	changes, err := s.backupService.ListChanges(sessionID)
	if err != nil {
		logger.S().Errorw("[handleListChanges] Failed to list changes", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.failedListChanges"))
		return
	}

	// Get project_id from first change if available
	var projectID string
	if len(changes) > 0 {
		projectID = changes[0].ProjectID
	}

	// Convert to response format
	result := make([]map[string]interface{}, len(changes))
	for i, c := range changes {
		result[i] = map[string]interface{}{
			"file_path":   c.FilePath,
			"change_type": c.ChangeType,
			"timestamp":   c.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sessionID,
		"project_id": projectID,
		"count":      len(result),
		"changes":    result,
	})
}

// handleGetDiff GET /api/changes/diff - Get diff for a specific file
func (s *Server) handleGetDiff(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	sessionID := r.URL.Query().Get("session_id")
	filePath := r.URL.Query().Get("path")

	logger.S().Debugw("[handleGetDiff] Request received", "session_id", sessionID, "path", filePath)

	if sessionID == "" || filePath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.fieldsRequired", map[string]string{"fields": "session_id and path"}))
		return
	}

	// Get project info
	projectID, err := s.backupService.GetProjectID(sessionID)
	if err != nil {
		logger.S().Errorw("[handleGetDiff] Failed to get project ID", "error", err)
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.sessionNotFound"))
		return
	}

	project, err := s.globalRepo.GetProject(projectID)
	if err != nil || project == nil {
		logger.S().Errorw("[handleGetDiff] Failed to get project", "project_id", projectID, "error", err)
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}

	diff, err := s.backupService.GetDiff(project.RootPath, sessionID, filePath)
	if err != nil {
		logger.S().Errorw("[handleGetDiff] Failed to get diff", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.failedGetDiff"))
		return
	}

	logger.S().Debugw("[handleGetDiff] Returning diff", "diff_lines", len(diff.Diff), "stats", diff.Stats)

	writeJSON(w, http.StatusOK, diff)
}

// handleRevert POST /api/changes/revert - Revert a single file
func (s *Server) handleRevert(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		SessionID string `json:"session_id"`
		FilePath  string `json:"path"`
		ProjectID string `json:"project_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.SessionID == "" || req.FilePath == "" || req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.fieldsRequired", map[string]string{"fields": "session_id, path, and project_id"}))
		return
	}

	project, err := s.globalRepo.GetProject(req.ProjectID)
	if err != nil || project == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}

	if err := s.backupService.Revert(project.RootPath, req.SessionID, req.FilePath); err != nil {
		logger.S().Errorw("[handleRevert] Failed to revert", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.failedRevertFile"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "reverted",
		"path":   req.FilePath,
	})
}

// handleRevertAll POST /api/changes/revert-all - Revert all changes in a session
func (s *Server) handleRevertAll(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		SessionID string `json:"session_id"`
		ProjectID string `json:"project_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.SessionID == "" || req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.fieldsRequired", map[string]string{"fields": "session_id and project_id"}))
		return
	}

	project, err := s.globalRepo.GetProject(req.ProjectID)
	if err != nil || project == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}

	if err := s.backupService.RevertAll(project.RootPath, req.SessionID); err != nil {
		logger.S().Errorw("[handleRevertAll] Failed to revert all", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.failedRevertAll"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "reverted",
	})
}

// handleClearChanges DELETE /api/changes - Clear backup records for a session
func (s *Server) handleClearChanges(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	sessionID := r.URL.Query().Get("session_id")
	projectID := r.URL.Query().Get("project_id")

	if sessionID == "" || projectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.fieldsRequired", map[string]string{"fields": "session_id and project_id"}))
		return
	}

	project, err := s.globalRepo.GetProject(projectID)
	if err != nil || project == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.projectNotFound"))
		return
	}

	if err := s.backupService.ClearSession(project.RootPath, sessionID); err != nil {
		logger.S().Errorw("[handleClearChanges] Failed to clear session", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", i18n.T("api.error.failedClearSession"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "cleared",
	})
}