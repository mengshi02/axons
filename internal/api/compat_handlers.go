// Package api provides compatibility API handlers for web frontend.
package api

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/internal/service"
	"github.com/mengshi02/axons/internal/task"
	"github.com/mengshi02/axons/pkg/types"
	"go.uber.org/zap"
)

// ====== Types for axons web compatibility ======

// WebGraphNode represents a graph node for web API response.
type WebGraphNode struct {
	ID         string                 `json:"id"`
	Label      string                 `json:"label"`
	Properties map[string]interface{} `json:"properties"`
}

// WebGraphRelationship represents a graph relationship for web API response.
type WebGraphRelationship struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	SourceID   string  `json:"sourceId"`
	TargetID   string  `json:"targetId"`
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// WebGraphData represents graph data for visualization.
type WebGraphData struct {
	Nodes         []WebGraphNode         `json:"nodes"`
	Relationships []WebGraphRelationship `json:"relationships"`
	Stats         *WebGraphStats         `json:"stats,omitempty"`
}

// WebGraphStats represents graph statistics.
type WebGraphStats struct {
	TotalNodes        int  `json:"total_nodes"`
	TotalEdges        int  `json:"total_edges"`
	ReturnedNodes     int  `json:"returned_nodes"`
	ReturnedEdges     int  `json:"returned_edges"`
	FilteredConnected bool `json:"filtered_connected"`
}

// WebRepoInfo represents repository information for web API.
type WebRepoInfo struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"`
	IndexedAt  string      `json:"indexedAt,omitempty"`
	LastCommit string      `json:"lastCommit,omitempty"`
	Stats      *WebRepoStats `json:"stats,omitempty"`
}

// WebRepoStats represents repository statistics.
type WebRepoStats struct {
	Files   int `json:"files"`
	Nodes   int `json:"nodes"`
	Edges   int `json:"edges"`
	Symbols int `json:"symbols"`
	Calls   int `json:"calls"`
	Imports int `json:"imports"`
}

// WebSearchResult represents a search result for web API.
type WebSearchResult struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	FilePath     string  `json:"filePath"`
	StartLine    int     `json:"startLine"`
	EndLine      int     `json:"endLine"`
	Score        float64 `json:"score"`
	Content      string  `json:"content,omitempty"`
	Kind         string  `json:"kind,omitempty"`
	Message      string  `json:"message,omitempty"`
	SemanticRank int     `json:"semanticRank,omitempty"`
}

// WebIndexRequest represents an index request.
type WebIndexRequest struct {
	Path string `json:"path"`
}

// WebIndexResponse represents an index response.
type WebIndexResponse struct {
	Success      bool         `json:"success"`
	RepositoryID string       `json:"repositoryId,omitempty"`
	Name         string       `json:"name,omitempty"`
	Stats        *WebRepoStats `json:"stats,omitempty"`
	Error        string       `json:"error,omitempty"`
}

// ====== Helper: map axons SymbolKind to web NodeLabel ======

func symbolKindToWebLabel(kind string) string {
	switch kind {
	case "function":
		return "Function"
	case "method":
		return "Method"
	case "class":
		return "Class"
	case "interface":
		return "Interface"
	case "struct":
		return "Class"
	case "type":
		return "Type"
	case "enum":
		return "Enum"
	case "module":
		return "Module"
	case "variable":
		return "Variable"
	case "constant":
		return "Variable"
	case "field":
		return "Variable"
	case "property":
		return "Variable"
	case "parameter":
		return "Variable"
	default:
		return "CodeElement"
	}
}

// ====== Helper: map axons EdgeKind to web RelationshipType ======

func edgeKindToWebType(kind string) string {
	switch kind {
	case "calls":
		return "CALLS"
	case "imports":
		return "IMPORTS"
	case "imports-type":
		return "IMPORTS"
	case "extends":
		return "EXTENDS"
	case "implements":
		return "IMPLEMENTS"
	case "contains":
		return "CONTAINS"
	default:
		return strings.ToUpper(kind)
	}
}

// compatDB returns the project DB for the given project_id string.
// Returns nil if projectIDStr is empty or if the project DB cannot be accessed.
// This ensures strict project isolation - no fallback to other projects.
func (s *Server) compatDB(projectIDStr string) *sql.DB {
	if projectIDStr == "" {
		return nil
	}
	
	pdb, err := s.dbMgr.ProjectDB(projectIDStr)
	if err != nil {
		return nil
	}
	
	return pdb
}

// ====== GET /api/repos - List repositories ======

func (s *Server) handleWebRepos(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	// List all projects from main DB
	projects, err := s.globalRepo.ListProjects()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to list projects: %v", err))
		return
	}

	repos := make([]WebRepoInfo, 0, len(projects))
	for _, proj := range projects {
		absPath, _ := filepath.Abs(proj.RootPath)
		info := WebRepoInfo{
			Name:  proj.Name,
			Path:  absPath,
			Stats: &WebRepoStats{},
		}

		// Try to get per-project stats from the project DB (best-effort)
		if pdb, err := s.dbMgr.ProjectDB(proj.ID); err == nil {
			pdb.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&info.Stats.Nodes)
			pdb.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&info.Stats.Edges)
			pdb.QueryRow(`SELECT COUNT(DISTINCT file) FROM nodes`).Scan(&info.Stats.Files)
			pdb.QueryRow(`SELECT COUNT(*) FROM edges WHERE kind = 'calls'`).Scan(&info.Stats.Calls)
			pdb.QueryRow(`SELECT COUNT(*) FROM edges WHERE kind IN ('imports', 'imports-type')`).Scan(&info.Stats.Imports)
			info.Stats.Symbols = info.Stats.Nodes
			// Read last_index from project metadata
			pdb.QueryRow(`SELECT value FROM metadata WHERE key = 'last_index'`).Scan(&info.IndexedAt)
		}

		repos = append(repos, info)
	}

	writeJSON(w, http.StatusOK, repos)
}

// ====== GET /api/graph - Get full graph data ======

// Level presets for hierarchical graph loading
// Note: node types use lowercase values matching database storage
// Edge types use lowercase values matching database storage
var levelPresets = map[string]struct {
	nodeTypes       []string
	edgeTypes       []string
	excludeNodeTypes []string
	excludeEdgeTypes []string
}{
	// Structure level: high-level modules and types, no calls
	"structure": {
		nodeTypes: []string{"module", "class", "interface", "struct", "enum", "type", "trait"},
		edgeTypes: []string{"extends", "implements", "imports", "imports-type"},
	},
	// Class level: include types and their relationships, still no calls
	"class": {
		nodeTypes: []string{"module", "class", "interface", "struct", "enum", "type", "trait"},
		edgeTypes: []string{"extends", "implements", "imports", "imports-type"},
	},
	// Function level: include functions and methods, with calls
	"function": {
		nodeTypes: []string{"module", "class", "interface", "struct", "enum", "type", "trait", "function", "method"},
		edgeTypes: []string{"extends", "implements", "imports", "imports-type", "calls"},
	},
	// Full: everything
	"full": {
		nodeTypes: nil,
		edgeTypes: nil,
	},
	// No calls: exclude call edges to reduce clutter
	"no-calls": {
		nodeTypes:        nil,
		excludeEdgeTypes: []string{"calls"},
	},
	// Minimal: only types, no functions or variables
	"minimal": {
		nodeTypes:        []string{"class", "interface", "struct", "enum", "type", "trait"},
		excludeEdgeTypes: []string{"calls"},
	},
}

// parseCommaSeparatedList parses a comma-separated string into a slice
func parseCommaSeparatedList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// stringSliceToSet converts a slice to a set for O(1) lookup
// Returns nil if slice is empty or nil
func stringSliceToSet(slice []string) map[string]bool {
	if len(slice) == 0 {
		return nil
	}
	set := make(map[string]bool, len(slice))
	for _, s := range slice {
		set[s] = true
	}
	return set
}

