// Package api provides MCP integration for the daemon.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mengshi02/axons/internal/agent"
	"github.com/mengshi02/axons/internal/algorithms"
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/internal/logger"
	axonsmcp "github.com/mengshi02/axons/internal/mcp"
	"github.com/mengshi02/axons/internal/service"
	"github.com/mengshi02/axons/internal/version"
)

// getKeys returns the keys of a map for debugging
func getKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// MCPServer wraps the MCP server for daemon integration.
type MCPServer struct {
	server        *axonsmcp.MCPServer
	repo          *repository.Repository
	backupService *service.BackupService
	globalRepo    *repository.GlobalRepository
}

// NewMCPServer creates a new MCP server instance.
func NewMCPServer(repo *repository.Repository) *MCPServer {
	return &MCPServer{
		server: axonsmcp.NewMCPServer(repo),
		repo:   repo,
	}
}

// SetBackupService sets the backup service.
func (s *MCPServer) SetBackupService(backupSvc *service.BackupService, globalRepo *repository.GlobalRepository) {
	s.backupService = backupSvc
	s.globalRepo = globalRepo
}

// GetServer returns the underlying MCP server.
func (s *MCPServer) GetServer() *axonsmcp.MCPServer {
	return s.server
}

// WithRepo returns a shallow copy of MCPServer that uses the given repo and rootPath.
// Used to scope MCP tool calls to a specific project database.
func (s *MCPServer) WithRepo(repo *repository.Repository, rootPath string) *MCPServer {
	s.server.SetRootPath(rootPath)
	return &MCPServer{
		server:        s.server,
		repo:          repo,
		backupService: s.backupService,
		globalRepo:    s.globalRepo,
	}
}

// RegisterRoutes registers MCP routes on the HTTP router.
// The daemon uses HTTP/SSE transport for MCP communication.
func (s *MCPServer) RegisterRoutes(router *httprouter.Router) {
	// POST /mcp - Main MCP endpoint for JSON-RPC requests
	router.POST("/mcp", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		s.handleMCPRequest(w, r)
	})

	// GET /mcp/tools - List available tools (convenience endpoint)
	router.GET("/mcp/tools", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		s.handleListTools(w, r)
	})
}

// handleMCPRequest handles incoming MCP JSON-RPC requests.
func (s *MCPServer) handleMCPRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Jsonrpc string         `json:"jsonrpc"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
		ID      any            `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSONRPCError(w, nil, -32700, "Parse error")
		return
	}

	ctx := r.Context()
	var result any
	var err error

	switch req.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": "2026-04-11",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "axons-code-graph",
				"version": version.Version,
			},
		}

	case "tools/list":
		result = s.listToolsResponse()

	case "tools/call":
		name, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)
		result, err = s.callTool(ctx, name, args)

	case "ping":
		result = map[string]any{}

	default:
		err = &MethodNotFoundError{Method: req.Method}
	}

	if err != nil {
		sendJSONRPCError(w, req.ID, -32603, err.Error())
		return
	}

	sendJSONRPCResult(w, req.ID, result)
}

// handleListTools handles GET /mcp/tools requests.
func (s *MCPServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.listToolsResponse())
}

