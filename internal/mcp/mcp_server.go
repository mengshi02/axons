// Package mcp provides MCP server implementation using the official Go SDK.
package mcp

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mengshi02/axons/internal/algorithms"
	"github.com/mengshi02/axons/internal/db"
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/internal/service"
	"github.com/mengshi02/axons/internal/version"
	"github.com/mengshi02/axons/pkg/clients/embedding"
	"github.com/mengshi02/axons/pkg/clients/reranker"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SearchResult represents a search result item.
type SearchResult struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	File          string  `json:"file"`
	Line          int     `json:"line"`
	EndLine       int     `json:"end_line"`
	QualifiedName string  `json:"qualified_name,omitempty"`
	Score         float32 `json:"score"`
	RRFScore      float32 `json:"rrf_score,omitempty"`
	BM25Rank      int     `json:"bm25_rank,omitempty"`
	SemanticRank  int     `json:"semantic_rank,omitempty"`
}

// RerankResult represents a reranked result.
type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float32 `json:"relevance_score"`
	Document       string  `json:"document"`
}

// SymbolResult represents a symbol result.
type SymbolResult struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	File          string `json:"file"`
	Line          int    `json:"line"`
	EndLine       int    `json:"end_line"`
	QualifiedName string `json:"qualified_name,omitempty"`
}

// StatsResult represents statistics result.
type StatsResult struct {
	TotalNodes  int64            `json:"total_nodes"`
	TotalEdges  int64            `json:"total_edges"`
	TotalFiles  int64            `json:"total_files"`
	NodesByKind map[string]int64 `json:"nodes_by_kind"`
	EdgesByKind map[string]int64 `json:"edges_by_kind"`
}

// SourceCodeResult represents source code result.
type SymbolSource struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	SourceCode string `json:"source_code"`
}

// EmbeddingStatusResult represents embedding status result.
type EmbeddingStatusResult struct {
	TotalSymbols    int64 `json:"total_symbols"`
	TotalEmbeddings int64 `json:"total_embeddings"`
	Pending         int64 `json:"pending"`
}

// MCPServer wraps the official MCP server with axons tools.
type MCPServer struct {
	server   *mcp.Server
	repo     *repository.Repository
	db       *sql.DB
	dbPath   string
	rootPath string // Project root directory for file operations
	embedder embedding.Embedder
	searchSvc *service.SearchService
}

// Server is an alias for MCPServer for backward compatibility.
type Server = MCPServer

// NewServer creates a new MCP server with database path (for standalone MCP mode).
// This is the primary constructor for CLI usage.
func NewServer(dbPath string) (*MCPServer, error) {
	// Open database connection
	database, err := db.NewConnection(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	repo := repository.New(database)

	// Create the MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "axons-code-graph",
		Version: version.Version,
	}, nil)

	s := &MCPServer{
		server: server,
		repo:   repo,
		db:     database,
		dbPath: dbPath,
	}

	// Register all tools
	s.registerTools()

	return s, nil
}

// NewMCPServer creates a new MCP server with an existing repository.
// This is useful for embedding in other services.
func NewMCPServer(repo *repository.Repository) *MCPServer {
	// Create the MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "axons-code-graph",
		Version: version.Version,
	}, nil)

	s := &MCPServer{
		server: server,
		repo:   repo,
	}

	// Register all tools
	s.registerTools()

	return s
}

// SetService sets the search service.
func (s *MCPServer) SetService(svc *service.SearchService) {
	s.searchSvc = svc
}

// SetRootPath sets the project root directory for file operations.
func (s *MCPServer) SetRootPath(path string) {
	s.rootPath = path
}

// registerTools registers all MCP tools.
func (s *MCPServer) registerTools() {
	// Search tools
	s.registerSearchTools()

	// Graph traversal tools
	s.registerGraphTools()

	// Analysis tools
	s.registerAnalysisTools()

	// Source code tools
	s.registerSourceTools()

	// CCE (Cognitive Context Engine) tools
	s.registerCCETools()
}

// registerSearchTools registers search-related tools.
func (s *MCPServer) registerSearchTools() {
	// keyword_search - FTS5 BM25 search
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "keyword_search",
		Description: "Perform full-text search using FTS5 with BM25 ranking. Fast keyword-based search for code symbols.",
	}, s.handleKeywordSearch)

	// hybrid_search - FTS5 + Vector + RRF
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "hybrid_search",
		Description: "Perform hybrid search combining FTS5 keyword search and semantic vector search with RRF fusion. Best for comprehensive code service.",
	}, s.handleHybridSearch)

	// semantic_search - Vector similarity search
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "semantic_search",
		Description: "Search for code using natural language queries. Finds semantically similar code based on meaning.",
	}, s.handleSemanticSearch)

	// rerank_results - Rerank search results
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "rerank_results",
		Description: "Rerank search results using a reranking model for improved relevance. Supports Cohere, Jina APIs, or local mock reranker.",
	}, s.handleRerankResults)

	// search_symbols - Simple symbol search
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "search_symbols",
		Description: "Search for symbols in the code graph by name pattern",
	}, s.handleSearchSymbols)
}