func (s *Server) handleWebGraph(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	// Optional limit parameter - 0 means no limit
	limitStr := r.URL.Query().Get("limit")
	limit := 0 // default: no limit
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l >= 0 {
			limit = l
		}
	}

	// Optional filter_connected parameter - only return nodes with edges (default true)
	filterConnected := r.URL.Query().Get("filter_connected") != "false"

	// Optional include_stats parameter - include total counts
	includeStats := r.URL.Query().Get("include_stats") == "true"

	// Optional project_id parameter — route to the correct project DB
	projectIDStr := r.URL.Query().Get("project_id")
	var projectRootPath string
	if projectIDStr != "" {
		if proj, err := s.globalRepo.GetProject(projectIDStr); err == nil && proj != nil {
			projectRootPath = filepath.Clean(proj.RootPath)
		}
	}
	graphDB := s.compatDB(projectIDStr)
	if graphDB == nil {
		writeJSON(w, http.StatusOK, WebGraphData{
			Nodes:         []WebGraphNode{},
			Relationships: []WebGraphRelationship{},
		})
		return
	}

	// Parse level preset and type filters
	level := r.URL.Query().Get("level")
	nodeTypesFilter := parseCommaSeparatedList(r.URL.Query().Get("node_types"))
	excludeNodeTypes := parseCommaSeparatedList(r.URL.Query().Get("exclude_node_types"))
	edgeTypesFilter := parseCommaSeparatedList(r.URL.Query().Get("edge_types"))
	excludeEdgeTypes := parseCommaSeparatedList(r.URL.Query().Get("exclude_edge_types"))

	// Apply level preset if specified
	if level != "" {
		if preset, ok := levelPresets[level]; ok {
			// Only use preset if explicit filters are not provided
			if nodeTypesFilter == nil {
				nodeTypesFilter = preset.nodeTypes
			}
			if edgeTypesFilter == nil {
				edgeTypesFilter = preset.edgeTypes
			}
			if excludeNodeTypes == nil && preset.excludeNodeTypes != nil {
				excludeNodeTypes = preset.excludeNodeTypes
			}
			if excludeEdgeTypes == nil && preset.excludeEdgeTypes != nil {
				excludeEdgeTypes = preset.excludeEdgeTypes
			}
		}
	}

	// Convert filters to sets for efficient lookup
	nodeTypesSet := stringSliceToSet(nodeTypesFilter)
	excludeNodeTypesSet := stringSliceToSet(excludeNodeTypes)
	edgeTypesSet := stringSliceToSet(edgeTypesFilter)
	excludeEdgeTypesSet := stringSliceToSet(excludeEdgeTypes)

	data := WebGraphData{
		Nodes:         make([]WebGraphNode, 0),
		Relationships: make([]WebGraphRelationship, 0),
	}

	// Track unique files for File/Folder nodes
	fileSet := make(map[string]bool)

	// Helper function to normalize path
	normalizePath := func(filePath string) string {
		if projectRootPath == "" {
			return filePath
		}
		cleanPath := filepath.Clean(filePath)
		if !filepath.IsAbs(cleanPath) {
			return cleanPath
		}
		cleanRoot := filepath.Clean(projectRootPath)
		separator := string(filepath.Separator)
		prefix := cleanRoot + separator
		if strings.HasPrefix(cleanPath, prefix) {
			return strings.TrimPrefix(cleanPath, prefix)
		}
		if cleanPath == cleanRoot {
			return ""
		}
		return filePath
	}

	// Build edge query with type filtering
	var edgeQuery strings.Builder
	var edgeArgs []interface{}
	edgeQuery.WriteString(`SELECT id, source_id, target_id, kind, confidence FROM edges WHERE 1=1`)


	// Add edge type filter (include)
	if edgeTypesSet != nil {
		edgeQuery.WriteString(` AND kind IN (`)
		for i, kind := range edgeTypesFilter {
			if i > 0 {
				edgeQuery.WriteString(`,`)
			}
			edgeQuery.WriteString(`?`)
			edgeArgs = append(edgeArgs, kind)
		}
		edgeQuery.WriteString(`)`)
	}

	// Add edge type filter (exclude)
	if excludeEdgeTypesSet != nil {
		edgeQuery.WriteString(` AND kind NOT IN (`)
		for i, kind := range excludeEdgeTypes {
			if i > 0 {
				edgeQuery.WriteString(`,`)
			}
			edgeQuery.WriteString(`?`)
			edgeArgs = append(edgeArgs, kind)
		}
		edgeQuery.WriteString(`)`)
	}

	if limit > 0 {
		edgeQuery.WriteString(` ORDER BY id LIMIT ?`)
		edgeArgs = append(edgeArgs, limit*2)
	} else {
		edgeQuery.WriteString(` ORDER BY id`)
	}

	// Get edges first to determine connected nodes
	edgeRows, err := graphDB.Query(edgeQuery.String(), edgeArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to query edges: %v", err))
		return
	}
	defer edgeRows.Close()

	// Track connected node IDs and collect edges
	connectedNodes := make(map[int64]bool)
	for edgeRows.Next() {
		var id, sourceID, targetID int64
		var kind string
		var confidence sql.NullFloat64

		if err := edgeRows.Scan(&id, &sourceID, &targetID, &kind, &confidence); err != nil {
			continue
		}

		rel := WebGraphRelationship{
			ID:       strconv.FormatInt(id, 10),
			Type:     edgeKindToWebType(kind),
			SourceID: strconv.FormatInt(sourceID, 10),
			TargetID: strconv.FormatInt(targetID, 10),
		}
		if confidence.Valid {
			rel.Confidence = confidence.Float64
		}
		data.Relationships = append(data.Relationships, rel)

		// Track connected nodes
		connectedNodes[sourceID] = true
		connectedNodes[targetID] = true
	}
	edgeRows.Close()

	// Build node query with type filtering
	var nodeQuery strings.Builder
	var nodeArgs []interface{}

	if filterConnected && len(connectedNodes) > 0 {
		// Only query nodes that have edges
		nodeQuery.WriteString(`
			SELECT DISTINCT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.qualified_name, n.exported, n.visibility, n.role
			FROM nodes n
			WHERE 1=1`)


		// Add node type filter (include)
		if nodeTypesSet != nil {
			nodeQuery.WriteString(` AND n.kind IN (`)
			for i, kind := range nodeTypesFilter {
				if i > 0 {
					nodeQuery.WriteString(`,`)
				}
				nodeQuery.WriteString(`?`)
				nodeArgs = append(nodeArgs, kind)
			}
			nodeQuery.WriteString(`)`)
		}

		// Add node type filter (exclude)
		if excludeNodeTypesSet != nil {
			nodeQuery.WriteString(` AND n.kind NOT IN (`)
			for i, kind := range excludeNodeTypes {
				if i > 0 {
					nodeQuery.WriteString(`,`)
				}
				nodeQuery.WriteString(`?`)
				nodeArgs = append(nodeArgs, kind)
			}
			nodeQuery.WriteString(`)`)
		}

		// Filter by connected nodes (use edge type filters, not node type filters)
		nodeQuery.WriteString(` AND n.id IN (
			SELECT DISTINCT source_id FROM edges WHERE 1=1`)
		if edgeTypesSet != nil {
			nodeQuery.WriteString(` AND kind IN (`)
			for i, kind := range edgeTypesFilter {
				if i > 0 {
					nodeQuery.WriteString(`,`)
				}
				nodeQuery.WriteString(`?`)
				nodeArgs = append(nodeArgs, kind)
			}
			nodeQuery.WriteString(`)`)
		}
		if excludeEdgeTypesSet != nil {
			nodeQuery.WriteString(` AND kind NOT IN (`)
			for i, kind := range excludeEdgeTypes {
				if i > 0 {
					nodeQuery.WriteString(`,`)
				}
				nodeQuery.WriteString(`?`)
				nodeArgs = append(nodeArgs, kind)
			}
			nodeQuery.WriteString(`)`)
		}
		nodeQuery.WriteString(`
			UNION
			SELECT DISTINCT target_id FROM edges WHERE 1=1`)
		if edgeTypesSet != nil {
			nodeQuery.WriteString(` AND kind IN (`)
			for i, kind := range edgeTypesFilter {
				if i > 0 {
					nodeQuery.WriteString(`,`)
				}
				nodeQuery.WriteString(`?`)
				nodeArgs = append(nodeArgs, kind)
			}
			nodeQuery.WriteString(`)`)
		}
		if excludeEdgeTypesSet != nil {
			nodeQuery.WriteString(` AND kind NOT IN (`)
			for i, kind := range excludeEdgeTypes {
				if i > 0 {
					nodeQuery.WriteString(`,`)
				}
				nodeQuery.WriteString(`?`)
				nodeArgs = append(nodeArgs, kind)
			}
			nodeQuery.WriteString(`)`)
		}
		nodeQuery.WriteString(`)`)
	} else {
		// Query all nodes with type filtering
		nodeQuery.WriteString(`
			SELECT id, name, kind, file, line, end_line, qualified_name, exported, visibility, role
			FROM nodes
			WHERE 1=1`)


		// Add node type filter (include)
		if nodeTypesSet != nil {
			nodeQuery.WriteString(` AND kind IN (`)
			for i, kind := range nodeTypesFilter {
				if i > 0 {
					nodeQuery.WriteString(`,`)
				}
				nodeQuery.WriteString(`?`)
				nodeArgs = append(nodeArgs, kind)
			}
			nodeQuery.WriteString(`)`)
		}

		// Add node type filter (exclude)
		if excludeNodeTypesSet != nil {
			nodeQuery.WriteString(` AND kind NOT IN (`)
			for i, kind := range excludeNodeTypes {
				if i > 0 {
					nodeQuery.WriteString(`,`)
				}
				nodeQuery.WriteString(`?`)
				nodeArgs = append(nodeArgs, kind)
			}
			nodeQuery.WriteString(`)`)
		}
	}

	if limit > 0 {
		nodeQuery.WriteString(` ORDER BY id LIMIT ?`)
		nodeArgs = append(nodeArgs, limit)
	} else {
		nodeQuery.WriteString(` ORDER BY id`)
	}

	// Get nodes
	nodeRows, err := graphDB.Query(nodeQuery.String(), nodeArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to query nodes: %v", err))
		return
	}
	defer nodeRows.Close()

	for nodeRows.Next() {
		var id int64
		var name, kind, file, qualifiedName, visibility, role string
		var line, endLine int
		var exported bool
		var endLineNull sql.NullInt64

		if err := nodeRows.Scan(&id, &name, &kind, &file, &line, &endLineNull, &qualifiedName, &exported, &visibility, &role); err != nil {
			continue
		}

		if endLineNull.Valid {
			endLine = int(endLineNull.Int64)
		}

		// Normalize file path and track for File/Folder node generation
		normalizedFile := file
		if file != "" {
			normalizedFile = normalizePath(file)
			if normalizedFile != "" {
				fileSet[normalizedFile] = true
			}
		}

		node := WebGraphNode{
			ID:    strconv.FormatInt(id, 10),
			Label: symbolKindToWebLabel(kind),
			Properties: map[string]interface{}{
				"name":           name,
				"filePath":       normalizedFile,
				"startLine":      line,
				"endLine":        endLine,
				"qualifiedName":  qualifiedName,
				"exported":       exported,
				"visibility":     visibility,
				"role":           role,
			},
		}
		data.Nodes = append(data.Nodes, node)
	}

	// Generate File and Folder nodes from tracked files (from nodes table)
	folderSet := make(map[string]bool)
	fileNodeIDs := make(map[string]bool) // Track all file node IDs

	for filePath := range fileSet {
		normalizedPath := normalizePath(filePath)
		if normalizedPath == "" {
			continue
		}

		// Add File node
		fileName := filepath.Base(normalizedPath)
		fileNodeID := "file:" + normalizedPath
		fileNodeIDs[fileNodeID] = true
		fileNode := WebGraphNode{
			ID:    fileNodeID,
			Label: "File",
			Properties: map[string]interface{}{
				"name":     fileName,
				"path":     normalizedPath,
				"filePath": normalizedPath,
			},
		}
		data.Nodes = append(data.Nodes, fileNode)

		// Add Folder nodes for all parent directories
		dir := filepath.Dir(normalizedPath)
		for dir != "" && dir != "." {
			if !folderSet[dir] {
				folderSet[dir] = true
				folderName := filepath.Base(dir)
				folderNode := WebGraphNode{
					ID:    "folder:" + dir,
					Label: "Folder",
					Properties: map[string]interface{}{
						"name": folderName,
						"path": dir,
					},
				}
				data.Nodes = append(data.Nodes, folderNode)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Also scan filesystem for all files (including non-code files)
	// This uses the same blacklist logic as collect_files.go
	if projectRootPath != "" {
		scanAllFiles(projectRootPath, normalizePath, folderSet, fileNodeIDs, &data.Nodes)
	}

	// Include stats if requested
	if includeStats {
		var totalNodes, totalEdges int
		graphDB.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&totalNodes)
		graphDB.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&totalEdges)
		data.Stats = &WebGraphStats{
			TotalNodes:        totalNodes,
			TotalEdges:        totalEdges,
			ReturnedNodes:     len(data.Nodes),
			ReturnedEdges:     len(data.Relationships),
			FilteredConnected: filterConnected,
		}
	}

	writeJSON(w, http.StatusOK, data)
}