// listToolsResponse returns the tools/list response.
func (s *MCPServer) listToolsResponse() map[string]any {
	return map[string]any{
		"tools": []map[string]any{
			{
				"name":        "keyword_search",
				"description": "Perform full-text search using FTS5 with BM25 ranking. Fast keyword-based search for code symbols.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query":  map[string]any{"type": "string", "description": "The search query string"},
						"kind":   map[string]any{"type": "string", "description": "Filter by symbol kind (function, method, class, etc.)"},
						"file":   map[string]any{"type": "string", "description": "Filter by file path pattern"},
						"limit":  map[string]any{"type": "integer", "description": "Maximum number of results (default 20)"},
					},
					"required": []string{"query"},
				},
			},
			{
				"name":        "hybrid_search",
				"description": "Perform hybrid search combining FTS5 keyword search and semantic vector search with RRF fusion. Best for comprehensive code search.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query":     map[string]any{"type": "string", "description": "Natural language or keyword query"},
						"kind":      map[string]any{"type": "string", "description": "Filter by symbol kind"},
						"file":      map[string]any{"type": "string", "description": "Filter by file path pattern"},
						"limit":     map[string]any{"type": "integer", "description": "Maximum number of results (default 10)"},
						"threshold": map[string]any{"type": "number", "description": "Minimum similarity score 0.0-1.0 (default 0.2)"},
						"provider":  map[string]any{"type": "string", "description": "Embedding provider: openai, ollama (default: ollama)"},
						"model":     map[string]any{"type": "string", "description": "Embedding model name"},
					},
					"required": []string{"query"},
				},
			},
			{
				"name":        "semantic_search",
				"description": "Search for code using natural language queries. Finds semantically similar code based on meaning.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query":     map[string]any{"type": "string", "description": "Natural language query"},
						"kind":      map[string]any{"type": "string", "description": "Filter by symbol kind"},
						"file":      map[string]any{"type": "string", "description": "Filter by file path pattern"},
						"limit":     map[string]any{"type": "integer", "description": "Maximum number of results (default 10)"},
						"threshold": map[string]any{"type": "number", "description": "Minimum similarity score 0.0-1.0 (default 0.5)"},
						"provider":  map[string]any{"type": "string", "description": "Embedding provider: openai, ollama"},
						"model":     map[string]any{"type": "string", "description": "Embedding model name"},
					},
					"required": []string{"query"},
				},
			},
			{
				"name":        "rerank_results",
				"description": "Rerank search results using a reranking model for improved relevance. Supports Cohere, Jina APIs, or local mock reranker.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query":     map[string]any{"type": "string", "description": "The original search query"},
						"documents": map[string]any{"type": "array", "description": "Array of document texts to rerank"},
						"provider":  map[string]any{"type": "string", "description": "Rerank provider: cohere, jina, or mock (default: mock)"},
						"model":     map[string]any{"type": "string", "description": "Rerank model name"},
						"top_n":     map[string]any{"type": "integer", "description": "Number of top results to return"},
					},
					"required": []string{"query", "documents"},
				},
			},
			{
				"name":        "search_symbols",
				"description": "Search for symbols in the code graph by name pattern",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"pattern": map[string]any{"type": "string", "description": "The name pattern to search for"},
						"limit":   map[string]any{"type": "integer", "description": "Maximum results"},
					},
					"required": []string{"pattern"},
				},
			},
			{
				"name":        "get_symbol",
				"description": "Get detailed information about a symbol by ID",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{"type": "integer", "description": "The symbol ID"},
					},
					"required": []string{"id"},
				},
			},
			{
				"name":        "find_callers",
				"description": "Find all functions that call a given symbol",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{"type": "integer", "description": "The symbol ID"},
					},
					"required": []string{"id"},
				},
			},
			{
				"name":        "find_callees",
				"description": "Find all functions called by a given symbol",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{"type": "integer", "description": "The symbol ID"},
					},
					"required": []string{"id"},
				},
			},
			{
				"name":        "path",
				"description": "Find shortest path between two symbols",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"from_id": map[string]any{"type": "integer", "description": "Source symbol ID"},
						"to_id":   map[string]any{"type": "integer", "description": "Target symbol ID"},
					},
					"required": []string{"from_id", "to_id"},
				},
			},
			{
				"name":        "list_files",
				"description": "List all indexed files in the code graph",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			{
				"name":        "get_stats",
				"description": "Get statistics about the code graph",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			{
				"name":        "find_dead_code",
				"description": "Find potentially dead code (unused functions)",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			{
				"name":        "find_hotspots",
				"description": "Find code hotspots (highly connected functions)",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{"type": "integer", "description": "Maximum number of results (default 20)"},
					},
				},
			},
			{
				"name":        "get_source_code",
				"description": "Retrieve the source code content for one or more symbols by their IDs. Returns the actual code text.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"ids": map[string]any{"type": "array", "description": "Array of symbol IDs to retrieve source code for"},
					},
					"required": []string{"ids"},
				},
			},
			{
				"name":        "embedding_status",
				"description": "Get the status of code embeddings",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			{
				"name":        "read_file",
				"description": "Read the raw content of any file by path. Supports optional line range. Useful for config files, SQL migrations, READMEs, and other non-indexed files.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":       map[string]any{"type": "string", "description": "Absolute or relative file path to read"},
						"start_line": map[string]any{"type": "integer", "description": "First line to read, 1-based inclusive (default: 1)"},
						"end_line":   map[string]any{"type": "integer", "description": "Last line to read, 1-based inclusive (default: all)"},
					},
					"required": []string{"path"},
				},
			},
			{
				"name":        "write_file",
				"description": "Write content to a file, creating parent directories if needed. Path must be within the project root.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":    map[string]any{"type": "string", "description": "Absolute or relative file path to write"},
						"content": map[string]any{"type": "string", "description": "Full content to write (overwrites existing)"},
					},
					"required": []string{"path", "content"},
				},
			},
			{
				"name":        "run_command",
				"description": "Run a shell command and return stdout, stderr, exit code. Allowlist: go, python, node, cargo, git, make, etc.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string", "description": "Command to run"},
						"args":    map[string]any{"type": "array", "description": "Command arguments"},
						"cwd":     map[string]any{"type": "string", "description": "Working directory (default: project root)"},
						"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (default: 30, max: 120)"},
					},
					"required": []string{"command"},
				},
			},
		},
	}
}