// registerGraphTools registers graph traversal tools.
func (s *MCPServer) registerGraphTools() {
	// get_symbol
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_symbol",
		Description: "Get detailed information about a symbol by ID",
	}, s.handleGetSymbol)

	// find_callers
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "find_callers",
		Description: "Find all functions that call a given symbol",
	}, s.handleFindCallers)

	// find_callees
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "find_callees",
		Description: "Find all functions called by a given symbol",
	}, s.handleFindCallees)

	// path - shortest path
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "path",
		Description: "Find shortest path between two symbols",
	}, s.handlePath)
}

// registerAnalysisTools registers analysis tools.
func (s *MCPServer) registerAnalysisTools() {
	// list_files
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_files",
		Description: "List all indexed files in the code graph",
	}, s.handleListFiles)

	// get_stats
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_stats",
		Description: "Get statistics about the code graph",
	}, s.handleGetStats)

	// find_dead_code
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "find_dead_code",
		Description: "Find potentially dead code (unused functions)",
	}, s.handleFindDeadCode)

	// find_hotspots
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "find_hotspots",
		Description: "Find code hotspots (highly connected functions)",
	}, s.handleFindHotspots)

	// find_impact - BFS impact analysis
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "find_impact",
		Description: "Find all symbols impacted by a change to the given symbol (reverse BFS traversal). Returns callers up to max_depth hops away.",
	}, s.handleFindImpact)

	// find_call_chain - BFS call chain between two symbols
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "find_call_chain",
		Description: "Find all call paths between two symbols using BFS. Returns every route from source to target.",
	}, s.handleFindCallChain)

	// get_complexity - cyclomatic/cognitive complexity
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_complexity",
		Description: "Get cyclomatic and cognitive complexity metrics for a function symbol.",
	}, s.handleGetComplexity)

	// get_cochanges - co-change coupling analysis
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_cochanges",
		Description: "Get file co-change pairs that frequently change together (coupling analysis based on git history).",
	}, s.handleGetCoChanges)

	// get_pagerank - PageRank importance ranking
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_pagerank",
		Description: "Get the top-N most important symbols ranked by PageRank algorithm on the call graph.",
	}, s.handleGetPageRank)

	// arch_check - Architecture rule validation
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "arch_check",
		Description: "Check the codebase against stored architecture deny rules. Returns violations where files in from_pattern depend on files in to_pattern.",
	}, s.handleArchCheck)

	// list_communities - Louvain community detection
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_communities",
		Description: "Detect module communities in the call graph using Louvain algorithm. Returns clusters of closely related symbols.",
	}, s.handleListCommunities)

	// get_modules - Top-level module overview
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_modules",
		Description: "List top-level modules or packages in the codebase with symbol counts. Useful for architecture overview.",
	}, s.handleGetModules)

	// get_node_by_file - Find symbols in a file
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_node_by_file",
		Description: "Find all symbols defined in a given file. Useful for exploring a specific file's contents.",
	}, s.handleGetNodeByFile)

	// list_processes - List materialized execution flows
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_processes",
		Description: "List materialized execution flows (processes) detected during build. Each process traces a multi-hop call chain from an entry point. Useful for understanding how features are implemented.",
	}, s.handleListProcesses)

	// get_process - Get a specific process with full step details
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_process",
		Description: "Get detailed steps of a specific execution flow (process). Returns ordered call chain with file locations. Use list_processes to find process IDs.",
	}, s.handleGetProcess)
}

// registerSourceTools registers source code tools.
func (s *MCPServer) registerSourceTools() {
	// get_source_code
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_source_code",
		Description: "Retrieve the source code content for one or more symbols by their IDs. Returns the actual code text.",
	}, s.handleGetSourceCode)

	// embedding_status
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "embedding_status",
		Description: "Get the status of code embeddings",
	}, s.handleEmbeddingStatus)

	// read_file - read raw file content by path
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "read_file",
		Description: "Read the raw content of any file by path. Supports optional line range. Useful for reading config files, SQL migrations, READMEs, and other non-indexed files.",
	}, s.handleReadFile)

	// smart_read - intelligent file reading based on size
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "smart_read",
		Description: "Intelligently read a file based on its size. For small files (<500 lines): reads entire content. For medium files (500-2000 lines): reads with smart truncation (head + tail). For large files (>2000 lines): returns outline and suggests using search tools.",
	}, s.handleSmartRead)

	// write_file - write content to a file
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "write_file",
		Description: "Write content to a file, creating parent directories if needed. Overwrites existing content. Path must be within the project root.",
	}, s.handleWriteFile)

	// run_command - execute a shell command
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "run_command",
		Description: "Run a shell command and return stdout, stderr, and exit code. Only an allowlist of commands is permitted (go, python, node, cargo, git, make, etc.).",
	}, s.handleRunCommand)
}

// GetServer returns the underlying MCP server.
func (s *MCPServer) GetServer() *mcp.Server {
	return s.server
}

// SetEmbedder sets the embedder for semantic service.
func (s *MCPServer) SetEmbedder(embedder embedding.Embedder) {
	s.embedder = embedder
}

// ============================================================================
// Search Tool Handlers
// ============================================================================