// ====== POST /api/graph/delta - Get incremental graph changes ======

// GraphDeltaRequest represents the request body for the delta endpoint.
type GraphDeltaRequest struct {
	ProjectID    string   `json:"project_id"`
	ChangedFiles []string `json:"changed_files"`
	RemovedFiles []string `json:"removed_files"`
}

// GraphDeltaResponse represents the incremental graph changes.
type GraphDeltaResponse struct {
	AddedNodes         []WebGraphNode         `json:"added_nodes"`
	AddedEdges         []WebGraphRelationship `json:"added_edges"`
	RemovedNodeIDs     []string              `json:"removed_node_ids"`
	RemovedEdgeIDs     []string              `json:"removed_edge_ids"`
	IsFullRebuild      bool                  `json:"is_full_rebuild"`
}

func (s *Server) handleWebGraphDelta(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	var req GraphDeltaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	if req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "project_id is required")
		return
	}

	// If too many files changed, signal a full rebuild instead
	if len(req.ChangedFiles) > 500 || len(req.RemovedFiles) > 500 {
		writeJSON(w, http.StatusOK, GraphDeltaResponse{
			IsFullRebuild: true,
		})
		return
	}

	graphDB := s.compatDB(req.ProjectID)
	if graphDB == nil {
		writeJSON(w, http.StatusOK, GraphDeltaResponse{
			IsFullRebuild: true,
		})
		return
	}

	// Resolve project root path for path normalization
	var projectRootPath string
	if proj, err := s.globalRepo.GetProject(req.ProjectID); err == nil && proj != nil {
		projectRootPath = filepath.Clean(proj.RootPath)
	}

	normalizePath := func(filePath string) string {
		if projectRootPath == "" {
			return filePath
		}
		cleanPath := filepath.Clean(filePath)
		if !filepath.IsAbs(cleanPath) {
			return cleanPath
		}
		cleanRoot := filepath.Clean(projectRootPath)
		separator := string(filepath.Separator)
		prefix := cleanRoot + separator
		if strings.HasPrefix(cleanPath, prefix) {
			return strings.TrimPrefix(cleanPath, prefix)
		}
		if cleanPath == cleanRoot {
			return ""
		}
		return filePath
	}

	response := GraphDeltaResponse{
		AddedNodes:     make([]WebGraphNode, 0),
		AddedEdges:     make([]WebGraphRelationship, 0),
		RemovedNodeIDs: make([]string, 0),
		RemovedEdgeIDs: make([]string, 0),
	}

	// Normalize changed_files and removed_files to relative paths
	// The frontend may send absolute paths, but the database stores relative paths
	normalizedChangedFiles := make([]string, 0, len(req.ChangedFiles))
	for _, f := range req.ChangedFiles {
		normalizedChangedFiles = append(normalizedChangedFiles, normalizePath(f))
	}
	normalizedRemovedFiles := make([]string, 0, len(req.RemovedFiles))
	for _, f := range req.RemovedFiles {
		normalizedRemovedFiles = append(normalizedRemovedFiles, normalizePath(f))
	}

	// 1. Collect removed node IDs from removed files
	for _, file := range normalizedRemovedFiles {
		rows, err := graphDB.Query(`SELECT id FROM nodes WHERE file = ?`, file)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				response.RemovedNodeIDs = append(response.RemovedNodeIDs, strconv.FormatInt(id, 10))
			}
		}
		rows.Close()

		// Also collect edges connected to removed nodes
		edgeRows, err := graphDB.Query(`
			SELECT e.id FROM edges e
			WHERE e.source_id IN (SELECT id FROM nodes WHERE file = ?)
			   OR e.target_id IN (SELECT id FROM nodes WHERE file = ?)
		`, file, file)
		if err != nil {
			continue
		}
		for edgeRows.Next() {
			var id int64
			if edgeRows.Scan(&id) == nil {
				response.RemovedEdgeIDs = append(response.RemovedEdgeIDs, strconv.FormatInt(id, 10))
			}
		}
		edgeRows.Close()
	}

	// 2. Collect added/updated nodes from changed files
	changedNodeIDs := make(map[int64]bool)
	for _, file := range normalizedChangedFiles {
		rows, err := graphDB.Query(`
			SELECT id, name, kind, file, line, end_line, qualified_name, exported, visibility, role
			FROM nodes WHERE file = ?
		`, file)
		if err != nil {
			logger.Debug("Delta API: failed to query nodes for file",
				zap.String("file", file),
				zap.Error(err),
			)
			continue
		}
		nodeCount := 0
		for rows.Next() {
			var id int64
			var name, kind, fileStr, qualifiedName, visibility, role string
			var line int
			var endLineNull sql.NullInt64
			var exported bool

			if err := rows.Scan(&id, &name, &kind, &fileStr, &line, &endLineNull, &qualifiedName, &exported, &visibility, &role); err != nil {
				continue
			}

			endLine := 0
			if endLineNull.Valid {
				endLine = int(endLineNull.Int64)
			}

			normalizedFile := normalizePath(fileStr)

			node := WebGraphNode{
				ID:    strconv.FormatInt(id, 10),
				Label: symbolKindToWebLabel(kind),
				Properties: map[string]interface{}{
					"name":          name,
					"filePath":      normalizedFile,
					"startLine":     line,
					"endLine":       endLine,
					"qualifiedName": qualifiedName,
					"exported":      exported,
					"visibility":    visibility,
					"role":          role,
				},
			}
			response.AddedNodes = append(response.AddedNodes, node)
			changedNodeIDs[id] = true
			nodeCount++
		}
		rows.Close()
		logger.Debug("Delta API: nodes found for changed file",
			zap.String("file", file),
			zap.Int("nodeCount", nodeCount),
		)
	}

	// 3. Collect edges connected to changed nodes
	if len(changedNodeIDs) > 0 {
		// Include edges between changed nodes and any existing nodes
		var edgeQuery strings.Builder
		var edgeArgs []interface{}

		edgeQuery.WriteString(`
			SELECT e.id, e.source_id, e.target_id, e.kind, e.confidence
			FROM edges e
			WHERE e.source_id IN (`)
		idx := 0
		for id := range changedNodeIDs {
			if idx > 0 {
				edgeQuery.WriteString(`,`)
			}
			edgeQuery.WriteString(`?`)
			edgeArgs = append(edgeArgs, id)
			idx++
		}
		edgeQuery.WriteString(`) OR e.target_id IN (`)
		idx = 0
		for id := range changedNodeIDs {
			if idx > 0 {
				edgeQuery.WriteString(`,`)
			}
			edgeQuery.WriteString(`?`)
			edgeArgs = append(edgeArgs, id)
			idx++
		}
		edgeQuery.WriteString(`)`)

		// Debug: log the edge query details
		changedIDList := make([]int64, 0, len(changedNodeIDs))
		for id := range changedNodeIDs {
			changedIDList = append(changedIDList, id)
		}
		logger.Debug("Delta API edge query",
			zap.Int("changedNodeCount", len(changedNodeIDs)),
			zap.Int64s("changedNodeIDs", changedIDList),
			zap.String("sql", edgeQuery.String()),
			zap.Int("argCount", len(edgeArgs)),
		)

		edgeRows, err := graphDB.Query(edgeQuery.String(), edgeArgs...)
		if err == nil {
			edgeKindCount := make(map[string]int)
			for edgeRows.Next() {
				var id, sourceID, targetID int64
				var kind string
				var confidence sql.NullFloat64

				if err := edgeRows.Scan(&id, &sourceID, &targetID, &kind, &confidence); err != nil {
					continue
				}

				rel := WebGraphRelationship{
					ID:       strconv.FormatInt(id, 10),
					Type:     edgeKindToWebType(kind),
					SourceID: strconv.FormatInt(sourceID, 10),
					TargetID: strconv.FormatInt(targetID, 10),
				}
				if confidence.Valid {
					rel.Confidence = confidence.Float64
				}
				response.AddedEdges = append(response.AddedEdges, rel)
				edgeKindCount[kind]++
			}
			edgeRows.Close()
			logger.Debug("Delta API: edge kinds in response", zap.Any("kinds", edgeKindCount))
		} else {
			logger.Debug("Delta API edge query failed", zap.Error(err))
		}
	} else {
		logger.Debug("Delta API: no changedNodeIDs, skipping edge query")
	}

	// Debug: also count total edges in DB for comparison
	var totalEdgeCount int
	graphDB.QueryRow("SELECT COUNT(*) FROM edges").Scan(&totalEdgeCount)

	// Debug: count edges connected to changed nodes directly
	edgesConnectedToChanged := 0
	if len(changedNodeIDs) > 0 {
		var countArgs []interface{}
		var countQuery strings.Builder
		countQuery.WriteString("SELECT COUNT(*) FROM edges WHERE source_id IN (")
		idx := 0
		for id := range changedNodeIDs {
			if idx > 0 {
				countQuery.WriteString(",")
			}
			countQuery.WriteString("?")
			countArgs = append(countArgs, id)
			idx++
		}
		countQuery.WriteString(") OR target_id IN (")
		idx = 0
		for id := range changedNodeIDs {
			if idx > 0 {
				countQuery.WriteString(",")
			}
			countQuery.WriteString("?")
			countArgs = append(countArgs, id)
			idx++
		}
		countQuery.WriteString(")")
		graphDB.QueryRow(countQuery.String(), countArgs...).Scan(&edgesConnectedToChanged)
	}

	logger.Info("Graph delta response",
		zap.Int("addedNodes", len(response.AddedNodes)),
		zap.Int("addedEdges", len(response.AddedEdges)),
		zap.Int("removedNodes", len(response.RemovedNodeIDs)),
		zap.Int("removedEdges", len(response.RemovedEdgeIDs)),
		zap.Int("changedNodeIDs", len(changedNodeIDs)),
		zap.Int("totalEdgesInDB", totalEdgeCount),
		zap.Int("edgesConnectedToChangedNodes", edgesConnectedToChanged),
		zap.Bool("isFullRebuild", response.IsFullRebuild),
	)

	writeJSON(w, http.StatusOK, response)
}

