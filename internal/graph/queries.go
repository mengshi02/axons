// Package graph provides code graph building and querying capabilities.
package graph

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// QueryService provides high-level query operations for the code graph.
type QueryService struct {
	repo *repository.Repository
}

// NewQueryService creates a new QueryService.
func NewQueryService(repo *repository.Repository) *QueryService {
	return &QueryService{repo: repo}
}

// SymbolLookupResult represents the result of a symbol lookup.
type SymbolLookupResult struct {
	Node        *types.Node         `json:"node"`
	Callers     []*types.Node       `json:"callers,omitempty"`
	Callees     []*types.Node       `json:"callees,omitempty"`
	Definition  *types.Definition   `json:"definition,omitempty"`
	References  []*types.Node       `json:"references,omitempty"`
}

// SymbolLookup looks up a symbol by name.
func (qs *QueryService) SymbolLookup(ctx context.Context, name string, opts *types.QueryOptions) (*SymbolLookupResult, error) {
	nodes, err := qs.repo.FindNodesByName(name, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("failed to find nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, nil
	}

	// Take the first match or filter by kind/file
	var node *types.Node
	for _, n := range nodes {
		if opts.Kind != "" && n.Kind != opts.Kind {
			continue
		}
		if opts.File != "" && !strings.Contains(n.File, opts.File) {
			continue
		}
		if opts.NoTests && strings.Contains(n.File, "_test.") {
			continue
		}
		node = n
		break
	}

	if node == nil {
		node = nodes[0]
	}

	result := &SymbolLookupResult{
		Node: node,
	}

	// Get callers and callees
	callers, err := qs.repo.FindCallers(node.ID)
	if err == nil {
		result.Callers = callers
	}

	callees, err := qs.repo.FindCallees(node.ID)
	if err == nil {
		result.Callees = callees
	}

	return result, nil
}

// DependenciesResult represents the result of a dependencies query.
type DependenciesResult struct {
	Node         *types.Node   `json:"node"`
	DirectDeps   []*types.Node `json:"direct_dependencies"`
	TransitiveDeps []*types.Node `json:"transitive_dependencies"`
	DepCount     int           `json:"dependency_count"`
}

// GetDependencies gets all dependencies of a symbol.
func (qs *QueryService) GetDependencies(ctx context.Context, nodeID int64, maxDepth int) (*DependenciesResult, error) {
	node, err := qs.repo.FindNodeByID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find node: %w", err)
	}

	result := &DependenciesResult{
		Node: node,
	}

	// Get direct dependencies (callees)
	directDeps, err := qs.repo.FindCallees(nodeID)
	if err != nil {
		return nil, err
	}
	result.DirectDeps = directDeps

	// BFS for transitive dependencies
	if maxDepth > 1 {
		visited := make(map[int64]bool)
		transitive := []*types.Node{}
		queue := []struct {
			id    int64
			depth int
		}{}

		for _, dep := range directDeps {
			visited[dep.ID] = true
			queue = append(queue, struct {
				id    int64
				depth int
			}{dep.ID, 1})
		}

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			if current.depth >= maxDepth {
				continue
			}

			callees, err := qs.repo.FindCallees(current.id)
			if err != nil {
				continue
			}

			for _, callee := range callees {
				if !visited[callee.ID] {
					visited[callee.ID] = true
					transitive = append(transitive, callee)
					queue = append(queue, struct {
						id    int64
						depth int
					}{callee.ID, current.depth + 1})
				}
			}
		}

		result.TransitiveDeps = transitive
	}

	result.DepCount = len(result.DirectDeps) + len(result.TransitiveDeps)
	return result, nil
}

// DependentsResult represents the result of a dependents query.
type DependentsResult struct {
	Node           *types.Node   `json:"node"`
	DirectDependents []*types.Node `json:"direct_dependents"`
	TransitiveDependents []*types.Node `json:"transitive_dependents"`
	DependentCount int           `json:"dependent_count"`
}