func (s *MCPServer) handleKeywordSearch(ctx context.Context, req *mcp.CallToolRequest, args KeywordSearchArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Query == "" {
		return nil, nil, fmt.Errorf("query is required")
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}

	// Use FTS5Search for keyword search
	results, err := s.repo.FTS5SearchWithFilter(args.Query, args.Kind, args.File, limit)
	if err != nil {
		return nil, nil, err
	}

	searchResults := make([]SearchResult, len(results))
	for i, r := range results {
		searchResults[i] = SearchResult{
			ID:            r.NodeID,
			Name:          r.Name,
			Kind:          r.Kind,
			File:          r.File,
			Line:          r.Line,
			EndLine:       r.EndLine,
			QualifiedName: r.QualifiedName,
			Score:         float32(r.BM25Score),
			BM25Rank:      i + 1,
		}
	}

	return nil, map[string]interface{}{
		"results": searchResults,
		"count":   len(searchResults),
	}, nil
}

func (s *MCPServer) handleHybridSearch(ctx context.Context, req *mcp.CallToolRequest, args HybridSearchArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Query == "" {
		return nil, nil, fmt.Errorf("query is required")
	}

	if s.searchSvc == nil {
		return nil, nil, fmt.Errorf("search service not initialized")
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}

	threshold := float32(args.Threshold)
	if threshold <= 0 {
		threshold = 0.2
	}

	// Create search request
	searchReq := &service.Request{
		Query:    args.Query,
		Mode:     service.ModeHybrid,
		Limit:    limit,
		MinScore: threshold,
		Kind:     args.Kind,
		File:     args.File,
	}

	// Set embedder if provider specified
	if args.Provider != "" {
		embedder := getEmbedder(args.Provider, args.Model)
		s.searchSvc.SetEmbedder(embedder)
	}

	resp, err := s.searchSvc.Search(ctx, searchReq)
	if err != nil {
		return nil, nil, err
	}

	searchResults := make([]SearchResult, len(resp.Results))
	for i, r := range resp.Results {
		searchResults[i] = SearchResult{
			ID:            r.ID,
			Name:          r.Name,
			Kind:          r.Kind,
			File:          r.File,
			Line:          r.Line,
			EndLine:       r.EndLine,
			QualifiedName: r.QualifiedName,
			Score:         r.Score,
			RRFScore:      r.RRFScore,
			BM25Rank:      r.BM25Rank,
			SemanticRank:  r.SemanticRank,
		}
	}

	result := map[string]interface{}{
		"results": searchResults,
		"count":   len(searchResults),
	}
	if resp.Message != "" {
		result["message"] = resp.Message
	}

	return nil, result, nil
}

func (s *MCPServer) handleSemanticSearch(ctx context.Context, req *mcp.CallToolRequest, args SemanticSearchArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Query == "" {
		return nil, nil, fmt.Errorf("query is required")
	}

	if s.searchSvc == nil {
		return nil, nil, fmt.Errorf("search service not initialized")
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}

	threshold := float32(args.Threshold)
	if threshold <= 0 {
		threshold = 0.5
	}

	// Create search request
	searchReq := &service.Request{
		Query:    args.Query,
		Mode:     service.ModeSemantic,
		Limit:    limit,
		MinScore: threshold,
		Kind:     args.Kind,
		File:     args.File,
	}

	// Set embedder if provider specified
	if args.Provider != "" {
		embedder := getEmbedder(args.Provider, args.Model)
		s.searchSvc.SetEmbedder(embedder)
	}

	resp, err := s.searchSvc.Search(ctx, searchReq)
	if err != nil {
		return nil, nil, err
	}

	searchResults := make([]SearchResult, len(resp.Results))
	for i, r := range resp.Results {
		searchResults[i] = SearchResult{
			ID:            r.ID,
			Name:          r.Name,
			Kind:          r.Kind,
			File:          r.File,
			Line:          r.Line,
			EndLine:       r.EndLine,
			QualifiedName: r.QualifiedName,
			Score:         r.Score,
			SemanticRank:  r.SemanticRank,
		}
	}

	result := map[string]interface{}{
		"results": searchResults,
		"count":   len(searchResults),
	}
	if resp.Message != "" {
		result["message"] = resp.Message
	}

	return nil, result, nil
}

func (s *MCPServer) handleRerankResults(ctx context.Context, req *mcp.CallToolRequest, args RerankArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Query == "" {
		return nil, nil, fmt.Errorf("query is required")
	}

	if len(args.Documents) == 0 {
		return nil, nil, fmt.Errorf("documents are required")
	}

	provider := args.Provider
	if provider == "" {
		provider = "mock"
	}

	reranker := getReranker(provider)
	if reranker == nil {
		return nil, nil, fmt.Errorf("unknown rerank provider: %s", provider)
	}

	results, err := reranker.Rerank(ctx, args.Query, args.Documents)
	if err != nil {
		return nil, nil, err
	}

	// Apply topN filter
	if args.TopN > 0 && args.TopN < len(results) {
		results = results[:args.TopN]
	}

	rerankResults := make([]RerankResult, len(results))
	for i, r := range results {
		rerankResults[i] = RerankResult{
			Index:          r.Index,
			RelevanceScore: r.Score,
			Document:       r.Document,
		}
	}

	return nil, map[string]interface{}{
		"results": rerankResults,
		"count":   len(rerankResults),
	}, nil
}