// ====== GET /api/file - Get file content ======

func (s *Server) handleWebFile(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	// Security check: prevent path traversal
	if strings.Contains(filePath, "..") {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidPath"))
		return
	}

	// Get absolute path
	absPath := s.resolveFilePath(filePath, r.URL.Query().Get("project_id"))

	switch r.Method {
	case "GET":
		s.handleFileGet(w, absPath)
	case "POST":
		s.handleFileWrite(w, r, absPath)
	case "DELETE":
		s.handleFileDelete(w, absPath)
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
	}
}

// resolveFilePath resolves relative path to absolute path
func (s *Server) resolveFilePath(filePath string, projectID string) string {
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		// Get root dir from projects table via globalRepo
		var rootDir string
		if projectID != "" {
			// Query project to get root_path
			if project, err := s.globalRepo.GetProject(projectID); err == nil && project != nil {
				rootDir = project.RootPath
			}
		}
		if rootDir == "" {
			// Fallback: use first project's root_path
			if projects, err := s.globalRepo.ListProjects(); err == nil && len(projects) > 0 {
				rootDir = projects[0].RootPath
			}
		}
		if rootDir == "" {
			rootDir = "."
		}
		absPath = filepath.Join(rootDir, filePath)
	}
	return absPath
}

// binaryExtensions maps file extensions to MIME types for binary file preview.
// This includes image formats (rendered via <img>) and video formats (rendered via <video>).
var binaryExtensions = map[string]string{
	// Images
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".tiff": "image/tiff",
	".tif":  "image/tiff",
	".avif": "image/avif",
	// Videos (browser-friendly formats)
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".ogg":  "video/ogg",
	".m4v":  "video/mp4",
}

