package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/pkg/types"
)

// handleGetAppState returns the full project list together with the currently
// persisted active_project_id in a single atomic response.
//
// This eliminates the React render window that existed when the frontend called
// GET /v1/projects and GET /v1/config/general separately: between the two calls
// the component would briefly see projects.length > 0 but currentProject = null,
// causing an unwanted DropZone flash.
//
// GET /v1/app/state
// Response:
//
//	{
//	  "projects":          [...],   // same as GET /v1/projects
//	  "count":             3,
//	  "active_project_id": "abc"    // last value saved by PUT /v1/app/state/active-project
//	}
func (s *Server) handleGetAppState(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	projects, err := s.globalRepo.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR",
			fmt.Sprintf("Failed to list projects: %v", err))
		return
	}
	if projects == nil {
		projects = []*types.Project{}
	}

	// active_project_id may be empty string if never set — that is fine.
	activeID, _ := s.globalRepo.GetSetting("active_project_id")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects":          projects,
		"count":             len(projects),
		"active_project_id": activeID,
	})
}

// handleSetActiveProject persists the selected project id so that the next
// GET /v1/app/state can restore the user's selection after a browser refresh.
//
// PUT /v1/app/state/active-project
// Body: { "project_id": "..." }
func (s *Server) handleSetActiveProject(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	if err := s.globalRepo.SetSetting("active_project_id", req.ProjectID); err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR",
			fmt.Sprintf("Failed to save active project: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetFileTreeState returns the persisted file tree expanded paths.
// This allows the file tree to restore its expansion state across browser refreshes
// and panel switches.
//
// GET /v1/app/state/file-tree?project_id=xxx
// Response: { "expanded_paths": ["path1", "path2", ...] }
func (s *Server) handleGetFileTreeState(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	// Use project-scoped key so expanded paths are isolated per project
	settingKey := "file_tree_expanded_paths"
	if projectID != "" {
		settingKey = "file_tree_expanded_paths:" + projectID
	}

	expandedPathsJSON, err := s.globalRepo.GetSetting(settingKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR",
			fmt.Sprintf("Failed to get file tree state: %v", err))
		return
	}

	// If not set, return empty array
	if expandedPathsJSON == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"expanded_paths": []string{}})
		return
	}

	// Parse the JSON array
	var expandedPaths []string
	if err := json.Unmarshal([]byte(expandedPathsJSON), &expandedPaths); err != nil {
		// If corrupted, return empty
		writeJSON(w, http.StatusOK, map[string]interface{}{"expanded_paths": []string{}})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"expanded_paths": expandedPaths})
}

// handleSetFileTreeState persists the file tree expanded paths.
//
// PUT /v1/app/state/file-tree?project_id=xxx
// Body: { "expanded_paths": ["path1", "path2", ...] }
func (s *Server) handleSetFileTreeState(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	// Use project-scoped key so expanded paths are isolated per project
	settingKey := "file_tree_expanded_paths"
	if projectID != "" {
		settingKey = "file_tree_expanded_paths:" + projectID
	}

	var req struct {
		ExpandedPaths []string `json:"expanded_paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Serialize to JSON for storage
	expandedPathsJSON, err := json.Marshal(req.ExpandedPaths)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SERIALIZE_ERROR",
			fmt.Sprintf("Failed to serialize expanded paths: %v", err))
		return
	}

	if err := s.globalRepo.SetSetting(settingKey, string(expandedPathsJSON)); err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR",
			fmt.Sprintf("Failed to save file tree state: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}