func (s *MCPServer) handleSearchSymbols(ctx context.Context, req *mcp.CallToolRequest, args SearchSymbolsArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Pattern == "" {
		return nil, nil, fmt.Errorf("pattern is required")
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}

	nodes, err := s.repo.FindNodesByName(args.Pattern, limit)
	if err != nil {
		return nil, nil, err
	}

	results := make([]SymbolResult, len(nodes))
	for i, node := range nodes {
		results[i] = SymbolResult{
			ID:            node.ID,
			Name:          node.Name,
			Kind:          string(node.Kind),
			File:          node.File,
			Line:          node.Line,
			EndLine:       node.EndLine,
			QualifiedName: node.QualifiedName,
		}
	}

	return nil, map[string]interface{}{
		"results": results,
		"count":   len(results),
	}, nil
}

// ============================================================================
// Graph Tool Handlers
// ============================================================================

func (s *MCPServer) handleGetSymbol(ctx context.Context, req *mcp.CallToolRequest, args GetSymbolArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	node, err := s.repo.FindNodeByID(args.ID)
	if err != nil {
		return nil, nil, err
	}

	result := SymbolResult{
		ID:            node.ID,
		Name:          node.Name,
		Kind:          string(node.Kind),
		File:          node.File,
		Line:          node.Line,
		EndLine:       node.EndLine,
		QualifiedName: node.QualifiedName,
	}

	return nil, map[string]interface{}{
		"symbol": result,
	}, nil
}

func (s *MCPServer) handleFindCallers(ctx context.Context, req *mcp.CallToolRequest, args FindCallersArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	nodes, err := s.repo.FindCallers(args.ID)
	if err != nil {
		return nil, nil, err
	}

	results := make([]SymbolResult, len(nodes))
	for i, node := range nodes {
		results[i] = SymbolResult{
			ID:            node.ID,
			Name:          node.Name,
			Kind:          string(node.Kind),
			File:          node.File,
			Line:          node.Line,
			EndLine:       node.EndLine,
			QualifiedName: node.QualifiedName,
		}
	}

	return nil, map[string]interface{}{
		"callers": results,
		"count":   len(results),
	}, nil
}

func (s *MCPServer) handleFindCallees(ctx context.Context, req *mcp.CallToolRequest, args FindCalleesArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	nodes, err := s.repo.FindCallees(args.ID)
	if err != nil {
		return nil, nil, err
	}

	results := make([]SymbolResult, len(nodes))
	for i, node := range nodes {
		results[i] = SymbolResult{
			ID:            node.ID,
			Name:          node.Name,
			Kind:          string(node.Kind),
			File:          node.File,
			Line:          node.Line,
			EndLine:       node.EndLine,
			QualifiedName: node.QualifiedName,
		}
	}

	return nil, map[string]interface{}{
		"callees": results,
		"count":   len(results),
	}, nil
}

func (s *MCPServer) handlePath(ctx context.Context, req *mcp.CallToolRequest, args PathArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	maxDepth := args.MaxDepth
	if maxDepth <= 0 || maxDepth > 10 {
		maxDepth = 6
	}

	qs := graph.NewQueryService(s.repo)
	result, err := qs.FindCallChain(ctx, args.FromID, args.ToID, maxDepth)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find call chain: %w", err)
	}

	type pathInfo struct {
		Nodes []SymbolResult `json:"nodes"`
	}
	paths := make([]pathInfo, 0, len(result.Paths))
	for _, path := range result.Paths {
		nodes := make([]SymbolResult, 0, len(path))
		for _, n := range path {
			if n == nil {
				continue
			}
			nodes = append(nodes, SymbolResult{
				ID:            n.ID,
				Name:          n.Name,
				Kind:          string(n.Kind),
				File:          n.File,
				Line:          n.Line,
				EndLine:       n.EndLine,
				QualifiedName: n.QualifiedName,
			})
		}
		paths = append(paths, pathInfo{Nodes: nodes})
	}

	return nil, map[string]interface{}{
		"paths": paths,
		"found": result.Found,
		"count": len(paths),
	}, nil
}

// ============================================================================
// Analysis Tool Handlers
// ============================================================================

func (s *MCPServer) handleListFiles(ctx context.Context, req *mcp.CallToolRequest, args ListFilesArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	files, err := s.repo.GetAllFiles()
	if err != nil {
		return nil, nil, err
	}

	return nil, map[string]interface{}{
		"files": files,
		"count": len(files),
	}, nil
}

