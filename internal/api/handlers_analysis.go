// Package api provides analysis-related HTTP API handlers.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/algorithms"
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/internal/i18n"
	"github.com/mengshi02/axons/pkg/types"
)

// ─────────────────────────────────────────────
//  Shared helper types
// ─────────────────────────────────────────────

// HotspotInfo represents a hotspot function.
type HotspotInfo struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	File      string  `json:"file"`
	Line      int     `json:"line"`
	FanIn     int     `json:"fan_in"`
	FanOut    int     `json:"fan_out"`
	Score     float64 `json:"score"`
	Exported  bool    `json:"exported"`
}

// DeadCodeEntry represents a dead code entry.
type DeadCodeEntry struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
}

// ─────────────────────────────────────────────
//  P0: Hotspots & Dead Code
// ─────────────────────────────────────────────

// handleHotspots handles hotspot analysis requests.
// GET /v1/analysis/hotspots?limit=20&project_id=<uuid>
func (s *Server) handleHotspots(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	repo, err := s.repoForProject(r.URL.Query().Get("project_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	qs := graph.NewQueryService(repo)
	hotspots, err := qs.FindHotspots(r.Context(), limit*3)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ANALYSIS_ERROR", fmt.Sprintf("Failed to find hotspots: %v", err))
		return
	}

	// Sort by score descending
	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].Score > hotspots[j].Score
	})

	results := make([]HotspotInfo, 0, limit)
	for _, h := range hotspots {
		if len(results) >= limit {
			break
		}
		results = append(results, HotspotInfo{
			ID:       h.Node.ID,
			Name:     h.Node.Name,
			Kind:     string(h.Node.Kind),
			File:     h.Node.File,
			Line:     h.Node.Line,
			FanIn:    h.FanIn,
			FanOut:   h.FanOut,
			Score:    h.Score,
			Exported: h.Node.Exported,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hotspots": results,
		"count":    len(results),
	})
}

// handleDeadCode handles dead code detection requests.
// GET /v1/analysis/deadcode?project_id=<uuid>
func (s *Server) handleDeadCode(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	repo, err := s.repoForProject(r.URL.Query().Get("project_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	qs := graph.NewQueryService(repo)
	result, err := qs.FindDeadCode(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ANALYSIS_ERROR", fmt.Sprintf("Failed to find dead code: %v", err))
		return
	}

	dead := make([]DeadCodeEntry, 0, len(result.DeadNodes))
	for _, n := range result.DeadNodes {
		dead = append(dead, DeadCodeEntry{
			ID:       n.ID,
			Name:     n.Name,
			Kind:     string(n.Kind),
			File:     n.File,
			Line:     n.Line,
			Exported: n.Exported,
		})
	}

	unusedExports := make([]DeadCodeEntry, 0, len(result.UnusedExports))
	for _, n := range result.UnusedExports {
		unusedExports = append(unusedExports, DeadCodeEntry{
			ID:       n.ID,
			Name:     n.Name,
			Kind:     string(n.Kind),
			File:     n.File,
			Line:     n.Line,
			Exported: true,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"dead_nodes":     dead,
		"unused_exports": unusedExports,
		"count":          result.Count,
	})
}

// ─────────────────────────────────────────────
//  P1: Graph Metrics / Communities / PageRank
// ─────────────────────────────────────────────

// GraphMetricsResponse wraps graph-level metrics for the frontend.
type GraphMetricsResponse struct {
	TotalNodes     int     `json:"total_nodes"`
	TotalEdges     int     `json:"total_edges"`
	AvgInDegree    float64 `json:"avg_in_degree"`
	AvgOutDegree   float64 `json:"avg_out_degree"`
	MaxInDegree    int     `json:"max_in_degree"`
	MaxOutDegree   int     `json:"max_out_degree"`
	Density        float64 `json:"density"`
	NumSCCs        int     `json:"num_sccs"`
	LargestSCCSize int     `json:"largest_scc_size"`
	NumCommunities int     `json:"num_communities"`
	Modularity     float64 `json:"modularity"`
	IsDAG          bool    `json:"is_dag"`
}

// handleGraphMetrics handles graph metric requests.
// GET /v1/graph/metrics?project_id=<uuid>
func (s *Server) handleGraphMetrics(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	nodes, edges, err := s.loadGraphDataByProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GRAPH_ERROR", fmt.Sprintf("Failed to load graph: %v", err))
		return
	}

	adapter := algorithms.NewGraphAdapter(nodes, edges)
	m := adapter.CalculateMetrics()

	writeJSON(w, http.StatusOK, &GraphMetricsResponse{
		TotalNodes:     m.TotalNodes,
		TotalEdges:     m.TotalEdges,
		AvgInDegree:    m.AvgInDegree,
		AvgOutDegree:   m.AvgOutDegree,
		MaxInDegree:    m.MaxInDegree,
		MaxOutDegree:   m.MaxOutDegree,
		Density:        m.Density,
		NumSCCs:        m.NumSCCs,
		LargestSCCSize: m.LargestSCCSize,
		NumCommunities: m.NumCommunities,
		Modularity:     m.Modularity,
		IsDAG:          m.IsDAG,
	})
}

// CommunityNodeInfo is a slim node summary for community API.
type CommunityNodeInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
}

// CommunityInfo represents a detected community.
type CommunityInfo struct {
	ID      int64               `json:"id"`
	Nodes   []CommunityNodeInfo `json:"nodes"`
	Size    int                 `json:"size"`
	Density float64             `json:"density"`
}

// handleGraphCommunities handles community detection requests.
// GET /v1/graph/communities?resolution=1.0&project_id=<uuid>
func (s *Server) handleGraphCommunities(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	resolution := 1.0
	if v := r.URL.Query().Get("resolution"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			resolution = f
		}
	}

	projectID := r.URL.Query().Get("project_id")

	nodes, edges, err := s.loadGraphDataByProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GRAPH_ERROR", fmt.Sprintf("Failed to load graph: %v", err))
		return
	}

	adapter := algorithms.NewGraphAdapter(nodes, edges)
	comms := adapter.LouvainCommunities(resolution)

	result := make([]CommunityInfo, 0, len(comms))
	for _, c := range comms {
		nodeInfos := make([]CommunityNodeInfo, 0, len(c.Nodes))
		for _, n := range c.Nodes {
			if n == nil {
				continue
			}
			nodeInfos = append(nodeInfos, CommunityNodeInfo{
				ID:   n.ID,
				Name: n.Name,
				Kind: string(n.Kind),
				File: n.File,
			})
		}
		if len(nodeInfos) == 0 {
			continue
		}
		result = append(result, CommunityInfo{
			ID:      c.ID,
			Nodes:   nodeInfos,
			Size:    c.Size,
			Density: c.Density,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"communities": result,
		"count":       len(result),
	})
}