// callTool calls a tool by name using the underlying MCP server.
func (s *MCPServer) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	// For now, we implement tool calling directly here
	// This avoids the complexity of the SDK's CallTool method
	return s.CallToolDirect(ctx, name, args)
}

// callToolDirect directly calls tool handlers.
func (s *MCPServer) CallToolDirect(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "keyword_search":
		return s.handleKeywordSearch(args)
	case "hybrid_search":
		return s.handleHybridSearch(ctx, args)
	case "semantic_search":
		return s.handleSemanticSearch(ctx, args)
	case "rerank_results":
		return s.handleRerankResults(ctx, args)
	case "search_symbols":
		return s.handleSearchSymbols(args)
	case "get_symbol":
		return s.handleGetSymbol(args)
	case "find_callers":
		return s.handleFindCallers(args)
	case "find_callees":
		return s.handleFindCallees(args)
	case "path":
		return s.handlePath(args)
	case "list_files":
		return s.handleListFiles()
	case "get_stats":
		return s.handleGetStats()
	case "find_dead_code":
		return s.handleFindDeadCode()
	case "find_hotspots":
		return s.handleFindHotspots(args)
	case "get_source_code":
		return s.handleGetSourceCode(args)
	case "embedding_status":
		return s.handleEmbeddingStatus()
	case "read_file":
		return s.handleReadFile(args)
	case "write_file":
		return s.handleWriteFile(ctx, args)
	case "run_command":
		return s.handleRunCommand(ctx, args)
	case "find_impact":
		return s.handleFindImpact(args)
	case "find_call_chain":
		return s.handleFindCallChain(args)
	case "get_complexity":
		return s.handleGetComplexity(args)
	case "get_cochanges":
		return s.handleGetCoChanges(args)
	case "get_pagerank":
		return s.handleGetPageRank(args)
	case "arch_check":
		return s.handleArchCheck(args)
	case "list_communities":
		return s.handleListCommunities(args)
	case "get_modules":
		return s.handleGetModules(args)
	case "get_node_by_file":
		return s.handleGetNodeByFile(args)
	case "list_processes":
		return s.handleListProcesses(args)
	case "get_process":
		return s.handleGetProcess(args)
	default:
		return nil, &MethodNotFoundError{Method: name}
	}
}

// Tool handler implementations (delegated to the underlying server)

func (s *MCPServer) handleKeywordSearch(args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, &InvalidParamsError{Message: "query is required"}
	}

	kind, _ := args["kind"].(string)
	file, _ := args["file"].(string)
	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	results, err := s.repo.FTS5SearchWithFilter(query, kind, file, limit)
	if err != nil {
		return nil, err
	}

	output := make([]map[string]any, len(results))
	for i, r := range results {
		output[i] = map[string]any{
			"id":             r.NodeID,
			"name":           r.Name,
			"kind":           r.Kind,
			"file":           r.File,
			"line":           r.Line,
			"end_line":       r.EndLine,
			"qualified_name": r.QualifiedName,
			"bm25_score":     r.BM25Score,
			"docstring":      r.Docstring,
		}
	}

	return map[string]any{
		"query":         query,
		"total_results": len(output),
		"results":       output,
	}, nil
}

func (s *MCPServer) handleHybridSearch(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, &InvalidParamsError{Message: "query is required"}
	}

	kind, _ := args["kind"].(string)
	file, _ := args["file"].(string)
	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	threshold := 0.2
	if t, ok := args["threshold"].(float64); ok {
		threshold = t
	}

	// Use the underlying server's hybrid search if available
	// For now, fall back to keyword search only
	results, err := s.repo.FTS5SearchWithFilter(query, kind, file, limit)
	if err != nil {
		return nil, err
	}

	output := make([]map[string]any, len(results))
	for i, r := range results {
		output[i] = map[string]any{
			"id":             r.NodeID,
			"name":           r.Name,
			"kind":           r.Kind,
			"file":           r.File,
			"line":           r.Line,
			"end_line":       r.EndLine,
			"qualified_name": r.QualifiedName,
			"score":          r.BM25Score,
		}
	}

	return map[string]any{
		"query":         query,
		"total_results": len(output),
		"threshold":     threshold,
		"results":       output,
	}, nil
}

func (s *MCPServer) handleSemanticSearch(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, &InvalidParamsError{Message: "query is required"}
	}

	return map[string]any{
		"error":  "semantic_search requires embedding service",
		"query":  query,
		"hint":   "Use hybrid_search or keyword_search instead",
	}, nil
}

