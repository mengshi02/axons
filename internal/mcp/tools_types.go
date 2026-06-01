// Package mcp provides MCP server implementation using the official Go SDK.
package mcp

// Argument types for MCP tools.

// Search tools

// KeywordSearchArgs represents arguments for keyword_search tool.
type KeywordSearchArgs struct {
	Query string `json:"query" jsonschema_description:"The search query string"`
	Kind  string `json:"kind,omitempty" jsonschema_description:"Filter by symbol kind (function, method, class, etc.)"`
	File  string `json:"file,omitempty" jsonschema_description:"Filter by file path pattern"`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Maximum number of results (default 20)"`
}

// HybridSearchArgs represents arguments for hybrid_search tool.
type HybridSearchArgs struct {
	Query     string  `json:"query" jsonschema_description:"Natural language or keyword query"`
	Kind      string  `json:"kind,omitempty" jsonschema_description:"Filter by symbol kind"`
	File      string  `json:"file,omitempty" jsonschema_description:"Filter by file path pattern"`
	Limit     int     `json:"limit,omitempty" jsonschema_description:"Maximum number of results (default 10)"`
	Threshold float64 `json:"threshold,omitempty" jsonschema_description:"Minimum similarity score 0.0-1.0 (default 0.2)"`
	Provider  string  `json:"provider,omitempty" jsonschema_description:"Embedding provider: openai, ollama (default: ollama)"`
	Model     string  `json:"model,omitempty" jsonschema_description:"Embedding model name"`
}

// SemanticSearchArgs represents arguments for semantic_search tool.
type SemanticSearchArgs struct {
	Query     string  `json:"query" jsonschema_description:"Natural language query describing the code you're looking for"`
	Kind      string  `json:"kind,omitempty" jsonschema_description:"Filter by symbol kind"`
	File      string  `json:"file,omitempty" jsonschema_description:"Filter by file path pattern"`
	Limit     int     `json:"limit,omitempty" jsonschema_description:"Maximum number of results to return (default 10)"`
	Threshold float64 `json:"threshold,omitempty" jsonschema_description:"Minimum similarity score threshold 0.0-1.0 (default 0.5)"`
	Provider  string  `json:"provider,omitempty" jsonschema_description:"Embedding provider: openai, ollama, or noop (default: ollama)"`
	Model     string  `json:"model,omitempty" jsonschema_description:"Embedding model name"`
}

// RerankArgs represents arguments for rerank_results tool.
type RerankArgs struct {
	Query     string   `json:"query" jsonschema_description:"The original search query"`
	Documents []string `json:"documents" jsonschema_description:"Array of document texts to rerank"`
	Provider  string   `json:"provider,omitempty" jsonschema_description:"Rerank provider: cohere, jina, or mock (default: mock)"`
	Model     string   `json:"model,omitempty" jsonschema_description:"Rerank model name"`
	TopN      int      `json:"top_n,omitempty" jsonschema_description:"Number of top results to return"`
}

// SearchSymbolsArgs represents arguments for search_symbols tool.
type SearchSymbolsArgs struct {
	Pattern string `json:"pattern" jsonschema_description:"The name pattern to search for"`
	Kind    string `json:"kind,omitempty" jsonschema_description:"Filter by symbol kind"`
	Limit   int    `json:"limit,omitempty" jsonschema_description:"Maximum number of results (default 20)"`
}

// Graph tools

// GetSymbolArgs represents arguments for get_symbol tool.
type GetSymbolArgs struct {
	ID int64 `json:"id" jsonschema_description:"The symbol ID"`
}

// FindCallersArgs represents arguments for find_callers tool.
type FindCallersArgs struct {
	ID int64 `json:"id" jsonschema_description:"The symbol ID"`
}

// FindCalleesArgs represents arguments for find_callees tool.
type FindCalleesArgs struct {
	ID int64 `json:"id" jsonschema_description:"The symbol ID"`
}

// PathArgs represents arguments for path tool.
type PathArgs struct {
	FromID   int64 `json:"from_id" jsonschema_description:"The source symbol ID"`
	ToID     int64 `json:"to_id" jsonschema_description:"The target symbol ID"`
	MaxDepth int   `json:"max_depth,omitempty" jsonschema_description:"Maximum BFS depth (default 6, max 10)"`
}

// FindImpactArgs represents arguments for find_impact tool.
type FindImpactArgs struct {
	ID       int64 `json:"id" jsonschema_description:"The symbol ID to analyze impact for"`
	MaxDepth int   `json:"max_depth,omitempty" jsonschema_description:"Maximum BFS depth (default 3)"`
}

// FindCallChainArgs represents arguments for find_call_chain tool.
type FindCallChainArgs struct {
	FromID   int64 `json:"from_id" jsonschema_description:"Source symbol ID"`
	ToID     int64 `json:"to_id" jsonschema_description:"Target symbol ID"`
	MaxDepth int   `json:"max_depth,omitempty" jsonschema_description:"Maximum depth (default 5)"`
}

// GetComplexityArgs represents arguments for get_complexity tool.
type GetComplexityArgs struct {
	ID int64 `json:"id" jsonschema_description:"The symbol ID"`
}

// GetCoChangesArgs represents arguments for get_cochanges tool.
type GetCoChangesArgs struct {
	File     string `json:"file,omitempty" jsonschema_description:"Filter by file path pattern"`
	Limit    int    `json:"limit,omitempty" jsonschema_description:"Maximum number of results (default 20)"`
	MinCount int    `json:"min_count,omitempty" jsonschema_description:"Minimum co-change count (default 2)"`
}