// handleFileGet handles GET requests to read file content
func (s *Server) handleFileGet(w http.ResponseWriter, absPath string) {
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.fileNotFound"))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to read file: %v", err))
		return
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	if mimeType, ok := binaryExtensions[ext]; ok {
		// Binary file (image/video): return base64-encoded content with MIME type so
		// the frontend can render it via a data URL instead of showing
		// garbled binary text.
		encoded := base64.StdEncoding.EncodeToString(content)
		writeJSON(w, http.StatusOK, map[string]string{
			"content":  encoded,
			"path":     absPath,
			"mimeType": mimeType,
			"isBinary": "true",
		})
		return
	}

	// Return JSON format for compatibility with frontend (expects data.content)
	writeJSON(w, http.StatusOK, map[string]string{
		"content": string(content),
		"path":    absPath,
	})
}

// ====== GET /api/file/raw - Stream raw file content ======

// handleWebFileRaw streams the raw bytes of a file directly, with proper
// Content-Type and Content-Length headers. This is used by the frontend
// for <video> / <audio> elements that cannot load from base64 data URLs.
func (s *Server) handleWebFileRaw(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	// Security check: prevent path traversal
	if strings.Contains(filePath, "..") {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidPath"))
		return
	}

	absPath := s.resolveFilePath(filePath, r.URL.Query().Get("project_id"))

	// Stat the file to get size and handle range requests
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.fileNotFound"))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to stat file: %v", err))
		return
	}

	// Determine Content-Type from extension
	ext := strings.ToLower(filepath.Ext(absPath))
	contentType := "application/octet-stream"
	if mimeType, ok := binaryExtensions[ext]; ok {
		contentType = mimeType
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Accept-Ranges", "bytes")

	// Handle HTTP Range requests (essential for video seeking)
	http.ServeFile(w, r, absPath)
}

// handleFileWrite handles POST requests to write file content
func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request, absPath string) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Use the path from request body if provided, otherwise use absPath from query
	targetPath := absPath
	if req.Path != "" {
		// If request body has a path, resolve it relative to the project
		targetPath = s.resolveFilePath(req.Path, r.URL.Query().Get("project_id"))
	}

	// Ensure directory exists
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to create directory: %v", err))
		return
	}

	// Write file
	if err := os.WriteFile(targetPath, []byte(req.Content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to write file: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "File saved",
		"path":    targetPath,
	})
}

// handleFileDelete handles DELETE requests to delete a file
func (s *Server) handleFileDelete(w http.ResponseWriter, absPath string) {
	// Check if file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.fileNotFound"))
		return
	}

	// Delete file
	if err := os.Remove(absPath); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("Failed to delete file: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "File deleted",
		"path":    absPath,
	})
}

// ====== GET /api/nodes/:id/neighbors - Get node neighbors (for on-demand edge loading) ======

func (s *Server) handleWebNodeNeighbors(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	// Get node ID from URL
	nodeIDStr := params.ByName("id")
	if nodeIDStr == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.nodeIdRequired"))
		return
	}

	nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidNodeId"))
		return
	}

	// Optional parameters
	depthStr := r.URL.Query().Get("depth")
	depth := 1 // default depth
	if depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil && d > 0 && d <= 3 {
			depth = d
		}
	}

	edgeTypesFilter := parseCommaSeparatedList(r.URL.Query().Get("edge_types"))
	excludeEdgeTypes := parseCommaSeparatedList(r.URL.Query().Get("exclude_edge_types"))
	direction := r.URL.Query().Get("direction") // "incoming", "outgoing", "both" (default: "both")

	// Optional project_id parameter — route to the correct project DB
	projectIDStr := r.URL.Query().Get("project_id")
	neighborDB := s.compatDB(projectIDStr)
	if neighborDB == nil {
		writeJSON(w, http.StatusOK, WebGraphData{
			Nodes:         []WebGraphNode{},
			Relationships: []WebGraphRelationship{},
		})
		return
	}
	_ = projectIDStr // physical isolation: no project_id column in nodes/edges

	// Convert filters to sets
	edgeTypesSet := stringSliceToSet(edgeTypesFilter)
	excludeEdgeTypesSet := stringSliceToSet(excludeEdgeTypes)

	data := WebGraphData{
		Nodes:         make([]WebGraphNode, 0),
		Relationships: make([]WebGraphRelationship, 0),
	}

	// Track visited nodes and edges
	visitedNodes := make(map[int64]bool)
	visitedEdges := make(map[int64]bool)
	visitedNodes[nodeID] = true

	// BFS to find neighbors
	currentLevel := []int64{nodeID}
	fileSet := make(map[string]bool)

	for d := 0; d < depth; d++ {
		nextLevel := make([]int64, 0)

		for _, currentID := range currentLevel {
			// Build edge query based on direction
			var edgeQuery strings.Builder
			var edgeArgs []interface{}

			edgeQuery.WriteString(`SELECT id, source_id, target_id, kind, confidence FROM edges WHERE `)

			switch direction {
			case "incoming":
				edgeQuery.WriteString(`target_id = ?`)
				edgeArgs = append(edgeArgs, currentID)
			case "outgoing":
				edgeQuery.WriteString(`source_id = ?`)
				edgeArgs = append(edgeArgs, currentID)
			default: // "both"
				edgeQuery.WriteString(`(source_id = ? OR target_id = ?)`)
				edgeArgs = append(edgeArgs, currentID, currentID)
			}

			// Add edge type filter (include)
			if edgeTypesSet != nil {
				edgeQuery.WriteString(` AND kind IN (`)
				for i, kind := range edgeTypesFilter {
					if i > 0 {
						edgeQuery.WriteString(`,`)
					}
					edgeQuery.WriteString(`?`)
					edgeArgs = append(edgeArgs, kind)
				}
				edgeQuery.WriteString(`)`)
			}

			// Add edge type filter (exclude)
			if excludeEdgeTypesSet != nil {
				edgeQuery.WriteString(` AND kind NOT IN (`)
				for i, kind := range excludeEdgeTypes {
					if i > 0 {
						edgeQuery.WriteString(`,`)
					}
					edgeQuery.WriteString(`?`)
					edgeArgs = append(edgeArgs, kind)
				}
				edgeQuery.WriteString(`)`)
			}

			edgeRows, err := neighborDB.Query(edgeQuery.String(), edgeArgs...)
			if err != nil {
				continue
			}

			for edgeRows.Next() {
				var edgeID, sourceID, targetID int64
				var kind string
				var confidence sql.NullFloat64

				if err := edgeRows.Scan(&edgeID, &sourceID, &targetID, &kind, &confidence); err != nil {
					continue
				}

				// Skip already visited edges
				if visitedEdges[edgeID] {
					continue
				}
				visitedEdges[edgeID] = true

				// Add edge to result
				rel := WebGraphRelationship{
					ID:       strconv.FormatInt(edgeID, 10),
					Type:     edgeKindToWebType(kind),
					SourceID: strconv.FormatInt(sourceID, 10),
					TargetID: strconv.FormatInt(targetID, 10),
				}
				if confidence.Valid {
					rel.Confidence = confidence.Float64
				}
				data.Relationships = append(data.Relationships, rel)

				// Add neighbor to next level
				neighborID := sourceID
				if sourceID == currentID {
					neighborID = targetID
				}

				if !visitedNodes[neighborID] {
					visitedNodes[neighborID] = true
					nextLevel = append(nextLevel, neighborID)
				}
			}
			edgeRows.Close()
		}

		currentLevel = nextLevel
	}

	// Fetch node details for all visited nodes
	if len(visitedNodes) > 1 {
		nodeIDs := make([]interface{}, 0, len(visitedNodes)-1)
		for id := range visitedNodes {
			if id != nodeID { // Exclude the starting node
				nodeIDs = append(nodeIDs, id)
			}
		}

		if len(nodeIDs) > 0 {
			placeholders := make([]string, len(nodeIDs))
			for i := range nodeIDs {
				placeholders[i] = "?"
			}

			nodeQuery := fmt.Sprintf(`
				SELECT id, name, kind, file, line, end_line, qualified_name, exported, visibility, role
				FROM nodes
				WHERE id IN (%s)
			`, strings.Join(placeholders, ","))

			nodeRows, err := neighborDB.Query(nodeQuery, nodeIDs...)
			if err == nil {
				defer nodeRows.Close()

				for nodeRows.Next() {
					var id int64
					var name, kind, file, qualifiedName, visibility, role string
					var line, endLine int
					var exported bool
					var endLineNull sql.NullInt64

					if err := nodeRows.Scan(&id, &name, &kind, &file, &line, &endLineNull, &qualifiedName, &exported, &visibility, &role); err != nil {
						continue
					}

					if endLineNull.Valid {
						endLine = int(endLineNull.Int64)
					}

					node := WebGraphNode{
						ID:    strconv.FormatInt(id, 10),
						Label: symbolKindToWebLabel(kind),
						Properties: map[string]interface{}{
							"name":          name,
							"filePath":      file,
							"startLine":     line,
							"endLine":       endLine,
							"qualifiedName": qualifiedName,
							"exported":      exported,
							"visibility":    visibility,
							"role":          role,
						},
					}
					data.Nodes = append(data.Nodes, node)

					if file != "" {
						fileSet[file] = true
					}
				}
			}
		}
	}

	// Include stats
	data.Stats = &WebGraphStats{
		ReturnedNodes:     len(data.Nodes),
		ReturnedEdges:     len(data.Relationships),
		FilteredConnected: false,
	}

	writeJSON(w, http.StatusOK, data)
}