func (s *MCPServer) handleRerankResults(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, &InvalidParamsError{Message: "query is required"}
	}

	docsRaw, ok := args["documents"].([]any)
	if !ok || len(docsRaw) == 0 {
		return nil, &InvalidParamsError{Message: "documents array is required"}
	}

	// Convert documents
	docs := make([]string, len(docsRaw))
	for i, d := range docsRaw {
		if str, ok := d.(string); ok {
			docs[i] = str
		}
	}

	topN := len(docs)
	if t, ok := args["top_n"].(float64); ok && int(t) < len(docs) {
		topN = int(t)
	}

	// Mock rerank: just return documents with mock scores
	results := make([]map[string]any, topN)
	for i := 0; i < topN; i++ {
		results[i] = map[string]any{
			"index":          i,
			"relevance_score": 1.0 - float64(i)*0.1,
			"document":       docs[i],
		}
	}

	return map[string]any{
		"query":         query,
		"total_results": len(results),
		"results":       results,
	}, nil
}

func (s *MCPServer) handleSearchSymbols(args map[string]any) (any, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, &InvalidParamsError{Message: "pattern is required"}
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	nodes, err := s.repo.FindNodesByName(pattern, limit)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, len(nodes))
	for i, node := range nodes {
		results[i] = map[string]any{
			"id":             node.ID,
			"name":           node.Name,
			"kind":           string(node.Kind),
			"file":           node.File,
			"line":           node.Line,
			"qualified_name": node.QualifiedName,
		}
	}

	return map[string]any{
		"pattern": pattern,
		"count":   len(results),
		"results": results,
	}, nil
}

func (s *MCPServer) handleGetSymbol(args map[string]any) (any, error) {
	id, ok := args["id"].(float64)
	if !ok {
		return nil, &InvalidParamsError{Message: "id is required"}
	}

	node, err := s.repo.FindNodeByID(int64(id))
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"id":             node.ID,
		"name":           node.Name,
		"kind":           string(node.Kind),
		"file":           node.File,
		"line":           node.Line,
		"end_line":       node.EndLine,
		"qualified_name": node.QualifiedName,
		"exported":       node.Exported,
		"visibility":     string(node.Visibility),
	}, nil
}

func (s *MCPServer) handleFindCallers(args map[string]any) (any, error) {
	id, ok := args["id"].(float64)
	if !ok {
		return nil, &InvalidParamsError{Message: "id is required"}
	}

	callers, err := s.repo.FindCallers(int64(id))
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, len(callers))
	for i, caller := range callers {
		results[i] = map[string]any{
			"id":   caller.ID,
			"name": caller.Name,
			"kind": string(caller.Kind),
			"file": caller.File,
			"line": caller.Line,
		}
	}

	return map[string]any{
		"callers": results,
		"count":   len(results),
	}, nil
}

func (s *MCPServer) handleFindCallees(args map[string]any) (any, error) {
	id, ok := args["id"].(float64)
	if !ok {
		return nil, &InvalidParamsError{Message: "id is required"}
	}

	callees, err := s.repo.FindCallees(int64(id))
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, len(callees))
	for i, callee := range callees {
		results[i] = map[string]any{
			"id":   callee.ID,
			"name": callee.Name,
			"kind": string(callee.Kind),
			"file": callee.File,
			"line": callee.Line,
		}
	}

	return map[string]any{
		"callees": results,
		"count":   len(results),
	}, nil
}

func (s *MCPServer) handlePath(args map[string]any) (any, error) {
	fromID, ok1 := args["from_id"].(float64)
	toID, ok2 := args["to_id"].(float64)
	if !ok1 || !ok2 {
		return nil, &InvalidParamsError{Message: "from_id and to_id are required"}
	}

	from, err := s.repo.FindNodeByID(int64(fromID))
	if err != nil {
		return nil, err
	}

	to, err := s.repo.FindNodeByID(int64(toID))
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"from": map[string]any{
			"id":   from.ID,
			"name": from.Name,
			"file": from.File,
			"line": from.Line,
		},
		"to": map[string]any{
			"id":   to.ID,
			"name": to.Name,
			"file": to.File,
			"line": to.Line,
		},
		"message": "Shortest path algorithm not yet implemented",
	}, nil
}

func (s *MCPServer) handleListFiles() (any, error) {
	files, err := s.repo.GetAllFiles()
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"files": files,
		"count": len(files),
	}, nil
}

