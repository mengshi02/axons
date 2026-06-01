package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/i18n"
)

// ============================================================
// File Tree API — handlers_filetree.go
//
// Routes (registered in server.go):
//   GET    /api/filetree          — list directory tree entries
//   POST   /api/filetree/file     — create a new file
//   DELETE /api/filetree/file     — delete a file
//   POST   /api/filetree/folder   — create a new folder
//   DELETE /api/filetree/folder   — delete a folder (recursively)
//   POST   /api/filetree/rename   — rename / move a file or folder
//   GET    /api/filetree/stat     — stat a single path (exists, isDir, size…)
// ============================================================

// fileTreeEntry represents a single node returned by the list endpoint.
type fileTreeEntry struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`     // relative to project root
	AbsPath  string          `json:"abs_path"` // absolute path (for Open-in-Terminal etc.)
	IsDir    bool            `json:"is_dir"`
	Size     int64           `json:"size,omitempty"`
	ModTime  string          `json:"mod_time,omitempty"`
	Children []fileTreeEntry `json:"children,omitempty"`
}

// resolveAndValidate resolves a relative path against the project root and
// guards against path-traversal attacks.
// It reuses the existing resolveFilePath helper so the project-lookup logic
// stays in one place.
func (s *Server) resolveAndValidate(rawPath, projectID string) (string, error) {
	if strings.Contains(rawPath, "..") {
		return "", fmt.Errorf("invalid path: path traversal not allowed")
	}
	abs := s.resolveFilePath(rawPath, projectID)
	// Verify the resolved path still sits inside the project root.
	if projectID != "" {
		if proj, err := s.globalRepo.GetProject(projectID); err == nil && proj != nil {
			root := filepath.Clean(proj.RootPath)
			if !strings.HasPrefix(filepath.Clean(abs)+string(filepath.Separator), root+string(filepath.Separator)) &&
				filepath.Clean(abs) != root {
				return "", fmt.Errorf("path escapes project root")
			}
		}
	}
	return abs, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/filetree — list directory (optionally recursive)
// Query params:
//   path       — relative (or absolute) path; defaults to project root
//   project_id — project ID used for path resolution
//   recursive  — "true" to return the full subtree (default: false)
//   depth      — max recursion depth when recursive=true (default: unlimited)
//   lite       — "true" to skip size/mod_time (lightweight entries, default: false)
// ──────────────────────────────────────────────────────────────────────────────

// ignoredDirs is a set of directories to skip during tree scanning.
var ignoredDirs = map[string]bool{
	".git":         false,
	"node_modules": false,
	".DS_Store":    false,
	"__pycache__":  false,
	".venv":        false,
	"venv":         false,
	".idea":        false,
	".vscode":      false,
	"vendor":       false,
	"dist":         false,
	"build":        false,
	".next":        false,
	".nuxt":        false,
	"coverage":     false,
}

func (s *Server) handleFileTreeList(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")
	rawPath := r.URL.Query().Get("path")
	recursive := r.URL.Query().Get("recursive") == "true"
	lite := r.URL.Query().Get("lite") == "true"

	// Default to project root when no path is given
	if rawPath == "" {
		rawPath = "."
	}

	absPath, err := s.resolveAndValidate(rawPath, projectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.pathNotFound"))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Determine project root for relative-path computation
	projectRoot := absPath
	if !info.IsDir() {
		projectRoot = filepath.Dir(absPath)
	}
	if projectID != "" {
		if proj, err2 := s.globalRepo.GetProject(projectID); err2 == nil && proj != nil {
			projectRoot = filepath.Clean(proj.RootPath)
		}
	}

	var entries []fileTreeEntry
	if info.IsDir() {
		entries, err = listDir(absPath, projectRoot, recursive, -1, lite)
	} else {
		// Single file: return its stat
		rel, _ := filepath.Rel(projectRoot, absPath)
		entries = []fileTreeEntry{statEntry(absPath, rel, info)}
		err = nil
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to list directory: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    rawPath,
		"entries": entries,
		"count":   len(entries),
	})
}

// listDir reads a directory and returns sorted entries.
// depth < 0 means unlimited.
// lite=true skips de.Info() calls (no size/mod_time), reducing disk I/O.
func listDir(dirAbs, rootAbs string, recursive bool, depth int, lite bool) ([]fileTreeEntry, error) {
	des, err := os.ReadDir(dirAbs)
	if err != nil {
		return nil, err
	}

	// Sort: directories first, then files, both alphabetically.
	sort.Slice(des, func(i, j int) bool {
		di, dj := des[i].IsDir(), des[j].IsDir()
		if di != dj {
			return di
		}
		return strings.ToLower(des[i].Name()) < strings.ToLower(des[j].Name())
	})

	entries := make([]fileTreeEntry, 0, len(des))
	for _, de := range des {
		name := de.Name()
		// Skip hidden files / ignored dirs
		if strings.HasPrefix(name, ".") || ignoredDirs[name] {
			continue
		}

		absChild := filepath.Join(dirAbs, name)
		rel, _ := filepath.Rel(rootAbs, absChild)

		var e fileTreeEntry
		if lite {
			// lite mode: skip de.Info() to avoid extra disk I/O
			e = fileTreeEntry{
				Name:   name,
				Path:   rel,
				AbsPath: absChild,
				IsDir:  de.IsDir(),
			}
		} else {
			fi, err := de.Info()
			if err != nil {
				continue
			}
			e = statEntry(absChild, rel, fi)
		}

		if de.IsDir() && recursive && depth != 0 {
			nextDepth := depth - 1
			children, _ := listDir(absChild, rootAbs, recursive, nextDepth, lite)
			e.Children = children
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func statEntry(absPath, relPath string, fi fs.FileInfo) fileTreeEntry {
	size := int64(0)
	if !fi.IsDir() {
		size = fi.Size()
	}
	return fileTreeEntry{
		Name:    fi.Name(),
		Path:    relPath,
		AbsPath: absPath,
		IsDir:   fi.IsDir(),
		Size:    size,
		ModTime: fi.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /api/filetree/stat — stat a single path
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFileTreeStat(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")
	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	absPath, err := s.resolveAndValidate(rawPath, projectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"exists": false})
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"exists":   true,
		"is_dir":   fi.IsDir(),
		"size":     fi.Size(),
		"mod_time": fi.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
		"abs_path": absPath,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/filetree/file — create a new file
// Body: { "path": "relative/path/to/file.go", "content": "", "project_id": "..." }
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFileTreeCreateFile(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	absPath, err := s.resolveAndValidate(req.Path, req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	// Don't overwrite an existing file
	if _, err := os.Stat(absPath); err == nil {
		writeError(w, http.StatusConflict, "CONFLICT", i18n.T("api.error.fileAlreadyExists"))
		return
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to create parent dirs: %v", err))
		return
	}
	if err := os.WriteFile(absPath, []byte(req.Content), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to write file: %v", err))
		return
	}

	// Return entry for incremental update support (entry.path = req.Path, the relative path)
	newFi, _ := os.Stat(absPath)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "File created",
		"abs_path": absPath,
		"path":     req.Path,
		"entry":    statEntry(absPath, req.Path, newFi),
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// DELETE /api/filetree/file — delete a file (not a directory)
// Query params: path, project_id
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFileTreeDeleteFile(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")
	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	absPath, err := s.resolveAndValidate(rawPath, projectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.fileNotFound"))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if fi.IsDir() {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathIsDirectory"))
		return
	}

	if err := os.Remove(absPath); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to delete file: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "File deleted", "path": rawPath})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/filetree/folder — create a new folder
// Body: { "path": "relative/path", "project_id": "..." }
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFileTreeCreateFolder(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		Path      string `json:"path"`
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	absPath, err := s.resolveAndValidate(req.Path, req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	if err := os.MkdirAll(absPath, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to create folder: %v", err))
		return
	}
	// Return entry for incremental update support (with children subtree for directory)
	newFi, _ := os.Stat(absPath)
	entry := statEntry(absPath, req.Path, newFi)
	if newFi != nil && newFi.IsDir() {
		// Compute project root for listDir's relative-path calculation
		projectRoot := filepath.Clean(absPath)
		if req.ProjectID != "" {
			if proj, err := s.globalRepo.GetProject(req.ProjectID); err == nil && proj != nil {
				projectRoot = filepath.Clean(proj.RootPath)
			}
		}
		children, _ := listDir(absPath, projectRoot, true, -1, false)
		entry.Children = children
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Folder created",
		"abs_path": absPath,
		"path":     req.Path,
		"entry":    entry,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// DELETE /api/filetree/folder — delete a folder recursively
// Query params: path, project_id
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFileTreeDeleteFolder(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")
	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	absPath, err := s.resolveAndValidate(rawPath, projectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.folderNotFound"))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if !fi.IsDir() {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathIsFile"))
		return
	}

	if err := os.RemoveAll(absPath); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to delete folder: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Folder deleted", "path": rawPath})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/filetree/rename — rename or move a file or folder
// Body: { "old_path": "...", "new_path": "...", "project_id": "..." }
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFileRename(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		OldPath   string `json:"old_path"`
		NewPath   string `json:"new_path"`
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	if req.OldPath == "" || req.NewPath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.oldNewPathRequired"))
		return
	}

	oldAbs, err := s.resolveAndValidate(req.OldPath, req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("old_path: %v", err))
		return
	}
	newAbs, err := s.resolveAndValidate(req.NewPath, req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("new_path: %v", err))
		return
	}

	if _, err := os.Stat(oldAbs); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.sourcePathNotFound"))
		return
	}
	if _, err := os.Stat(newAbs); err == nil {
		writeError(w, http.StatusConflict, "CONFLICT", i18n.T("api.error.destinationAlreadyExists"))
		return
	}
	// Ensure destination parent dir exists
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to create destination dir: %v", err))
		return
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to rename: %v", err))
		return
	}

	// Return entry for incremental update support (with children subtree for directory)
	newFi, _ := os.Stat(newAbs)
	entry := statEntry(newAbs, req.NewPath, newFi)
	if newFi != nil && newFi.IsDir() {
		// Compute project root for listDir's relative-path calculation
		projectRoot := filepath.Clean(newAbs)
		if req.ProjectID != "" {
			if proj, err := s.globalRepo.GetProject(req.ProjectID); err == nil && proj != nil {
				projectRoot = filepath.Clean(proj.RootPath)
			}
		}
		children, _ := listDir(newAbs, projectRoot, true, -1, false)
		entry.Children = children
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "Renamed successfully",
		"old_abs_path": oldAbs,
		"new_abs_path": newAbs,
		"old_path":     req.OldPath,
		"new_path":     req.NewPath,
		"entry":        entry,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/filetree/copy — copy a file or folder to a new destination
// Body: { "src_path": "...", "dst_path": "...", "project_id": "..." }
// If dst_path already exists and is a directory, the entry is copied *into* it
// (keeping its original name). If dst_path doesn't exist, the entry is copied
// as dst_path (allowing rename-on-copy).
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFileTreeCopy(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req struct {
		SrcPath   string `json:"src_path"`
		DstPath   string `json:"dst_path"`
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}
	if req.SrcPath == "" || req.DstPath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.oldNewPathRequired"))
		return
	}

	srcAbs, err := s.resolveAndValidate(req.SrcPath, req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("src_path: %v", err))
		return
	}
	dstAbs, err := s.resolveAndValidate(req.DstPath, req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("dst_path: %v", err))
		return
	}

	srcFi, err := os.Stat(srcAbs)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.sourcePathNotFound"))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Guard: cannot copy a directory into itself or any of its descendants
	if srcFi.IsDir() {
		srcPrefix := filepath.Clean(srcAbs) + string(filepath.Separator)
		dstClean := filepath.Clean(dstAbs)
		if dstClean == filepath.Clean(srcAbs) || strings.HasPrefix(dstClean, srcPrefix) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Cannot copy a folder into itself or its subdirectory")
			return
		}
	}

	// If dst already exists and is a directory → copy *into* it (keep original name)
	dstFi, dstErr := os.Stat(dstAbs)
	if dstErr == nil && dstFi.IsDir() {
		dstAbs = filepath.Join(dstAbs, srcFi.Name())
		// Re-check: if the target inside the dir already exists → conflict
		if _, err := os.Stat(dstAbs); err == nil {
			writeError(w, http.StatusConflict, "CONFLICT", i18n.T("api.error.destinationAlreadyExists"))
			return
		}
	} else if dstErr == nil {
		// dst exists and is NOT a dir → conflict
		writeError(w, http.StatusConflict, "CONFLICT", i18n.T("api.error.destinationAlreadyExists"))
		return
	}

	// Ensure destination parent dir exists
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to create destination dir: %v", err))
		return
	}

	if srcFi.IsDir() {
		if err := copyDirRecursive(srcAbs, dstAbs); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to copy folder: %v", err))
			return
		}
	} else {
		data, err := os.ReadFile(srcAbs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to read source file: %v", err))
			return
		}
		if err := os.WriteFile(dstAbs, data, srcFi.Mode()); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to write destination file: %v", err))
			return
		}
	}

	// Return entry for incremental update support (with children subtree for directory)
	// Note: dstAbs may have been adjusted (when dst is an existing directory, file is copied *into* it)
	dstResultFi, _ := os.Stat(dstAbs)
	// Compute the actual relative path of the destination
	projectRoot := ""
	if req.ProjectID != "" {
		if proj, err := s.globalRepo.GetProject(req.ProjectID); err == nil && proj != nil {
			projectRoot = filepath.Clean(proj.RootPath)
		}
	}
	var dstRelPath string
	if projectRoot != "" {
		dstRelPath, _ = filepath.Rel(projectRoot, dstAbs)
	} else {
		dstRelPath = req.DstPath // fallback to requested path
	}
	entry := statEntry(dstAbs, dstRelPath, dstResultFi)
	if dstResultFi != nil && dstResultFi.IsDir() {
		if projectRoot == "" {
			projectRoot = filepath.Clean(dstAbs)
		}
		children, _ := listDir(dstAbs, projectRoot, true, -1, false)
		entry.Children = children
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":      "Copied successfully",
		"src_abs_path": srcAbs,
		"dst_abs_path": dstAbs,
		"src_path":     req.SrcPath,
		"dst_path":     req.DstPath,
		"entry":        entry,
	})
}

// copyDirRecursive recursively copies a directory tree from src to dst.
func copyDirRecursive(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, fi.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// handleFolderCreate — kept for backwards-compat route /api/folder
// delegates to handleFileTreeCreateFolder logic
// ──────────────────────────────────────────────────────────────────────────────

func (s *Server) handleFolderCreate(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	s.handleFileTreeCreateFolder(w, r, ps)
}