// ====== GET /api/graph/drilldown - Drill-down into a node's sub-graph ======

func (s *Server) handleWebGraphDrilldown(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	// Required: node_id (qualified name or DB ID)
	nodeIDStr := r.URL.Query().Get("node_id")
	qualifiedName := r.URL.Query().Get("qualified_name")
	if nodeIDStr == "" && qualifiedName == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "node_id or qualified_name is required")
		return
	}

	// Optional: hops (default 1, max 3)
	hopsStr := r.URL.Query().Get("hops")
	hops := 1
	if hopsStr != "" {
		if h, err := strconv.Atoi(hopsStr); err == nil && h > 0 && h <= 3 {
			hops = h
		}
	}

	// Optional: level filter (structure/class/function/full)
	level := r.URL.Query().Get("level")

	// Optional: project_id
	projectIDStr := r.URL.Query().Get("project_id")
	graphDB := s.compatDB(projectIDStr)
	if graphDB == nil {
		writeJSON(w, http.StatusOK, WebGraphData{
			Nodes:         []WebGraphNode{},
			Relationships: []WebGraphRelationship{},
		})
		return
	}

	// Find the starting node
	var startNodeID int64
	if nodeIDStr != "" {
		id, err := strconv.ParseInt(nodeIDStr, 10, 64)
		if err == nil {
			startNodeID = id
		}
	}
	if startNodeID == 0 && qualifiedName != "" {
		var id int64
		err := graphDB.QueryRow("SELECT id FROM nodes WHERE qualified_name = ? LIMIT 1", qualifiedName).Scan(&id)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Node with qualified_name '%s' not found", qualifiedName))
			return
		}
		startNodeID = id
	}
	if startNodeID == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Node not found")
		return
	}

	// Build kind filter from level
	var allowedKinds []string
	switch level {
	case "structure":
		allowedKinds = []string{"Package", "Module", "File", "Class", "Interface", "Struct"}
	case "class":
		allowedKinds = []string{"Package", "Module", "File", "Class", "Interface", "Struct", "Method", "Function", "Constructor"}
	case "function":
		// All kinds
	default:
		allowedKinds = nil // no filter
	}

	// BFS to find nodes within hops
	visitedNodes := map[int64]bool{startNodeID: true}
	visitedEdges := map[int64]bool{}
	currentLevel := []int64{startNodeID}

	for h := 0; h < hops; h++ {
		nextLevel := make([]int64, 0)
		for _, currentID := range currentLevel {
			// Find all edges connected to this node
			rows, err := graphDB.Query(`
				SELECT e.id, e.source_id, e.target_id, e.kind, e.confidence
				FROM edges e
				WHERE e.source_id = ? OR e.target_id = ?
			`, currentID, currentID)
			if err != nil {
				continue
			}
			for rows.Next() {
				var edgeID, sourceID, targetID int64
				var kind string
				var confidence sql.NullFloat64
				if err := rows.Scan(&edgeID, &sourceID, &targetID, &kind, &confidence); err != nil {
					continue
				}
				if visitedEdges[edgeID] {
					continue
				}
				visitedEdges[edgeID] = true

				neighborID := sourceID
				if sourceID == currentID {
					neighborID = targetID
				}
				if !visitedNodes[neighborID] {
					visitedNodes[neighborID] = true
					nextLevel = append(nextLevel, neighborID)
				}
			}
			rows.Close()
		}
		currentLevel = nextLevel
	}

	// Build response: fetch node details
	data := WebGraphData{
		Nodes:         make([]WebGraphNode, 0),
		Relationships: make([]WebGraphRelationship, 0),
	}

	nodeIDs := make([]interface{}, 0, len(visitedNodes))
	for id := range visitedNodes {
		nodeIDs = append(nodeIDs, id)
	}

	if len(nodeIDs) > 0 {
		placeholders := make([]string, len(nodeIDs))
		for i := range nodeIDs {
			placeholders[i] = "?"
		}
		nodeQuery := fmt.Sprintf(`
			SELECT id, name, kind, file, line, end_line, qualified_name, exported, visibility, role
			FROM nodes WHERE id IN (%s)
		`, strings.Join(placeholders, ","))

		rows, err := graphDB.Query(nodeQuery, nodeIDs...)
		if err == nil {
			for rows.Next() {
				var id int64
				var name, kind, file, qualifiedName, visibility, role string
				var line, endLine int
				var exported bool

				if err := rows.Scan(&id, &name, &kind, &file, &line, &endLine, &qualifiedName, &exported, &visibility, &role); err != nil {
					continue
				}

				// Apply level filter
				if allowedKinds != nil {
					found := false
					for _, ak := range allowedKinds {
						if kind == ak {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				}

				props := map[string]interface{}{
					"kind":           kind,
					"file":           file,
					"line":           line,
					"endLine":        endLine,
					"qualifiedName":  qualifiedName,
					"exported":       exported,
					"visibility":     visibility,
					"role":           role,
					"nodeType":       kind,
				}
				data.Nodes = append(data.Nodes, WebGraphNode{
					ID:         strconv.FormatInt(id, 10),
					Label:      name,
					Properties: props,
				})
			}
			rows.Close()
		}
	}

	// Fetch edge details
	edgeIDs := make([]interface{}, 0, len(visitedEdges))
	for id := range visitedEdges {
		edgeIDs = append(edgeIDs, id)
	}

	if len(edgeIDs) > 0 {
		placeholders := make([]string, len(edgeIDs))
		for i := range edgeIDs {
			placeholders[i] = "?"
		}
		edgeQuery := fmt.Sprintf(`
			SELECT id, source_id, target_id, kind, confidence, reason
			FROM edges WHERE id IN (%s)
		`, strings.Join(placeholders, ","))

		rows, err := graphDB.Query(edgeQuery, edgeIDs...)
		if err == nil {
			for rows.Next() {
				var id, sourceID, targetID int64
				var kind, reason string
				var confidence sql.NullFloat64
				if err := rows.Scan(&id, &sourceID, &targetID, &kind, &confidence, &reason); err != nil {
					continue
				}
				// Only include edges where both endpoints are in our node set (level filter)
				if visitedNodes[sourceID] && visitedNodes[targetID] {
					rel := WebGraphRelationship{
						ID:       strconv.FormatInt(id, 10),
						Type:     kind,
						SourceID: strconv.FormatInt(sourceID, 10),
						TargetID: strconv.FormatInt(targetID, 10),
						Reason:   reason,
					}
					if confidence.Valid {
						rel.Confidence = confidence.Float64
					}
					data.Relationships = append(data.Relationships, rel)
				}
			}
			rows.Close()
		}
	}

	data.Stats = &WebGraphStats{
		ReturnedNodes:     len(data.Nodes),
		ReturnedEdges:     len(data.Relationships),
		FilteredConnected: false,
	}

	writeJSON(w, http.StatusOK, data)
}

// scanAllFiles scans the filesystem for all files and adds File/Folder nodes.
// This uses the same blacklist logic as collect_files.go to skip unwanted files.
func scanAllFiles(rootPath string, normalizePath func(string) string, folderSet map[string]bool, fileNodeIDs map[string]bool, nodes *[]WebGraphNode) {
	// Directories to skip (same as collect_files.go)
	skipDirs := map[string]bool{
		".git": true, ".svn": true, ".hg": true,
		"node_modules": true, "vendor": true, "venv": true, ".venv": true,
		"dist": true, "build": true, "target": true, "out": true, "bin": true,
		"__pycache__": true, ".pytest_cache": true, ".mypy_cache": true,
		".cache": true, ".nyc_output": true, ".next": true, ".nuxt": true,
		".idea": true, ".vscode": true, ".vs": true, ".eclipse": true, ".settings": true,
		"coverage": true, ".coverage": true,
		".axons": true,
	}

	// File extensions to skip (binary/generated files)
	skipExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true, ".svg": true, ".webp": true,
		".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
		".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true, ".lib": true,
		".o": true, ".obj": true, ".class": true, ".jar": true, ".war": true,
		".zip": true, ".tar": true, ".gz": true, ".rar": true, ".7z": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
		".mp3": true, ".mp4": true, ".wav": true, ".avi": true, ".mov": true, ".mkv": true,
		".pyc": true, ".pyo": true, ".swp": true, ".swo": true,
	}

	// File names to skip
	skipFiles := map[string]bool{
		".DS_Store": true, "Thumbs.db": true, "desktop.ini": true, ".gitkeep": true,
	}

	filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		name := d.Name()

		if d.IsDir() {
			if skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip blacklisted files
		if skipFiles[name] {
			return nil
		}

		// Get file extension
		ext := strings.ToLower(filepath.Ext(path))

		// Skip blacklisted extensions
		if skipExts[ext] {
			return nil
		}

		// Skip minified files
		if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") {
			return nil
		}

		// Normalize path
		normalizedPath := normalizePath(path)
		if normalizedPath == "" {
			return nil
		}

		// Skip if already added
		fileNodeID := "file:" + normalizedPath
		if fileNodeIDs[fileNodeID] {
			return nil
		}

		// Add File node
		fileName := filepath.Base(normalizedPath)
		fileNodeIDs[fileNodeID] = true
		*nodes = append(*nodes, WebGraphNode{
			ID:    fileNodeID,
			Label: "File",
			Properties: map[string]interface{}{
				"name":     fileName,
				"path":     normalizedPath,
				"filePath": normalizedPath,
			},
		})

		// Add Folder nodes for all parent directories
		dir := filepath.Dir(normalizedPath)
		for dir != "" && dir != "." {
			if !folderSet[dir] {
				folderSet[dir] = true
				folderName := filepath.Base(dir)
				*nodes = append(*nodes, WebGraphNode{
					ID:    "folder:" + dir,
					Label: "Folder",
					Properties: map[string]interface{}{
						"name": folderName,
						"path": dir,
					},
				})
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}

		return nil
	})
}