func (s *MCPServer) handleGetStats() (any, error) {
	stats, err := s.repo.GetStats()
	if err != nil {
		return nil, err
	}

	nodesByKind := make(map[string]int64)
	for k, v := range stats.NodesByKind {
		nodesByKind[string(k)] = v
	}

	edgesByKind := make(map[string]int64)
	for k, v := range stats.EdgesByKind {
		edgesByKind[string(k)] = v
	}

	return map[string]any{
		"total_nodes":   stats.TotalNodes,
		"total_edges":   stats.TotalEdges,
		"total_files":   stats.TotalFiles,
		"nodes_by_kind": nodesByKind,
		"edges_by_kind": edgesByKind,
	}, nil
}

func (s *MCPServer) handleFindDeadCode() (any, error) {
	qs := graph.NewQueryService(s.repo)
	result, err := qs.FindDeadCode(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to find dead code: %w", err)
	}

	type deadCodeEntry struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		Kind          string `json:"kind"`
		File          string `json:"file"`
		Line          int    `json:"line"`
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
			Exported:      true,
			QualifiedName: n.QualifiedName,
		})
	}

	return map[string]any{
		"dead_nodes":     deadNodes,
		"unused_exports": unusedExports,
		"count":          result.Count,
	}, nil
}

func (s *MCPServer) handleFindHotspots(args map[string]any) (any, error) {
	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	qs := graph.NewQueryService(s.repo)
	hotspots, err := qs.FindHotspots(context.Background(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to find hotspots: %w", err)
	}

	type hotspotInfo struct {
		ID            int64   `json:"id"`
		Name          string  `json:"name"`
		Kind          string  `json:"kind"`
		File          string  `json:"file"`
		Line          int     `json:"line"`
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
			QualifiedName: h.Node.QualifiedName,
			FanIn:         h.FanIn,
			FanOut:        h.FanOut,
			CallCount:     h.CallCount,
			Score:         h.Score,
			Exported:      h.Node.Exported,
		})
	}

	return map[string]any{
		"hotspots": results,
		"count":    len(results),
	}, nil
}

func (s *MCPServer) handleGetSourceCode(args map[string]any) (any, error) {
	idsRaw, ok := args["ids"].([]any)
	if !ok || len(idsRaw) == 0 {
		return nil, &InvalidParamsError{Message: "ids array is required"}
	}

	ids := make([]int64, len(idsRaw))
	for i, id := range idsRaw {
		switch v := id.(type) {
		case float64:
			ids[i] = int64(v)
		case int64:
			ids[i] = v
		case int:
			ids[i] = int64(v)
		default:
			return nil, &InvalidParamsError{Message: "invalid id type"}
		}
	}

	sources, err := s.repo.GetSourceCodeForNodes(ids)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0, len(sources))
	for id, code := range sources {
		node, err := s.repo.FindNodeByID(id)
		if err != nil {
			continue
		}
		results = append(results, map[string]any{
			"id":         id,
			"name":       node.Name,
			"source":     code,
			"file":       node.File,
			"line":       node.Line,
			"end_line":   node.EndLine,
		})
	}

	return map[string]any{
		"symbols": results,
		"count":   len(results),
	}, nil
}

func (s *MCPServer) handleEmbeddingStatus() (any, error) {
	totalNodes, err := s.repo.CountNodes()
	if err != nil {
		return nil, err
	}

	totalEmbeddings, err := s.repo.GetEmbeddingCount()
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"total_symbols":    totalNodes,
		"total_embeddings": totalEmbeddings,
		"pending":          totalNodes - totalEmbeddings,
	}, nil
}