// GetDependents gets all dependents (callers) of a symbol.
func (qs *QueryService) GetDependents(ctx context.Context, nodeID int64, maxDepth int) (*DependentsResult, error) {
	node, err := qs.repo.FindNodeByID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find node: %w", err)
	}

	result := &DependentsResult{
		Node: node,
	}

	// Get direct dependents (callers)
	directDependents, err := qs.repo.FindCallers(nodeID)
	if err != nil {
		return nil, err
	}
	result.DirectDependents = directDependents

	// BFS for transitive dependents
	if maxDepth > 1 {
		visited := make(map[int64]bool)
		transitive := []*types.Node{}
		queue := []struct {
			id    int64
			depth int
		}{}

		for _, dep := range directDependents {
			visited[dep.ID] = true
			queue = append(queue, struct {
				id    int64
				depth int
			}{dep.ID, 1})
		}

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			if current.depth >= maxDepth {
				continue
			}

			callers, err := qs.repo.FindCallers(current.id)
			if err != nil {
				continue
			}

			for _, caller := range callers {
				if !visited[caller.ID] {
					visited[caller.ID] = true
					transitive = append(transitive, caller)
					queue = append(queue, struct {
						id    int64
						depth int
					}{caller.ID, current.depth + 1})
				}
			}
		}

		result.TransitiveDependents = transitive
	}

	result.DependentCount = len(result.DirectDependents) + len(result.TransitiveDependents)
	return result, nil
}

// ImpactAnalysisResult represents the result of an impact analysis.
type ImpactAnalysisResult struct {
	Root          *types.Node   `json:"root"`
	ImpactRadius  int           `json:"impact_radius"`
	ImpactedNodes []*types.Node `json:"impacted_nodes"`
	TotalAffected int           `json:"total_affected"`
	ByDepth       map[int][]*types.Node `json:"by_depth"`
	ByRole        map[string][]*types.Node `json:"by_role"`
}

// ImpactAnalysis performs impact analysis for a symbol change.
func (qs *QueryService) ImpactAnalysis(ctx context.Context, nodeID int64, maxDepth int) (*ImpactAnalysisResult, error) {
	node, err := qs.repo.FindNodeByID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find node: %w", err)
	}

	result := &ImpactAnalysisResult{
		Root:          node,
		ImpactRadius:  maxDepth,
		ImpactedNodes: []*types.Node{},
		ByDepth:       make(map[int][]*types.Node),
		ByRole:        make(map[string][]*types.Node),
	}

	// BFS for all impacted nodes (reverse call graph)
	visited := make(map[int64]bool)
	visited[node.ID] = true
	queue := []struct {
		id    int64
		depth int
	}{{node.ID, 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth {
			continue
		}

		callers, err := qs.repo.FindCallers(current.id)
		if err != nil {
			continue
		}

		for _, caller := range callers {
			if !visited[caller.ID] {
				visited[caller.ID] = true
				result.ImpactedNodes = append(result.ImpactedNodes, caller)
				result.ByDepth[current.depth+1] = append(result.ByDepth[current.depth+1], caller)
				result.ByRole[string(caller.Role)] = append(result.ByRole[string(caller.Role)], caller)
				queue = append(queue, struct {
					id    int64
					depth int
				}{caller.ID, current.depth + 1})
			}
		}
	}

	result.TotalAffected = len(result.ImpactedNodes)
	return result, nil
}

// SearchResult represents a search result.
type SearchResult struct {
	Node      *types.Node `json:"node"`
	Score     float64     `json:"score"`
	MatchType string      `json:"match_type"`
}

// SearchSymbols searches for symbols matching a query.
func (qs *QueryService) SearchSymbols(ctx context.Context, query string, limit int) ([]*SearchResult, error) {
	nodes, err := qs.repo.SearchNodes(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search nodes: %w", err)
	}

	results := make([]*SearchResult, len(nodes))
	for i, node := range nodes {
		matchType := "partial"
		if node.Name == query {
			matchType = "exact"
		} else if strings.HasPrefix(node.Name, query) {
			matchType = "prefix"
		}

		score := 1.0
		if matchType == "exact" {
			score = 3.0
		} else if matchType == "prefix" {
			score = 2.0
		}
		if node.Exported {
			score += 0.5
		}

		results[i] = &SearchResult{
			Node:      node,
			Score:     score,
			MatchType: matchType,
		}
	}

	return results, nil
}

// FileAnalysisResult represents the result of file analysis.
type FileAnalysisResult struct {
	File        string        `json:"file"`
	Nodes       []*types.Node `json:"nodes"`
	Exports     []*types.Node `json:"exports"`
	Imports     []*types.Node `json:"imports"`
	NodeCount   int           `json:"node_count"`
	ExportCount int           `json:"export_count"`
}