// PageRankInfo represents PageRank result for one node.
type PageRankInfo struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	File     string  `json:"file"`
	PageRank float64 `json:"page_rank"`
}

// handleGraphPageRank handles PageRank calculation requests.
// GET /v1/graph/pagerank?limit=50&damping=0.85&project_id=<uuid>
func (s *Server) handleGraphPageRank(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	damping := 0.85
	if v := r.URL.Query().Get("damping"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			damping = f
		}
	}

	projectID := r.URL.Query().Get("project_id")

	nodes, edges, err := s.loadGraphDataByProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GRAPH_ERROR", fmt.Sprintf("Failed to load graph: %v", err))
		return
	}

	adapter := algorithms.NewGraphAdapter(nodes, edges)
	ranks := adapter.CalculatePageRank(damping, 50)

	// Sort by PageRank descending
	sort.Slice(ranks, func(i, j int) bool {
		return ranks[i].PageRank > ranks[j].PageRank
	})

	results := make([]PageRankInfo, 0, limit)
	for _, r := range ranks {
		if len(results) >= limit {
			break
		}
		if r.Node == nil {
			continue
		}
		results = append(results, PageRankInfo{
			ID:       r.NodeID,
			Name:     r.Node.Name,
			Kind:     string(r.Node.Kind),
			File:     r.Node.File,
			PageRank: r.PageRank,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rankings": results,
		"count":    len(results),
	})
}

// handleGraphCycles handles cycle detection requests.
// GET /v1/graph/cycles?project_id=<uuid>
func (s *Server) handleGraphCycles(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	nodes, edges, err := s.loadGraphDataByProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GRAPH_ERROR", fmt.Sprintf("Failed to load graph: %v", err))
		return
	}

	adapter := algorithms.NewGraphAdapter(nodes, edges)
	cycles := adapter.FindCycles()

	type cycleInfo struct {
		NodeIDs []int64              `json:"node_ids"`
		Nodes   []CommunityNodeInfo  `json:"nodes"`
		Length  int                  `json:"length"`
	}

	result := make([]cycleInfo, 0, len(cycles))
	for _, c := range cycles {
		nodeInfos := make([]CommunityNodeInfo, 0, len(c.Nodes))
		for _, n := range c.Nodes {
			if n == nil {
				continue
			}
			nodeInfos = append(nodeInfos, CommunityNodeInfo{
				ID:   n.ID,
				Name: n.Name,
				Kind: string(n.Kind),
				File: n.File,
			})
		}
		result = append(result, cycleInfo{
			NodeIDs: c.Path,
			Nodes:   nodeInfos,
			Length:  len(c.Nodes),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cycles": result,
		"count":  len(result),
	})
}