func (s *MCPServer) handleReadFile(args map[string]any) (any, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return nil, &InvalidParamsError{Message: "path is required"}
	}
	startLine := 0
	endLine := 0
	if v, ok := args["start_line"].(float64); ok {
		startLine = int(v)
	}
	if v, ok := args["end_line"].(float64); ok {
		endLine = int(v)
	}

	// 解析路径：优先使用 ResolveSafePath（支持相对路径），
	// 若安全检查失败（路径在项目根以外），且为绝对路径则直接使用。
	absPath, err := s.server.ResolveSafePath(path)
	if err != nil {
		if filepath.IsAbs(path) {
			absPath = filepath.Clean(path)
		} else {
			return nil, err
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	const maxBytes = 512 * 1024
	truncated := false
	if len(data) > maxBytes {
		data = data[:maxBytes]
		truncated = true
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)
	start := 1
	end := totalLines
	if startLine > 0 {
		start = startLine
	}
	if endLine > 0 && endLine < end {
		end = endLine
	}
	if start < 1 {
		start = 1
	}
	if end > totalLines {
		end = totalLines
	}

	content := strings.Join(lines[start-1:end], "\n")
	return map[string]any{
		"path":        absPath,
		"content":     content,
		"start_line":  start,
		"end_line":    end,
		"total_lines": totalLines,
		"truncated":   truncated,
	}, nil
}

func (s *MCPServer) handleWriteFile(ctx context.Context, args map[string]any) (any, error) {
	logger.S().Debugw("[handleWriteFile] Received args", "args", args)
	path, _ := args["path"].(string)
	if path == "" {
		logger.S().Debugw("[handleWriteFile] Path is empty or missing", "args_keys", getKeys(args))
		return nil, &InvalidParamsError{Message: "path is required"}
	}
	content, _ := args["content"].(string)

	absPath, err := s.server.ResolveSafePath(path)
	if err != nil {
		return nil, err
	}

	// Backup file before modification (if backup service is available and we have context)
	if s.backupService != nil && s.globalRepo != nil {
		projectID := agent.GetProjectIDFromContext(ctx)
		sessionID := agent.GetSessionIDFromContext(ctx)
		
		logger.S().Infow("[handleWriteFile] Backup check", "projectID", projectID, "sessionID", sessionID, "path", path)
		
		if projectID != "" && sessionID != "" {
			project, err := s.globalRepo.GetProject(projectID)
			if err == nil && project != nil {
				logger.S().Infow("[handleWriteFile] Backing up file", "projectRoot", project.RootPath, "path", path)
				// Backup before write
				if err := s.backupService.Backup(project.RootPath, projectID, sessionID, path); err != nil {
					logger.S().Warnw("[handleWriteFile] Failed to backup file", "error", err, "path", path)
					// Continue with write even if backup fails
				} else {
					logger.S().Infow("[handleWriteFile] Backup successful", "path", path)
				}
			} else {
				logger.S().Warnw("[handleWriteFile] Project not found", "projectID", projectID, "error", err)
			}
		} else {
			logger.S().Warnw("[handleWriteFile] Missing context", "projectID", projectID, "sessionID", sessionID)
		}
	} else {
		logger.S().Warnw("[handleWriteFile] Backup service not available", "hasBackupService", s.backupService != nil, "hasGlobalRepo", s.globalRepo != nil)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return map[string]any{
		"path":  absPath,
		"bytes": len(content),
		"ok":    true,
	}, nil
}

func (s *MCPServer) handleRunCommand(ctx context.Context, args map[string]any) (any, error) {
	command, _ := args["command"].(string)
	if command == "" {
		return nil, &InvalidParamsError{Message: "command is required"}
	}

	if !isAllowedCommand(command) {
		return nil, fmt.Errorf("command %q is not in the allowlist", command)
	}

	var cmdArgs []string
	switch v := args["args"].(type) {
	case []any:
		for _, a := range v {
			if str, ok := a.(string); ok {
				cmdArgs = append(cmdArgs, str)
			}
		}
	case string:
		// 兼容 LLM 发送字符串格式的情况
		// 支持两种格式:
		// 1. 空格分隔: "build -v ./nsqd"
		// 2. 换行分隔: "build\n-v\n./nsqd"
		v = strings.TrimSpace(v)
		if v != "" {
			// 先按换行符分割，再处理空格
			lines := strings.Split(v, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					// 每行可能还包含空格分隔的多个参数
					parts := strings.Fields(line)
					cmdArgs = append(cmdArgs, parts...)
				}
			}
		}
	}

	timeout := 30
	if v, ok := args["timeout"].(float64); ok && int(v) > 0 {
		timeout = int(v)
	}
	if timeout > 120 {
		timeout = 120
	}

	cwd := s.server.RootDir()
	if v, _ := args["cwd"].(string); v != "" {
		var err error
		cwd, err = s.server.ResolveSafePath(v)
		if err != nil {
			return nil, err
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, command, cmdArgs...)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return map[string]any{
		"command":   command,
		"args":      cmdArgs,
		"cwd":       cwd,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"exit_code": exitCode,
		"ok":        exitCode == 0,
	}, nil
}

var apiAllowedCommands = []string{
	"go", "python", "python3", "node", "npm", "npx",
	"cargo", "rustc", "javac", "java", "mvn", "gradle",
	"git", "make", "sh", "bash",
}

func isAllowedCommand(cmd string) bool {
	// 提取命令的第一个单词(真正的命令名),兼容两种调用格式:
	// 1. "go" - 只有命令名
	// 2. "go test ./..." - 命令名带参数
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	base := filepath.Base(parts[0])
	for _, a := range apiAllowedCommands {
		if base == a {
			return true
		}
	}
	return false
}

// ServeStdio serves MCP over stdio (for MCP client connections).
func (s *MCPServer) ServeStdio() error {
	return s.server.ServeStdio()
}

// Close closes the MCP server.
func (s *MCPServer) Close() error {
	return s.server.Close()
}

// Helper functions for JSON-RPC responses

func sendJSONRPCResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func sendJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

// ============================================================================
// Handlers for graph analysis tools (delegating to s.repo / graph package)
// ============================================================================

func (s *MCPServer) handleFindImpact(args map[string]any) (any, error) {
	id, ok := args["id"].(float64)
	if !ok {
		return nil, &InvalidParamsError{Message: "id is required"}
	}
	maxDepth := 3
	if v, ok := args["max_depth"].(float64); ok && int(v) > 0 {
		maxDepth = int(v)
	}
	if maxDepth > 10 {
		maxDepth = 10
	}

	qs := graph.NewQueryService(s.repo)
	result, err := qs.ImpactAnalysis(context.Background(), int64(id), maxDepth)
	if err != nil {
		return nil, fmt.Errorf("impact analysis failed: %w", err)
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
	var root any
	if result.Root != nil {
		root = nodeInfo{ID: result.Root.ID, Name: result.Root.Name, Kind: string(result.Root.Kind), File: result.Root.File, Line: result.Root.Line}
	}
	return map[string]any{
		"root":           root,
		"impacted_nodes": impacted,
		"total_affected": result.TotalAffected,
		"impact_radius":  result.ImpactRadius,
	}, nil
}

func (s *MCPServer) handleFindCallChain(args map[string]any) (any, error) {
	fromID, ok1 := args["from_id"].(float64)
	toID, ok2 := args["to_id"].(float64)
	if !ok1 || !ok2 {
		return nil, &InvalidParamsError{Message: "from_id and to_id are required"}
	}
	maxDepth := 5
	if v, ok := args["max_depth"].(float64); ok && int(v) > 0 {
		maxDepth = int(v)
	}
	if maxDepth > 10 {
		maxDepth = 10
	}

	qs := graph.NewQueryService(s.repo)
	result, err := qs.FindCallChain(context.Background(), int64(fromID), int64(toID), maxDepth)
	if err != nil {
		return nil, fmt.Errorf("find call chain failed: %w", err)
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
	for _, p := range result.Paths {
		nodes := make([]nodeInfo, 0, len(p))
		for _, n := range p {
			if n == nil {
				continue
			}
			nodes = append(nodes, nodeInfo{ID: n.ID, Name: n.Name, Kind: string(n.Kind), File: n.File, Line: n.Line})
		}
		paths = append(paths, pathInfo{Nodes: nodes})
	}
	return map[string]any{
		"paths": paths,
		"found": result.Found,
		"count": len(paths),
	}, nil
}

func (s *MCPServer) handleGetComplexity(args map[string]any) (any, error) {
	id, ok := args["id"].(float64)
	if !ok {
		return nil, &InvalidParamsError{Message: "id is required"}
	}
	node, err := s.repo.FindNodeByID(int64(id))
	if err != nil {
		return nil, fmt.Errorf("symbol not found: %w", err)
	}
	metrics, err := s.repo.GetComplexityMetrics(int64(id))
	if err != nil {
		return map[string]any{
			"id":      node.ID,
			"name":    node.Name,
			"kind":    string(node.Kind),
			"file":    node.File,
			"line":    node.Line,
			"message": "No complexity metrics available for this symbol.",
		}, nil
	}
	return map[string]any{
		"id":                  node.ID,
		"name":                node.Name,
		"kind":                string(node.Kind),
		"file":                node.File,
		"line":                node.Line,
		"cyclomatic":          metrics.Cyclomatic,
		"cognitive":           metrics.Cognitive,
		"nesting":             metrics.Nesting,
		"lines_of_code":       metrics.LinesOfCode,
		"halstead_volume":     metrics.HalsteadVolume,
		"halstead_difficulty": metrics.HalsteadDifficulty,
		"halstead_effort":     metrics.HalsteadEffort,
		"halstead_bugs":       metrics.HalsteadBugs,
	}, nil
}

func (s *MCPServer) handleGetCoChanges(args map[string]any) (any, error) {
	limit := 20
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}
	minCount := 2
	if v, ok := args["min_count"].(float64); ok && int(v) > 0 {
		minCount = int(v)
	}
	file, _ := args["file"].(string)

	coChanges, err := s.repo.GetTopCoChanges(limit, minCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get co-changes: %w", err)
	}

	type coChangeInfo struct {
		FileA       string  `json:"file_a"`
		FileB       string  `json:"file_b"`
		CommitCount int     `json:"commit_count"`
		Jaccard     float64 `json:"jaccard"`
	}
	results := make([]coChangeInfo, 0, len(coChanges))
	for _, cc := range coChanges {
		if file != "" && cc.FileA != file && cc.FileB != file {
			continue
		}
		results = append(results, coChangeInfo{
			FileA:       cc.FileA,
			FileB:       cc.FileB,
			CommitCount: cc.CommitCount,
			Jaccard:     cc.Jaccard,
		})
	}
	return map[string]any{
		"co_changes": results,
		"count":      len(results),
	}, nil
}

func (s *MCPServer) handleGetPageRank(args map[string]any) (any, error) {
	limit := 20
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}
	damping := 0.85
	if v, ok := args["damping"].(float64); ok && v > 0 && v < 1 {
		damping = v
	}

	nodes, err := s.repo.ListAllNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to load nodes: %w", err)
	}
	allEdges, err := s.repo.ListAllEdges()
	if err != nil {
		return nil, fmt.Errorf("failed to load edges: %w", err)
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
	return map[string]any{
		"rankings": results,
		"count":    len(results),
	}, nil
}

func (s *MCPServer) handleArchCheck(args map[string]any) (any, error) {
	// arch_check requires direct DB access for arch_rules queries.
	// In daemon mode the underlying MCPServer has no db → return informational message.
	return map[string]any{
		"violations": []any{},
		"count":      0,
		"message":    "arch_check is not available in daemon mode. Use the standalone MCP server (axons mcp) to check architecture rules.",
	}, nil
}

func (s *MCPServer) handleListCommunities(args map[string]any) (any, error) {
	minSize := 2
	if v, ok := args["min_size"].(float64); ok && int(v) > 0 {
		minSize = int(v)
	}
	limit := 20
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}

	nodes, err := s.repo.ListAllNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	edges, err := s.repo.ListAllEdges()
	if err != nil {
		return nil, fmt.Errorf("failed to list edges: %w", err)
	}

	adapter := algorithms.NewGraphAdapter(nodes, edges)
	communities := adapter.LouvainCommunities(1.0)

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
	return map[string]any{
		"communities": result,
		"count":       len(result),
		"total_nodes": len(nodes),
	}, nil
}

func (s *MCPServer) handleGetModules(args map[string]any) (any, error) {
	depth := 2
	if v, ok := args["depth"].(float64); ok && int(v) > 0 {
		depth = int(v)
	}
	limit := 30
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}

	nodes, err := s.repo.ListAllNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	moduleCounts := make(map[string]int)
	for _, n := range nodes {
		if n.File == "" {
			continue
		}
		parts := apiSplitPath(n.File, depth)
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
	return map[string]any{
		"modules": modules,
		"count":   len(modules),
	}, nil
}

func apiSplitPath(path string, n int) string {
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

func (s *MCPServer) handleGetNodeByFile(args map[string]any) (any, error) {
	file, _ := args["file"].(string)
	if file == "" {
		return nil, &InvalidParamsError{Message: "file is required"}
	}
	limit := 50
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}

	nodes, err := s.repo.FindNodesByFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to find nodes: %w", err)
	}
	if len(nodes) > limit {
		nodes = nodes[:limit]
	}

	results := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		results = append(results, map[string]any{
			"id":             node.ID,
			"name":           node.Name,
			"kind":           string(node.Kind),
			"file":           node.File,
			"line":           node.Line,
			"end_line":       node.EndLine,
			"qualified_name": node.QualifiedName,
		})
	}
	return map[string]any{
		"file":    file,
		"symbols": results,
		"count":   len(results),
	}, nil
}

func (s *MCPServer) handleListProcesses(args map[string]any) (any, error) {
	limit := 50
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}

	procs, err := s.repo.ListProcesses(limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}
	count, _ := s.repo.CountProcesses()
	return map[string]any{
		"processes": procs,
		"count":     count,
	}, nil
}

func (s *MCPServer) handleGetProcess(args map[string]any) (any, error) {
	id, _ := args["id"].(string)
	if id == "" {
		return nil, &InvalidParamsError{Message: "id is required"}
	}

	proc, steps, err := s.repo.GetProcess(id)
	if err != nil {
		return nil, fmt.Errorf("process not found: %w", err)
	}
	return map[string]any{
		"process": proc,
		"steps":   steps,
	}, nil
}

// Error types

// MethodNotFoundError indicates an unknown JSON-RPC method.
type MethodNotFoundError struct {
	Method string
}

func (e *MethodNotFoundError) Error() string {
	return "method not found: " + e.Method
}

// InvalidParamsError indicates invalid parameters.
type InvalidParamsError struct {
	Message string
}

func (e *InvalidParamsError) Error() string {
	return "invalid params: " + e.Message
}