func (s *MCPServer) handleGetStats(ctx context.Context, req *mcp.CallToolRequest, args GetStatsArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	stats, err := s.repo.GetStats()
	if err != nil {
		return nil, nil, err
	}

	// Convert nodes by kind
	nodesByKind := make(map[string]int64)
	for k, v := range stats.NodesByKind {
		nodesByKind[string(k)] = v
	}

	// Convert edges by kind
	edgesByKind := make(map[string]int64)
	for k, v := range stats.EdgesByKind {
		edgesByKind[string(k)] = v
	}

	result := StatsResult{
		TotalNodes:  stats.TotalNodes,
		TotalEdges:  stats.TotalEdges,
		TotalFiles:  stats.TotalFiles,
		NodesByKind: nodesByKind,
		EdgesByKind: edgesByKind,
	}

	return nil, map[string]interface{}{
		"stats": result,
	}, nil
}

func (s *MCPServer) handleFindDeadCode(ctx context.Context, req *mcp.CallToolRequest, args FindDeadCodeArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	qs := graph.NewQueryService(s.repo)
	result, err := qs.FindDeadCode(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find dead code: %w", err)
	}

	type deadCodeEntry struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		Kind          string `json:"kind"`
		File          string `json:"file"`
		Line          int    `json:"line"`
		EndLine       int    `json:"end_line,omitempty"`
		Exported      bool   `json:"exported"`
		QualifiedName string `json:"qualified_name,omitempty"`
	}

	deadNodes := make([]deadCodeEntry, 0, len(result.DeadNodes))
	for _, n := range result.DeadNodes {
		deadNodes = append(deadNodes, deadCodeEntry{
			ID:            n.ID,
			Name:          n.Name,
			Kind:          string(n.Kind),
			File:          n.File,
			Line:          n.Line,
			EndLine:       n.EndLine,
			QualifiedName: n.QualifiedName,
		})
	}

	unusedExports := make([]deadCodeEntry, 0, len(result.UnusedExports))
	for _, n := range result.UnusedExports {
		unusedExports = append(unusedExports, deadCodeEntry{
			ID:            n.ID,
			Name:          n.Name,
			Kind:          string(n.Kind),
			File:          n.File,
			Line:          n.Line,
			EndLine:       n.EndLine,
			Exported:      true,
			QualifiedName: n.QualifiedName,
		})
	}

	return nil, map[string]interface{}{
		"dead_nodes":     deadNodes,
		"unused_exports": unusedExports,
		"count":          result.Count,
	}, nil
}

func (s *MCPServer) handleFindHotspots(ctx context.Context, req *mcp.CallToolRequest, args FindHotspotsArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}

	qs := graph.NewQueryService(s.repo)
	hotspots, err := qs.FindHotspots(ctx, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find hotspots: %w", err)
	}

	type hotspotInfo struct {
		ID            int64   `json:"id"`
		Name          string  `json:"name"`
		Kind          string  `json:"kind"`
		File          string  `json:"file"`
		Line          int     `json:"line"`
		EndLine       int     `json:"end_line,omitempty"`
		QualifiedName string  `json:"qualified_name,omitempty"`
		FanIn         int     `json:"fan_in"`
		FanOut        int     `json:"fan_out"`
		CallCount     int     `json:"call_count"`
		Score         float64 `json:"score"`
		Exported      bool    `json:"exported"`
	}

	results := make([]hotspotInfo, 0, len(hotspots))
	for _, h := range hotspots {
		if h.Node == nil {
			continue
		}
		results = append(results, hotspotInfo{
			ID:            h.Node.ID,
			Name:          h.Node.Name,
			Kind:          string(h.Node.Kind),
			File:          h.Node.File,
			Line:          h.Node.Line,
			EndLine:       h.Node.EndLine,
			QualifiedName: h.Node.QualifiedName,
			FanIn:         h.FanIn,
			FanOut:        h.FanOut,
			CallCount:     h.CallCount,
			Score:         h.Score,
			Exported:      h.Node.Exported,
		})
	}

	return nil, map[string]interface{}{
		"hotspots": results,
		"count":    len(results),
	}, nil
}

// handleFindImpact performs BFS impact analysis on the reverse call graph.
func (s *MCPServer) handleFindImpact(ctx context.Context, req *mcp.CallToolRequest, args FindImpactArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	maxDepth := args.MaxDepth
	if maxDepth <= 0 || maxDepth > 10 {
		maxDepth = 3
	}

	qs := graph.NewQueryService(s.repo)
	result, err := qs.ImpactAnalysis(ctx, args.ID, maxDepth)
	if err != nil {
		return nil, nil, fmt.Errorf("impact analysis failed: %w", err)
	}

	type nodeInfo struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
		File string `json:"file"`
		Line int    `json:"line"`
	}

	impacted := make([]nodeInfo, 0, len(result.ImpactedNodes))
	for _, n := range result.ImpactedNodes {
		if n == nil {
			continue
		}
		impacted = append(impacted, nodeInfo{ID: n.ID, Name: n.Name, Kind: string(n.Kind), File: n.File, Line: n.Line})
	}

	var root interface{}
	if result.Root != nil {
		root = nodeInfo{ID: result.Root.ID, Name: result.Root.Name, Kind: string(result.Root.Kind), File: result.Root.File, Line: result.Root.Line}
	}

	return nil, map[string]interface{}{
		"root":           root,
		"impacted_nodes": impacted,
		"total_affected": result.TotalAffected,
		"impact_radius":  result.ImpactRadius,
	}, nil
}