// GetPageRankArgs represents arguments for get_pagerank tool.
type GetPageRankArgs struct {
	Limit   int     `json:"limit,omitempty" jsonschema_description:"Maximum number of results (default 20)"`
	Damping float64 `json:"damping,omitempty" jsonschema_description:"Damping factor (default 0.85)"`
}

// Analysis tools

// ListFilesArgs represents arguments for list_files tool.
type ListFilesArgs struct {
	// No arguments needed
}

// GetStatsArgs represents arguments for get_stats tool.
type GetStatsArgs struct {
	// No arguments needed
}

// FindDeadCodeArgs represents arguments for find_dead_code tool.
type FindDeadCodeArgs struct {
	// No arguments needed
}

// FindHotspotsArgs represents arguments for find_hotspots tool.
type FindHotspotsArgs struct {
	Limit int `json:"limit,omitempty" jsonschema_description:"Maximum number of results (default 20)"`
}

// Source tools

// GetSourceCodeArgs represents arguments for get_source_code tool.
type GetSourceCodeArgs struct {
	IDs []int64 `json:"ids" jsonschema_description:"Array of symbol IDs to retrieve source code for"`
}

// EmbeddingStatusArgs represents arguments for embedding_status tool.
type EmbeddingStatusArgs struct {
	// No arguments needed
}

// ArchCheckArgs represents arguments for arch_check tool.
type ArchCheckArgs struct {
	ProjectID *int64 `json:"project_id,omitempty" jsonschema_description:"Filter by project ID (optional)"`
}

// ListCommunitiesArgs represents arguments for list_communities tool.
type ListCommunitiesArgs struct {
	MinSize int `json:"min_size,omitempty" jsonschema_description:"Minimum community size to include (default 2)"`
	Limit   int `json:"limit,omitempty" jsonschema_description:"Maximum number of communities (default 20)"`
}

// GetModulesArgs represents arguments for get_modules tool.
type GetModulesArgs struct {
	Depth int `json:"depth,omitempty" jsonschema_description:"Path depth for module grouping (default 2)"`
	Limit int `json:"limit,omitempty" jsonschema_description:"Maximum results (default 30)"`
}

// GetNodeByFileArgs represents arguments for get_node_by_file tool.
type GetNodeByFileArgs struct {
	File  string `json:"file" jsonschema_description:"File path or pattern to match"`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Maximum results (default 50)"`
}

// ListProcessesArgs represents arguments for list_processes tool.
type ListProcessesArgs struct {
	Limit int `json:"limit,omitempty" jsonschema_description:"Maximum number of processes (default 50)"`
}

// GetProcessArgs represents arguments for get_process tool.
type GetProcessArgs struct {
	ID string `json:"id" jsonschema_description:"Process ID (e.g. proc_handleBuild_123)"`
}

// File system tools

// ReadFileArgs represents arguments for read_file tool.
type ReadFileArgs struct {
	Path      string `json:"path" jsonschema_description:"Absolute or relative file path to read"`
	StartLine int    `json:"start_line,omitempty" jsonschema_description:"First line to read, 1-based inclusive (default: 1)"`
	EndLine   int    `json:"end_line,omitempty" jsonschema_description:"Last line to read, 1-based inclusive (default: all)"`
}

// WriteFileArgs represents arguments for write_file tool.
type WriteFileArgs struct {
	Path    string `json:"path" jsonschema_description:"Absolute or relative file path to write"`
	Content string `json:"content" jsonschema_description:"Full content to write to the file (overwrites existing)"`
}

// RunCommandArgs represents arguments for run_command tool.
type RunCommandArgs struct {
	Command string   `json:"command" jsonschema_description:"Command to run"`
	Args    []string `json:"args,omitempty" jsonschema_description:"Command arguments as a list"`
	Cwd     string   `json:"cwd,omitempty" jsonschema_description:"Working directory (default: project root)"`
	Timeout int      `json:"timeout,omitempty" jsonschema_description:"Timeout in seconds (default: 30, max: 120)"`
}

// SmartReadArgs represents arguments for smart_read tool.
type SmartReadArgs struct {
	Path string `json:"path" jsonschema_description:"Absolute or relative file path to read"`
	Mode string `json:"mode,omitempty" jsonschema_description:"Reading mode: 'auto' (default), 'full', 'outline', 'symbols'. Auto selects based on file size."`
}

// CCE (Cognitive Context Engine) tools

// GetContextArgs represents arguments for get_context tool.
type GetContextArgs struct {
	Query      string  `json:"query" jsonschema_description:"The query describing what code context you need"`
	Template   string  `json:"template,omitempty" jsonschema_description:"Context template: understand_function, change_impact, debug_trace, explore_module, general (default: general)"`
	MaxTokens  int     `json:"max_tokens,omitempty" jsonschema_description:"Maximum context tokens budget (default: 4000)"`
	MaxResults int     `json:"max_results,omitempty" jsonschema_description:"Maximum number of sources to gather (default: 15)"`
	MinScore   float64 `json:"min_score,omitempty" jsonschema_description:"Minimum relevance score 0.0-1.0 (default: 0.15)"`
}

// ListContextTemplatesArgs represents arguments for list_context_templates tool.
type ListContextTemplatesArgs struct {
	// No arguments needed
}