// AnalyzeFile analyzes a single file.
func (qs *QueryService) AnalyzeFile(ctx context.Context, filePath string) (*FileAnalysisResult, error) {
	nodes, err := qs.repo.FindNodesByFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to find nodes: %w", err)
	}

	exports := []*types.Node{}
	for _, node := range nodes {
		if node.Exported {
			exports = append(exports, node)
		}
	}

	return &FileAnalysisResult{
		File:        filePath,
		Nodes:       nodes,
		Exports:     exports,
		NodeCount:   len(nodes),
		ExportCount: len(exports),
	}, nil
}

// OverallStats represents overall graph statistics.
type OverallStats struct {
	TotalNodes   int64            `json:"total_nodes"`
	TotalEdges   int64            `json:"total_edges"`
	TotalFiles   int64            `json:"total_files"`
	NodesByKind  map[string]int64 `json:"nodes_by_kind"`
	EdgesByKind  map[string]int64 `json:"edges_by_kind"`
	NodesByRole  map[string]int64 `json:"nodes_by_role"`
}

// GetOverallStats returns overall graph statistics.
func (qs *QueryService) GetOverallStats(ctx context.Context) (*OverallStats, error) {
	stats, err := qs.repo.GetStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	result := &OverallStats{
		TotalNodes: stats.TotalNodes,
		TotalEdges: stats.TotalEdges,
		TotalFiles: stats.TotalFiles,
	}

	// Convert typed maps to string maps
	result.NodesByKind = make(map[string]int64)
	for k, v := range stats.NodesByKind {
		result.NodesByKind[string(k)] = v
	}

	result.EdgesByKind = make(map[string]int64)
	for k, v := range stats.EdgesByKind {
		result.EdgesByKind[string(k)] = v
	}

	result.NodesByRole = make(map[string]int64)
	for k, v := range stats.NodesByRole {
		result.NodesByRole[string(k)] = v
	}

	return result, nil
}

// CallChainResult represents the result of a call chain query.
type CallChainResult struct {
	From    *types.Node   `json:"from"`
	To      *types.Node   `json:"to"`
	Paths   [][]*types.Node `json:"paths"`
	Found   bool          `json:"found"`
}

// FindCallChain finds all call chains between two symbols.
// Uses DFS with path tracking to find multiple paths up to maxDepth.
func (qs *QueryService) FindCallChain(ctx context.Context, fromID, toID int64, maxDepth int) (*CallChainResult, error) {
	from, err := qs.repo.FindNodeByID(fromID)
	if err != nil {
		return nil, fmt.Errorf("failed to find from node: %w", err)
	}

	to, err := qs.repo.FindNodeByID(toID)
	if err != nil {
		return nil, fmt.Errorf("failed to find to node: %w", err)
	}

	result := &CallChainResult{
		From:  from,
		To:    to,
		Found: false,
		Paths: [][]*types.Node{},
	}

	// DFS to find all paths
	var allPaths [][]int64
	var currentPath []int64
	visited := make(map[int64]bool)

	var dfs func(nodeID int64, depth int)
	dfs = func(nodeID int64, depth int) {
		if depth > maxDepth {
			return
		}

		currentPath = append(currentPath, nodeID)

		if nodeID == toID && depth > 0 {
			// Found a path - make a copy
			pathCopy := make([]int64, len(currentPath))
			copy(pathCopy, currentPath)
			allPaths = append(allPaths, pathCopy)
			currentPath = currentPath[:len(currentPath)-1]
			return
		}

		visited[nodeID] = true

		callees, err := qs.repo.FindCallees(nodeID)
		if err == nil {
			for _, callee := range callees {
				if !visited[callee.ID] {
					dfs(callee.ID, depth+1)
				}
			}
		}

		visited[nodeID] = false
		currentPath = currentPath[:len(currentPath)-1]
	}

	dfs(fromID, 0)

	// Convert node ID paths to Node object paths
	for _, pathIDs := range allPaths {
		path := make([]*types.Node, len(pathIDs))
		for i, id := range pathIDs {
			node, _ := qs.repo.FindNodeByID(id)
			path[i] = node
		}
		result.Paths = append(result.Paths, path)
		result.Found = true
	}

	return result, nil
}

// InheritanceResult represents the result of an inheritance query.
type InheritanceResult struct {
	Node         *types.Node   `json:"node"`
	Extends      []*types.Node `json:"extends"`
	Implements   []*types.Node `json:"implements"`
	ExtendedBy   []*types.Node `json:"extended_by"`
	ImplementedBy []*types.Node `json:"implemented_by"`
}