// handleFindCallChain finds all BFS call paths between two symbols.
func (s *MCPServer) handleFindCallChain(ctx context.Context, req *mcp.CallToolRequest, args FindCallChainArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	maxDepth := args.MaxDepth
	if maxDepth <= 0 || maxDepth > 10 {
		maxDepth = 5
	}

	qs := graph.NewQueryService(s.repo)
	result, err := qs.FindCallChain(ctx, args.FromID, args.ToID, maxDepth)
	if err != nil {
		return nil, nil, fmt.Errorf("find call chain failed: %w", err)
	}

	type nodeInfo struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
		File string `json:"file"`
		Line int    `json:"line"`
	}
	type pathInfo struct {
		Nodes []nodeInfo `json:"nodes"`
	}

	paths := make([]pathInfo, 0, len(result.Paths))
	for _, path := range result.Paths {
		nodes := make([]nodeInfo, 0, len(path))
		for _, n := range path {
			if n == nil {
				continue
			}
			nodes = append(nodes, nodeInfo{ID: n.ID, Name: n.Name, Kind: string(n.Kind), File: n.File, Line: n.Line})
		}
		paths = append(paths, pathInfo{Nodes: nodes})
	}

	return nil, map[string]interface{}{
		"paths": paths,
		"found": result.Found,
		"count": len(paths),
	}, nil
}

// handleGetComplexity returns complexity metrics for a function symbol.
func (s *MCPServer) handleGetComplexity(ctx context.Context, req *mcp.CallToolRequest, args GetComplexityArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	node, err := s.repo.FindNodeByID(args.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("symbol not found: %w", err)
	}

	metrics, err := s.repo.GetComplexityMetrics(args.ID)
	if err != nil {
		// Return node info with no metrics rather than failing
		return nil, map[string]interface{}{
			"id":      node.ID,
			"name":    node.Name,
			"kind":    string(node.Kind),
			"file":    node.File,
			"line":    node.Line,
			"message": "No complexity metrics available for this symbol.",
		}, nil
	}

	return nil, map[string]interface{}{
		"id":                   node.ID,
		"name":                 node.Name,
		"kind":                 string(node.Kind),
		"file":                 node.File,
		"line":                 node.Line,
		"cyclomatic":           metrics.Cyclomatic,
		"cognitive":            metrics.Cognitive,
		"nesting":              metrics.Nesting,
		"lines_of_code":        metrics.LinesOfCode,
		"halstead_volume":      metrics.HalsteadVolume,
		"halstead_difficulty":  metrics.HalsteadDifficulty,
		"halstead_effort":      metrics.HalsteadEffort,
		"halstead_bugs":        metrics.HalsteadBugs,
	}, nil
}

// handleGetCoChanges returns file co-change pairs.
func (s *MCPServer) handleGetCoChanges(ctx context.Context, req *mcp.CallToolRequest, args GetCoChangesArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}
	minCount := args.MinCount
	if minCount <= 0 {
		minCount = 2
	}

	coChanges, err := s.repo.GetTopCoChanges(limit, minCount)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get co-changes: %w", err)
	}

	type coChangeInfo struct {
		FileA       string  `json:"file_a"`
		FileB       string  `json:"file_b"`
		CommitCount int     `json:"commit_count"`
		Jaccard     float64 `json:"jaccard"`
	}

	results := make([]coChangeInfo, 0, len(coChanges))
	for _, cc := range coChanges {
		if args.File != "" && cc.FileA != args.File && cc.FileB != args.File {
			continue
		}
		results = append(results, coChangeInfo{
			FileA:       cc.FileA,
			FileB:       cc.FileB,
			CommitCount: cc.CommitCount,
			Jaccard:     cc.Jaccard,
		})
	}

	return nil, map[string]interface{}{
		"co_changes": results,
		"count":      len(results),
	}, nil
}

// handleGetPageRank returns top symbols by PageRank.
func (s *MCPServer) handleGetPageRank(ctx context.Context, req *mcp.CallToolRequest, args GetPageRankArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}
	damping := args.Damping
	if damping <= 0 || damping >= 1 {
		damping = 0.85
	}

	nodes, err := s.repo.ListAllNodes()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load nodes: %w", err)
	}
	allEdges, err := s.repo.ListAllEdges()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load edges: %w", err)
	}

	adapter := algorithms.NewGraphAdapter(nodes, allEdges)
	ranks := adapter.CalculatePageRank(damping, 50)

	type rankInfo struct {
		ID       int64   `json:"id"`
		Name     string  `json:"name"`
		Kind     string  `json:"kind"`
		File     string  `json:"file"`
		Line     int     `json:"line"`
		PageRank float64 `json:"page_rank"`
	}

	results := make([]rankInfo, 0, limit)
	for _, r := range ranks {
		if len(results) >= limit {
			break
		}
		if r.Node == nil {
			continue
		}
		results = append(results, rankInfo{
			ID:       r.NodeID,
			Name:     r.Node.Name,
			Kind:     string(r.Node.Kind),
			File:     r.Node.File,
			Line:     r.Node.Line,
			PageRank: r.PageRank,
		})
	}

	return nil, map[string]interface{}{
		"rankings": results,
		"count":    len(results),
	}, nil
}