// ─────────────────────────────────────────────
//  P1: Impact Analysis & Call Chain
// ─────────────────────────────────────────────

// ImpactNodeInfo is a slim node summary for impact API.
type ImpactNodeInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// handleSymbolImpact handles impact analysis for a symbol.
// GET /v1/symbols/:id/impact?depth=3&project_id=<uuid>
func (s *Server) handleSymbolImpact(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	idStr := ps.ByName("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidSymbolId"))
		return
	}

	maxDepth := 3
	if v := r.URL.Query().Get("depth"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 10 {
			maxDepth = n
		}
	}

	repo, err := s.repoForProject(r.URL.Query().Get("project_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	qs := graph.NewQueryService(repo)
	result, err := qs.ImpactAnalysis(r.Context(), id, maxDepth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ANALYSIS_ERROR", fmt.Sprintf("Failed to analyze impact: %v", err))
		return
	}

	impacted := make([]ImpactNodeInfo, 0, len(result.ImpactedNodes))
	for _, n := range result.ImpactedNodes {
		impacted = append(impacted, ImpactNodeInfo{
			ID:   n.ID,
			Name: n.Name,
			Kind: string(n.Kind),
			File: n.File,
			Line: n.Line,
		})
	}

	// Convert ByDepth
	byDepth := make(map[string][]ImpactNodeInfo)
	for depth, ns := range result.ByDepth {
		key := fmt.Sprintf("%d", depth)
		infos := make([]ImpactNodeInfo, 0, len(ns))
		for _, n := range ns {
			infos = append(infos, ImpactNodeInfo{
				ID:   n.ID,
				Name: n.Name,
				Kind: string(n.Kind),
				File: n.File,
				Line: n.Line,
			})
		}
		byDepth[key] = infos
	}

	var rootInfo *ImpactNodeInfo
	if result.Root != nil {
		rootInfo = &ImpactNodeInfo{
			ID:   result.Root.ID,
			Name: result.Root.Name,
			Kind: string(result.Root.Kind),
			File: result.Root.File,
			Line: result.Root.Line,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"root":           rootInfo,
		"impacted_nodes": impacted,
		"total_affected": result.TotalAffected,
		"impact_radius":  result.ImpactRadius,
		"by_depth":       byDepth,
	})
}

// CallChainRequest represents a call chain query request.
type CallChainRequest struct {
	FromID    int64  `json:"from_id"`
	ToID      int64  `json:"to_id"`
	MaxDepth  int    `json:"max_depth,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// handleCallChain handles call chain query requests.
// POST /v1/callchain
func (s *Server) handleCallChain(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var req CallChainRequest
	if err := jsonDecode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidRequestBody"))
		return
	}

	if req.FromID == 0 || req.ToID == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.fromToIdRequired"))
		return
	}

	maxDepth := req.MaxDepth
	if maxDepth <= 0 || maxDepth > 10 {
		maxDepth = 5
	}

	repo, err := s.repoForProject(req.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}

	qs := graph.NewQueryService(repo)
	result, err := qs.FindCallChain(r.Context(), req.FromID, req.ToID, maxDepth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ANALYSIS_ERROR", fmt.Sprintf("Failed to find call chain: %v", err))
		return
	}

	// Convert paths
	type pathInfo struct {
		Nodes []ImpactNodeInfo `json:"nodes"`
	}
	paths := make([]pathInfo, 0, len(result.Paths))
	for _, path := range result.Paths {
		nodes := make([]ImpactNodeInfo, 0, len(path))
		for _, n := range path {
			if n == nil {
				continue
			}
			nodes = append(nodes, ImpactNodeInfo{
				ID:   n.ID,
				Name: n.Name,
				Kind: string(n.Kind),
				File: n.File,
				Line: n.Line,
			})
		}
		paths = append(paths, pathInfo{Nodes: nodes})
	}

	var fromInfo, toInfo *ImpactNodeInfo
	if result.From != nil {
		fromInfo = &ImpactNodeInfo{
			ID:   result.From.ID,
			Name: result.From.Name,
			Kind: string(result.From.Kind),
			File: result.From.File,
			Line: result.From.Line,
		}
	}
	if result.To != nil {
		toInfo = &ImpactNodeInfo{
			ID:   result.To.ID,
			Name: result.To.Name,
			Kind: string(result.To.Kind),
			File: result.To.File,
			Line: result.To.Line,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"from":  fromInfo,
		"to":    toInfo,
		"paths": paths,
		"found": result.Found,
		"count": len(paths),
	})
}

// ─────────────────────────────────────────────
//  CFG endpoint (enhanced)
// ─────────────────────────────────────────────

// handleCFGDetail returns CFG with dataflow for a node.
// GET /v1/symbols/:id/cfg?project_id=xxx
func (s *Server) handleCFGDetail(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	idStr := ps.ByName("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", i18n.T("api.error.invalidSymbolId"))
		return
	}

	// Use project-specific repo if project ID is provided
	cfgRepo := s.repo
	if projectID != "" {
		if pr, err := s.projectRepo(projectID); err == nil {
			cfgRepo = pr
		}
	}

	node, err := cfgRepo.FindNodeByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", i18n.T("api.error.symbolNotFound"))
		return
	}

	// Get AST nodes if available
	astNodes, _ := cfgRepo.FindASTNodesByParent(id)

	var cfgData interface{}
	if len(astNodes) > 0 {
		cfgData = buildCFGFromAST(astNodes)
	} else {
		cfgData = map[string]interface{}{
			"blocks": []interface{}{},
			"edges":  []interface{}{},
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"node": NodeInfo{
			ID:            node.ID,
			Name:          node.Name,
			Kind:          string(node.Kind),
			File:          node.File,
			Line:          node.Line,
			EndLine:       node.EndLine,
			QualifiedName: node.QualifiedName,
		},
		"cfg": cfgData,
	})
}

// buildCFGFromAST builds a simple CFG structure from AST nodes.
func buildCFGFromAST(astNodes []*types.AstNode) map[string]interface{} {
	blocks := make([]map[string]interface{}, 0)
	edges := make([]map[string]interface{}, 0)

	for i, n := range astNodes {
		block := map[string]interface{}{
			"index":      i,
			"type":       n.Kind,
			"start_line": n.Line,
			"end_line":   n.Line,
			"text":       n.Text,
		}
		blocks = append(blocks, block)

		if i > 0 {
			edges = append(edges, map[string]interface{}{
				"source": i - 1,
				"target": i,
				"kind":   "sequential",
			})
		}
	}

	return map[string]interface{}{
		"blocks": blocks,
		"edges":  edges,
	}
}

// ─────────────────────────────────────────────
//  Co-Change analysis (enhanced query)
// ─────────────────────────────────────────────

// handleCoChangeQuery returns co-change data for visualization.
// GET /v1/analysis/cochange?limit=50&min_count=3&project_id=<uuid>
func (s *Server) handleCoChangeQuery(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	projectID := r.URL.Query().Get("project_id")

	// Co-change data is project-specific, require project_id
	if projectID == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"co_changes": []interface{}{},
			"count":      0,
		})
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	minCount := 3
	if v := r.URL.Query().Get("min_count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			minCount = n
		}
	}
	fileFilter := r.URL.Query().Get("file")

	// Use project-specific repo
	coChangeRepo, err := s.projectRepo(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", fmt.Sprintf("Failed to get project database: %v", err))
		return
	}

	coChanges, err := coChangeRepo.GetTopCoChanges(limit*2, minCount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ANALYSIS_ERROR", fmt.Sprintf("Failed to get co-changes: %v", err))
		return
	}

	// Filter co-changes to only include files belonging to the project
	var projectFilePrefix string
	if project, err := s.globalRepo.GetProject(projectID); err == nil && project != nil {
		projectFilePrefix = project.RootPath
	}

	type coChangeInfo struct {
		FileA       string  `json:"file_a"`
		FileB       string  `json:"file_b"`
		CommitCount int     `json:"commit_count"`
		Jaccard     float64 `json:"jaccard"`
	}

	results := make([]coChangeInfo, 0, len(coChanges))
	for _, cc := range coChanges {
		if fileFilter != "" && !strings.Contains(cc.FileA, fileFilter) && !strings.Contains(cc.FileB, fileFilter) {
			continue
		}
		if projectFilePrefix != "" && !strings.HasPrefix(cc.FileA, projectFilePrefix) {
			continue
		}
		results = append(results, coChangeInfo{
			FileA:       cc.FileA,
			FileB:       cc.FileB,
			CommitCount: cc.CommitCount,
			Jaccard:     cc.Jaccard,
		})
		if len(results) >= limit {
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"co_changes": results,
		"count":      len(results),
	})
}

// ─────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────

// repoForProject returns a Repository for the given project UUID.
// If projectID is empty, it falls back to the main repo.
func (s *Server) repoForProject(projectID string) (*repository.Repository, error) {
	if projectID == "" {
		return s.repo, nil
	}
	return s.projectRepo(projectID)
}

// loadGraphDataByProject loads nodes and edges for a specific project.
// Uses physical isolation: each project has its own .db file.
func (s *Server) loadGraphDataByProject(projectID string) ([]*types.Node, []*types.Edge, error) {
	repo, err := s.repoForProject(projectID)
	if err != nil {
		return nil, nil, err
	}
	nodes, err := repo.ListAllNodes()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	edges, err := repo.ListAllEdges()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list edges: %w", err)
	}
	return nodes, edges, nil
}

// jsonDecode decodes JSON from request body.
func jsonDecode(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}