// GetInheritance gets inheritance relationships for a class/interface.
func (qs *QueryService) GetInheritance(ctx context.Context, nodeID int64) (*InheritanceResult, error) {
	node, err := qs.repo.FindNodeByID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to find node: %w", err)
	}

	result := &InheritanceResult{
		Node: node,
	}

	// Get what this class extends/implements
	parents, err := qs.repo.FindInheritance(nodeID)
	if err == nil {
		for _, parent := range parents {
			if parent.Kind == types.SymbolKindClass || parent.Kind == types.SymbolKindStruct {
				result.Extends = append(result.Extends, parent)
			} else if parent.Kind == types.SymbolKindInterface {
				result.Implements = append(result.Implements, parent)
			}
		}
	}

	// Get what extends/implements this
	implementations, err := qs.repo.FindImplementations(nodeID)
	if err == nil {
		for _, impl := range implementations {
			if node.Kind == types.SymbolKindInterface {
				result.ImplementedBy = append(result.ImplementedBy, impl)
			} else {
				result.ExtendedBy = append(result.ExtendedBy, impl)
			}
		}
	}

	return result, nil
}

// DeadCodeResult represents the result of a dead code analysis.
type DeadCodeResult struct {
	DeadNodes    []*types.Node `json:"dead_nodes"`
	UnusedExports []*types.Node `json:"unused_exports"`
	Count        int           `json:"count"`
}

// FindDeadCode finds potentially dead code (nodes with no callers).
func (qs *QueryService) FindDeadCode(ctx context.Context) (*DeadCodeResult, error) {
	result := &DeadCodeResult{
		DeadNodes:     []*types.Node{},
		UnusedExports: []*types.Node{},
	}

	// Get all function/method nodes
	functions, err := qs.repo.ListFunctionNodes(10000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list functions: %w", err)
	}

	for _, fn := range functions {
		// Skip test functions
		if strings.Contains(fn.File, "_test.") {
			continue
		}

		callers, err := qs.repo.FindCallers(fn.ID)
		if err != nil {
			continue
		}

		if len(callers) == 0 {
			// No callers - potentially dead code
			if fn.Exported {
				// Exported but unused
				result.UnusedExports = append(result.UnusedExports, fn)
			} else if fn.Role != types.RoleEntry {
				// Not an entry point and not exported
				result.DeadNodes = append(result.DeadNodes, fn)
			}
		}
	}

	result.Count = len(result.DeadNodes) + len(result.UnusedExports)
	return result, nil
}

// FindDeadCodeByProject finds potentially dead code for a specific project.
// Deprecated: use FindDeadCode instead (physical isolation, no project_id filtering needed).
func (qs *QueryService) FindDeadCodeByProject(ctx context.Context, projectID int64) (*DeadCodeResult, error) {
	return qs.FindDeadCode(ctx)
}

// HotspotResult represents a code hotspot.
type HotspotResult struct {
	Node       *types.Node `json:"node"`
	FanIn      int         `json:"fan_in"`
	FanOut     int         `json:"fan_out"`
	CallCount  int         `json:"call_count"`
	Score      float64     `json:"score"`
}

// FindHotspots finds code hotspots (highly connected nodes).
func (qs *QueryService) FindHotspots(ctx context.Context, limit int) ([]*HotspotResult, error) {
	// Get all function/method nodes
	functions, err := qs.repo.ListFunctionNodes(10000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list functions: %w", err)
	}

	hotspots := []*HotspotResult{}
	for _, fn := range functions {
		callers, _ := qs.repo.FindCallers(fn.ID)
		callees, _ := qs.repo.FindCallees(fn.ID)

		fanIn := len(callers)
		fanOut := len(callees)

		// Calculate hotspot score
		score := float64(fanIn*2 + fanOut)
		if fn.Exported {
			score += 1.0
		}

		if score > 0 {
			hotspots = append(hotspots, &HotspotResult{
				Node:      fn,
				FanIn:     fanIn,
				FanOut:    fanOut,
				CallCount: fanIn,
				Score:     score,
			})
		}
	}

	// Sort by score descending
	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].Score > hotspots[j].Score
	})

	// Limit results
	if len(hotspots) > limit {
		hotspots = hotspots[:limit]
	}

	return hotspots, nil
}

// FindHotspotsByProject finds code hotspots for a specific project.
// Deprecated: use FindHotspots instead (physical isolation, no project_id filtering needed).
func (qs *QueryService) FindHotspotsByProject(ctx context.Context, projectID int64, limit int) ([]*HotspotResult, error) {
	return qs.FindHotspots(ctx, limit)
}