// ============================================================================
// Source Tool Handlers
// ============================================================================

func (s *MCPServer) handleGetSourceCode(ctx context.Context, req *mcp.CallToolRequest, args GetSourceCodeArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if len(args.IDs) == 0 {
		return nil, nil, fmt.Errorf("ids are required")
	}

	sources, err := s.repo.GetSourceCodeForNodes(args.IDs)
	if err != nil {
		return nil, nil, err
	}

	results := make([]SymbolSource, 0, len(sources))
	for id, code := range sources {
		node, err := s.repo.FindNodeByID(id)
		if err != nil {
			continue
		}
		results = append(results, SymbolSource{
			ID:         id,
			Name:       node.Name,
			SourceCode: code,
		})
	}

	return nil, map[string]interface{}{
		"symbols": results,
		"count":   len(results),
	}, nil
}

func (s *MCPServer) handleEmbeddingStatus(ctx context.Context, req *mcp.CallToolRequest, args EmbeddingStatusArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	totalNodes, err := s.repo.CountNodes()
	if err != nil {
		return nil, nil, err
	}

	totalEmbeddings, err := s.repo.GetEmbeddingCount()
	if err != nil {
		return nil, nil, err
	}

	result := EmbeddingStatusResult{
		TotalSymbols:    totalNodes,
		TotalEmbeddings: totalEmbeddings,
		Pending:         totalNodes - totalEmbeddings,
	}

	return nil, map[string]interface{}{
		"status": result,
	}, nil
}

// ============================================================================
// Helpers
// ============================================================================