// ====== POST /api/search - Search code (supports keyword, semantic, hybrid modes) ======

func (s *Server) handleWebSearch(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	var req struct {
		Query     string  `json:"query"`
		Limit     int     `json:"limit"`
		Mode      string  `json:"mode"`      // hybrid, semantic, keyword
		MinScore  float64 `json:"minScore"`  // Minimum similarity score
		Kind      string  `json:"kind"`      // Filter by symbol kind
		File      string  `json:"file"`      // Filter by file path pattern
		NoTests   bool    `json:"noTests"`   // Exclude test files
		ProjectID string  `json:"projectId"` // Optional project routing
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	// Also accept project_id from query string
	if req.ProjectID == "" {
		req.ProjectID = r.URL.Query().Get("project_id")
	}

	// Set defaults
	mode := req.Mode
	if mode == "" {
		mode = "keyword" // Default to keyword for backward compatibility
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.2
	}

	// Route to the correct project repo
	searchRepo := s.repo
	if pdb := s.compatDB(req.ProjectID); pdb != nil {
		searchRepo = repository.New(pdb)
	}

	// For keyword mode, use simple repository search
	if mode == "keyword" {
		nodes, err := searchRepo.SearchNodes(req.Query, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SEARCH_ERROR", fmt.Sprintf("Search failed: %v", err))
			return
		}

		results := make([]WebSearchResult, len(nodes))
		for i, node := range nodes {
			results[i] = WebSearchResult{
				ID:        fmt.Sprintf("%d", node.ID),
				Name:      node.Name,
				Type:      symbolKindToWebLabel(string(node.Kind)),
				FilePath:  node.File,
				StartLine: node.Line,
				EndLine:   node.EndLine,
				Score:     1.0 - float64(i)*0.01,
				Kind:      string(node.Kind),
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"results": results,
			"count":   len(results),
		})
		return
	}

	// For semantic/hybrid mode, create embedder
	embedder := s.createEmbedder()

	// Semantic or hybrid search
	ctx := r.Context()

	// Get query embedding
	vectors, err := embedder.Embed(ctx, []string{req.Query})
	if err != nil || len(vectors) == 0 {
		// Fall back to keyword search
		nodes, ferr := searchRepo.SearchNodes(req.Query, limit)
		if ferr != nil {
			writeError(w, http.StatusInternalServerError, "SEARCH_ERROR", fmt.Sprintf("Search failed: %v", ferr))
			return
		}

		results := make([]WebSearchResult, len(nodes))
		for i, node := range nodes {
			results[i] = WebSearchResult{
				ID:        fmt.Sprintf("%d", node.ID),
				Name:      node.Name,
				Type:      symbolKindToWebLabel(string(node.Kind)),
				FilePath:  node.File,
				StartLine: node.Line,
				EndLine:   node.EndLine,
				Score:     1.0 - float64(i)*0.01,
				Kind:      string(node.Kind),
			}
		}

		msg := "Embedding failed. Showing keyword results only."
		if err != nil {
			msg = fmt.Sprintf("Embedding failed: %v. Showing keyword results only.", err)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"results": results,
			"count":   len(results),
			"message": msg,
		})
		return
	}

	// Perform semantic search
	semResults, err := searchRepo.SemanticSearch(vectors[0], limit*2, float32(minScore))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SEARCH_ERROR", fmt.Sprintf("Semantic search failed: %v", err))
		return
	}

	// Apply filters and convert results
	results := make([]WebSearchResult, 0, len(semResults))
	for i, sr := range semResults {
		// Apply kind filter
		if req.Kind != "" && sr.Kind != req.Kind {
			continue
		}
		// Apply file filter
		if req.File != "" && !strings.Contains(sr.File, req.File) {
			continue
		}
		// Apply test filter
		if req.NoTests && isTestFile(sr.File) {
			continue
		}

		results = append(results, WebSearchResult{
			ID:           fmt.Sprintf("%d", sr.NodeID),
			Name:         sr.Name,
			Type:         symbolKindToWebLabel(sr.Kind),
			FilePath:     sr.File,
			StartLine:    sr.Line,
			EndLine:      sr.EndLine,
			Score:        float64(sr.Score),
			Kind:         sr.Kind,
			SemanticRank: i + 1,
		})

		if len(results) >= limit {
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

// ====== POST /api/impact - Real graph-based impact analysis ======

type WebImpactRequest struct {
	Target    string `json:"target"`
	Direction string `json:"direction"` // upstream | downstream
	MaxDepth  int    `json:"maxDepth"`
	File      string `json:"file,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

type WebImpactSymbol struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Label    string `json:"label"`
	Depth    int    `json:"depth"`
	FilePath string `json:"filePath"`
}

type WebImpactResponse struct {
	Target    string                    `json:"target"`
	Direction string                    `json:"direction"`
	Root      WebImpactSymbol           `json:"root"`
	Affected  []WebImpactSymbol         `json:"affected"`
	ByDepth   map[int][]WebImpactSymbol `json:"byDepth"`
	Total     int                       `json:"total"`
}

func (s *Server) handleWebImpact(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	var req WebImpactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if strings.TrimSpace(req.Target) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.targetRequired"))
		return
	}

	direction := req.Direction
	if direction == "" {
		direction = "upstream"
	}
	if direction != "upstream" && direction != "downstream" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.directionInvalid"))
		return
	}

	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}

	// Route to the correct project repo
	projectIDStr := r.URL.Query().Get("project_id")
	impactRepo := s.repo
	if pdb := s.compatDB(projectIDStr); pdb != nil {
		impactRepo = repository.New(pdb)
	}

	nodes, err := impactRepo.FindNodesByName(req.Target, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "QUERY_ERROR", fmt.Sprintf("Failed to find target symbol: %v", err))
		return
	}

	filtered := make([]*types.Node, 0, len(nodes))
	for _, n := range nodes {
		if req.Kind != "" && string(n.Kind) != req.Kind {
			continue
		}
		if req.File != "" && !strings.Contains(n.File, req.File) {
			continue
		}
		filtered = append(filtered, n)
	}

	if len(filtered) == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.targetSymbolNotFound"))
		return
	}

	root := filtered[0]
	for _, n := range filtered {
		if n.Name == req.Target {
			root = n
			break
		}
	}

	rootSymbol := WebImpactSymbol{
		ID:       fmt.Sprintf("%d", root.ID),
		Name:     root.Name,
		Label:    symbolKindToWebLabel(string(root.Kind)),
		Depth:    0,
		FilePath: root.File,
	}

	visited := map[int64]bool{root.ID: true}
	queue := []struct {
		id    int64
		depth int
	}{{id: root.ID, depth: 0}}

	affected := make([]WebImpactSymbol, 0)
	byDepth := make(map[int][]WebImpactSymbol)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth {
			continue
		}

		var next []*types.Node
		if direction == "downstream" {
			next, err = impactRepo.FindCallees(current.id)
		} else {
			next, err = impactRepo.FindCallers(current.id)
		}
		if err != nil {
			continue
		}

		for _, n := range next {
			if visited[n.ID] {
				continue
			}
			visited[n.ID] = true

			depth := current.depth + 1
			symbol := WebImpactSymbol{
				ID:       fmt.Sprintf("%d", n.ID),
				Name:     n.Name,
				Label:    symbolKindToWebLabel(string(n.Kind)),
				Depth:    depth,
				FilePath: n.File,
			}

			affected = append(affected, symbol)
			byDepth[depth] = append(byDepth[depth], symbol)

			queue = append(queue, struct {
				id    int64
				depth int
			}{id: n.ID, depth: depth})
		}
	}

	writeJSON(w, http.StatusOK, WebImpactResponse{
		Target:    req.Target,
		Direction: direction,
		Root:      rootSymbol,
		Affected:  affected,
		ByDepth:   byDepth,
		Total:     len(affected),
	})
}

// ====== POST /api/index - Index a repository ======

func (s *Server) handleWebIndex(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	var req WebIndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.pathParamRequired"))
		return
	}

	// Resolve absolute path
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("Invalid path: %v", err))
		return
	}

	// Check if a project with this root path already exists; reuse it if so.
	projectID := ""
	existingProjects, _ := s.globalRepo.ListProjects()
	for _, p := range existingProjects {
		if filepath.Clean(p.RootPath) == filepath.Clean(absPath) {
			projectID = p.ID
			break
		}
	}

	// Create a new project entry if not found
	if projectID == "" {
		projectID = uuid.New().String()
		projectName := filepath.Base(absPath)
		if _, err := s.globalRepo.CreateProject(projectID, projectName, absPath); err != nil {
			writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to create project: %v", err))
			return
		}
	}

	// Obtain the project-specific repository (creates the project DB file if needed)
	projRepo, err := s.projectRepo(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to open project DB: %v", err))
		return
	}

	// Create task for async build
	t := s.taskMgr.CreateTask("build")
	taskID := t.ID

	// Associate project ID with the task so build-status API can find it
	t.Labels = task.Map{"project_id": projectID}

	go func() {
		ctx := t.Context()
		t.SetStatus("running")

		opts := &types.BuildOptions{
			RootDir:   absPath,
			FullBuild: true,
		}

		pipeline := graph.NewPipelineWithGlobal(projRepo, s.globalRepo, opts)
		result, err := pipeline.Build(ctx)
		if err != nil {
			t.SetError(err.Error())
			return
		}

		// Write metadata into the project DB
		if pdb, dbErr := s.dbMgr.ProjectDB(projectID); dbErr == nil {
			now := time.Now().Format(time.RFC3339)
			pdb.Exec(`INSERT INTO metadata (key, value) VALUES ('last_index', ?) ON CONFLICT(key) DO UPDATE SET value = ?`, now, now)
			pdb.Exec(`INSERT INTO metadata (key, value) VALUES ('root_dir', ?) ON CONFLICT(key) DO UPDATE SET value = ?`, absPath, absPath)
		}

		t.SetResult(map[string]interface{}{
			"project_id":    projectID,
			"files_parsed":  result.FilesParsed,
			"nodes_created": result.NodesCreated,
			"edges_created": result.EdgesCreated,
		})
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"task_id":    taskID,
		"status":     "pending",
		"project_id": projectID,
	})
}

// ====== GET /api/repo - Get repository info ======

func (s *Server) handleWebRepo(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	// Optional project_id — route to the correct project DB
	projectIDStr := r.URL.Query().Get("project_id")
	repoInfoDB := s.compatDB(projectIDStr)
	if repoInfoDB == nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", i18n.T("api.error.noProjectDb"))
		return
	}

	// Get the root directory from project metadata
	var rootDir string
	repoInfoDB.QueryRow(`SELECT value FROM metadata WHERE key = 'root_dir'`).Scan(&rootDir)
	if rootDir == "" {
		rootDir = "."
	}

	absPath, err := filepath.Abs(rootDir)
	if err != nil {
		absPath = rootDir
	}

	var indexedAt string
	repoInfoDB.QueryRow(`SELECT value FROM metadata WHERE key = 'last_index'`).Scan(&indexedAt)

	repo := WebRepoInfo{
		Name:      filepath.Base(absPath),
		Path:      absPath,
		IndexedAt: indexedAt,
		Stats:     &WebRepoStats{},
	}

	// Count stats from project DB
	repoInfoDB.QueryRow(`SELECT COUNT(*) FROM nodes`).Scan(&repo.Stats.Nodes)
	repoInfoDB.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&repo.Stats.Edges)
	repo.Stats.Symbols = repo.Stats.Nodes
	repoInfoDB.QueryRow(`SELECT COUNT(*) FROM edges WHERE kind = 'calls'`).Scan(&repo.Stats.Calls)
	repoInfoDB.QueryRow(`SELECT COUNT(*) FROM edges WHERE kind IN ('imports', 'imports-type')`).Scan(&repo.Stats.Imports)
	repoInfoDB.QueryRow(`SELECT COUNT(DISTINCT file) FROM nodes`).Scan(&repo.Stats.Files)

	writeJSON(w, http.StatusOK, repo)
}

// ====== POST /api/clone - Clone remote repository ======

// CloneRequest represents a clone request.
type CloneRequest struct {
	RemoteURL string `json:"remote_url"` // Remote repository URL
	Branch    string `json:"branch"`     // Optional: specify branch
	CloneMode string `json:"clone_mode"` // "managed" or "custom"
	Workspace string `json:"workspace"`  // Custom workspace path
}

// CloneResponse represents a clone response.
type CloneResponse struct {
	Success   bool   `json:"success"`
	LocalPath string `json:"local_path"` // Local path
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Managed   bool   `json:"managed"`
	Branch    string `json:"branch"`
	Error     string `json:"error,omitempty"`
}

// handleClone handles remote repository cloning.
func (s *Server) handleClone(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", i18n.T("api.error.methodNotAllowed"))
		return
	}

	var req CloneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.RemoteURL == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.remoteUrlRequired"))
		return
	}

	// Set defaults
	if req.CloneMode == "" {
		req.CloneMode = "managed"
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	// Execute clone
	cloneSvc := service.NewCloneService(s.config)
	result, err := cloneSvc.Clone(r.Context(), req.RemoteURL, req.Branch, req.CloneMode, req.Workspace)
	if err != nil {
		writeJSON(w, http.StatusOK, CloneResponse{
			Success:   false,
			Error:     err.Error(),
			LocalPath: req.Workspace, // Return request info for debugging
			Branch:    req.Branch,
		})
		return
	}

	// Return clone result
	writeJSON(w, http.StatusOK, CloneResponse{
		Success:   true,
		LocalPath: result.LocalPath,
		Name:      result.Name,
		Provider:  result.Provider,
		Managed:   result.Managed,
		Branch:    result.Branch,
	})
}