// Helper to get reranker by provider
// handleArchCheck checks the codebase against architecture deny rules.
func (s *MCPServer) handleArchCheck(ctx context.Context, req *mcp.CallToolRequest, args ArchCheckArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if s.db == nil {
		return nil, map[string]interface{}{
			"violations": []interface{}{},
			"count":      0,
			"message":    "Database not available in this mode",
		}, nil
	}

	// Load deny rules
	var rows *sql.Rows
	var err error
	if args.ProjectID != nil {
		rows, err = s.db.QueryContext(ctx, `SELECT id, name, from_pattern, to_pattern FROM arch_rules WHERE enabled=1 AND kind='deny' AND project_id=?`, *args.ProjectID)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT id, name, from_pattern, to_pattern FROM arch_rules WHERE enabled=1 AND kind='deny'`)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load arch rules: %w", err)
	}
	defer rows.Close()

	type rule struct{ id int64; name, from, to string }
	var rules []rule
	for rows.Next() {
		var r rule
		rows.Scan(&r.id, &r.name, &r.from, &r.to)
		rules = append(rules, r)
	}
	rows.Close()

	if len(rules) == 0 {
		return nil, map[string]interface{}{
			"violations": []interface{}{},
			"count":      0,
			"message":    "No deny rules configured. Use arch_rules API to add rules.",
		}, nil
	}

	type violation struct {
		RuleID     int64  `json:"rule_id"`
		RuleName   string `json:"rule_name"`
		SourceFile string `json:"source_file"`
		TargetFile string `json:"target_file"`
		SourceName string `json:"source_name"`
		TargetName string `json:"target_name"`
	}
	var violations []violation
	for _, r := range rules {
		edgeRows, err := s.db.QueryContext(ctx, `
			SELECT sn.file, sn.name, tn.file, tn.name
			FROM edges e
			JOIN nodes sn ON e.source_id = sn.id
			JOIN nodes tn ON e.target_id = tn.id
			WHERE sn.file LIKE ? AND tn.file LIKE ?
			LIMIT 50
		`, "%"+r.from+"%", "%"+r.to+"%")
		if err != nil {
			continue
		}
		for edgeRows.Next() {
			var v violation
			v.RuleID = r.id
			v.RuleName = r.name
			edgeRows.Scan(&v.SourceFile, &v.SourceName, &v.TargetFile, &v.TargetName)
			violations = append(violations, v)
		}
		edgeRows.Close()
	}

	return nil, map[string]interface{}{
		"violations":    violations,
		"count":         len(violations),
		"rules_checked": len(rules),
	}, nil
}

// handleListCommunities detects module communities using Louvain algorithm.
func (s *MCPServer) handleListCommunities(ctx context.Context, req *mcp.CallToolRequest, args ListCommunitiesArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	minSize := args.MinSize
	if minSize <= 0 {
		minSize = 2
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}

	nodes, err := s.repo.ListAllNodes()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	edges, err := s.repo.ListAllEdges()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list edges: %w", err)
	}

	adapter := algorithms.NewGraphAdapter(nodes, edges)
	communities := adapter.LouvainCommunities(1.0)

	// Filter by min size and limit
	type memberInfo struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Kind string `json:"kind"`
		File string `json:"file"`
	}
	type communityInfo struct {
		ID      int64        `json:"id"`
		Size    int          `json:"size"`
		Members []memberInfo `json:"members"`
	}

	result := make([]communityInfo, 0)
	for _, community := range communities {
		if community.Size < minSize {
			continue
		}
		if len(result) >= limit {
			break
		}
		members := make([]memberInfo, 0, len(community.Nodes))
		for _, n := range community.Nodes {
			if n == nil {
				continue
			}
			members = append(members, memberInfo{ID: n.ID, Name: n.Name, Kind: string(n.Kind), File: n.File})
		}
		result = append(result, communityInfo{ID: community.ID, Size: community.Size, Members: members})
	}

	return nil, map[string]interface{}{
		"communities": result,
		"count":       len(result),
		"total_nodes": len(nodes),
	}, nil
}

// handleGetModules lists top-level modules/packages with symbol counts.
func (s *MCPServer) handleGetModules(ctx context.Context, req *mcp.CallToolRequest, args GetModulesArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	depth := args.Depth
	if depth <= 0 {
		depth = 2
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 30
	}

	nodes, err := s.repo.ListAllNodes()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Group by file path prefix
	moduleCounts := make(map[string]int)
	for _, n := range nodes {
		if n.File == "" {
			continue
		}
		// Take first `depth` path segments
		parts := splitPath(n.File, depth)
		moduleCounts[parts]++
	}

	type moduleInfo struct {
		Module string `json:"module"`
		Count  int    `json:"count"`
	}
	modules := make([]moduleInfo, 0, len(moduleCounts))
	for k, v := range moduleCounts {
		modules = append(modules, moduleInfo{Module: k, Count: v})
	}
	// Sort by count descending
	for i := 0; i < len(modules)-1; i++ {
		for j := i + 1; j < len(modules); j++ {
			if modules[j].Count > modules[i].Count {
				modules[i], modules[j] = modules[j], modules[i]
			}
		}
	}
	if len(modules) > limit {
		modules = modules[:limit]
	}

	return nil, map[string]interface{}{
		"modules": modules,
		"count":   len(modules),
	}, nil
}

// splitPath returns the first n path segments of a file path.
func splitPath(path string, n int) string {
	count := 0
	for i, c := range path {
		if c == '/' {
			count++
			if count >= n {
				return path[:i]
			}
		}
	}
	return path
}

// handleGetNodeByFile finds all symbols in a given file.
func (s *MCPServer) handleGetNodeByFile(ctx context.Context, req *mcp.CallToolRequest, args GetNodeByFileArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.File == "" {
		return nil, nil, fmt.Errorf("file is required")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}

	nodes, err := s.repo.FindNodesByFile(args.File)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find nodes: %w", err)
	}

	// Apply limit
	if limit > 0 && len(nodes) > limit {
		nodes = nodes[:limit]
	}

	results := make([]SymbolResult, 0, len(nodes))
	for _, node := range nodes {
		results = append(results, SymbolResult{
			ID:            node.ID,
			Name:          node.Name,
			Kind:          string(node.Kind),
			File:          node.File,
			Line:          node.Line,
			EndLine:       node.EndLine,
			QualifiedName: node.QualifiedName,
		})
	}

	return nil, map[string]interface{}{
		"file":    args.File,
		"symbols": results,
		"count":   len(results),
	}, nil
}

func getReranker(provider string) reranker.Reranker {
	switch provider {
	case "cohere":
		return reranker.NewCohereReranker(reranker.CohereConfig{})
	case "jina":
		return reranker.NewJinaReranker(reranker.JinaConfig{})
	default:
		return reranker.NewMockReranker(reranker.MockConfig{})
	}
}

// Helper to get embedder by provider
func getEmbedder(provider, model string) embedding.Embedder {
	switch provider {
	case "openai":
		return embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{Model: model})
	case "ollama":
		return embedding.NewOllamaEmbedder("http://localhost:11434/v1", model)
	default:
		return embedding.NewNoopEmbedder(384)
	}
}

// Close closes the server and database connection.
func (s *MCPServer) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// ServeStdio starts the MCP server with stdio transport.
// This is useful for running the MCP server as a standalone process.
func (s *MCPServer) ServeStdio() error {
	return s.server.Run(context.Background(), &mcp.StdioTransport{})
}

// GetMCPServer returns the underlying MCP server.
func (s *MCPServer) GetMCPServer() *mcp.Server {
	return s.server
}

// handleListProcesses lists materialized execution flows.
func (s *MCPServer) handleListProcesses(ctx context.Context, req *mcp.CallToolRequest, args ListProcessesArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}

	procs, err := s.repo.ListProcesses(limit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list processes: %w", err)
	}

	count, _ := s.repo.CountProcesses()

	return nil, map[string]interface{}{
		"processes": procs,
		"count":     count,
	}, nil
}

// handleGetProcess gets a specific process with full step details.
func (s *MCPServer) handleGetProcess(ctx context.Context, req *mcp.CallToolRequest, args GetProcessArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.ID == "" {
		return nil, nil, fmt.Errorf("process id is required")
	}

	proc, steps, err := s.repo.GetProcess(args.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("process not found: %w", err)
	}

	return nil, map[string]interface{}{
		"process": proc,
		"steps":   steps,
	}, nil
}
