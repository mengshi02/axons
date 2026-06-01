package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mengshi02/axons/internal/logger"
)

// ToolExecutor interface for decoupling MCP Server dependency
type ToolExecutor interface {
	// CallToolDirect directly calls a tool
	CallToolDirect(ctx context.Context, name string, args map[string]any) (any, error)
}

// MCPTool wraps an MCP tool as an Agent Tool
type MCPTool struct {
	name        string
	description string
	parameters  map[string]any
	executor    ToolExecutor
}

// NewMCPTool creates an MCP tool
func NewMCPTool(name, description string, parameters map[string]any, executor ToolExecutor) *MCPTool {
	return &MCPTool{
		name:        name,
		description: description,
		parameters:  parameters,
		executor:    executor,
	}
}

// Name returns the tool name
func (t *MCPTool) Name() string {
	return t.name
}

// Description returns the tool description
func (t *MCPTool) Description() string {
	return t.description
}

// Parameters returns the parameter schema
func (t *MCPTool) Parameters() map[string]any {
	return t.parameters
}

// Execute executes the tool
func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	result, err := t.executor.CallToolDirect(ctx, t.name, args)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}
	return string(data), nil
}

// ToolDefinition represents tool definition
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// DefaultMCPToolDefinitions is the default MCP tool definitions
var DefaultMCPToolDefinitions = []ToolDefinition{
	{
		Name:        "keyword_search",
		Description: "Perform full-text search using FTS5 with BM25 ranking. Fast keyword-based search for code symbols.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "The search query string"},
				"kind":  map[string]any{"type": "string", "description": "Filter by symbol kind (function, method, class, etc.)"},
				"file":  map[string]any{"type": "string", "description": "Filter by file path pattern"},
				"limit": map[string]any{"type": "integer", "description": "Maximum number of results (default 20)"},
			},
			"required": []string{"query"},
		},
	},
	{
		Name:        "hybrid_search",
		Description: "Perform hybrid search combining FTS5 keyword search and semantic vector search. Best for comprehensive code search.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":     map[string]any{"type": "string", "description": "Natural language or keyword query"},
				"kind":      map[string]any{"type": "string", "description": "Filter by symbol kind"},
				"file":      map[string]any{"type": "string", "description": "Filter by file path pattern"},
				"limit":     map[string]any{"type": "integer", "description": "Maximum number of results (default 10)"},
				"threshold": map[string]any{"type": "number", "description": "Minimum similarity score 0.0-1.0 (default 0.2)"},
			},
			"required": []string{"query"},
		},
	},
	{
		Name:        "search_symbols",
		Description: "Search for symbols in the code graph by name pattern",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "The name pattern to search for"},
				"limit":   map[string]any{"type": "integer", "description": "Maximum results"},
			},
			"required": []string{"pattern"},
		},
	},
	{
		Name:        "get_symbol",
		Description: "Get detailed information about a symbol by ID",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "integer", "description": "The symbol ID"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "find_callers",
		Description: "Find all functions that call a given symbol",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "integer", "description": "The symbol ID"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "find_callees",
		Description: "Find all functions called by a given symbol",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "integer", "description": "The symbol ID"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "get_source_code",
		Description: "Retrieve the source code content for one or more symbols by their IDs",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ids": map[string]any{"type": "array", "description": "Array of symbol IDs to retrieve source code for"},
			},
			"required": []string{"ids"},
		},
	},
	{
		Name:        "list_files",
		Description: "List all indexed files in the code graph",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "get_stats",
		Description: "Get statistics about the code graph",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "find_dead_code",
		Description: "Find potentially dead code (unused functions)",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		Name:        "find_hotspots",
		Description: "Find code hotspots (highly connected functions)",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{"type": "integer", "description": "Maximum number of results (default 20)"},
			},
		},
	},
	{
		Name:        "path",
		Description: "Find shortest path between two symbols",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from_id":   map[string]any{"type": "integer", "description": "Source symbol ID"},
				"to_id":     map[string]any{"type": "integer", "description": "Target symbol ID"},
				"max_depth": map[string]any{"type": "integer", "description": "Maximum BFS depth (default 6)"},
			},
			"required": []string{"from_id", "to_id"},
		},
	},
	{
		Name:        "find_impact",
		Description: "Find all symbols impacted by a change to the given symbol (reverse BFS). Returns callers up to max_depth hops away.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":        map[string]any{"type": "integer", "description": "The symbol ID to analyze impact for"},
				"max_depth": map[string]any{"type": "integer", "description": "Maximum BFS depth (default 3)"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "find_call_chain",
		Description: "Find all call paths between two symbols using BFS. Returns every route from source to target.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from_id":   map[string]any{"type": "integer", "description": "Source symbol ID"},
				"to_id":     map[string]any{"type": "integer", "description": "Target symbol ID"},
				"max_depth": map[string]any{"type": "integer", "description": "Maximum depth (default 5)"},
			},
			"required": []string{"from_id", "to_id"},
		},
	},
	{
		Name:        "get_complexity",
		Description: "Get cyclomatic and cognitive complexity metrics for a function symbol.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "integer", "description": "The symbol ID"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "get_cochanges",
		Description: "Get file co-change pairs that frequently change together (git coupling analysis).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file":      map[string]any{"type": "string", "description": "Filter by file path pattern"},
				"limit":     map[string]any{"type": "integer", "description": "Maximum results (default 20)"},
				"min_count": map[string]any{"type": "integer", "description": "Minimum co-change count (default 2)"},
			},
		},
	},
	{
		Name:        "get_pagerank",
		Description: "Get top-N most important symbols ranked by PageRank on the call graph.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit":   map[string]any{"type": "integer", "description": "Maximum results (default 20)"},
				"damping": map[string]any{"type": "number", "description": "Damping factor (default 0.85)"},
			},
		},
	},
	{
		Name:        "arch_check",
		Description: "Check the codebase against stored architecture deny rules. Returns violations where files in from_pattern depend on files in to_pattern.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "integer", "description": "Filter by project ID (optional)"},
			},
		},
	},
	{
		Name:        "list_communities",
		Description: "Detect module communities in the call graph using Louvain algorithm. Returns clusters of closely related symbols.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"min_size": map[string]any{"type": "integer", "description": "Minimum community size to include (default 2)"},
				"limit":    map[string]any{"type": "integer", "description": "Maximum number of communities (default 20)"},
			},
		},
	},
	{
		Name:        "get_modules",
		Description: "List top-level modules or packages in the codebase with symbol counts. Useful for architecture overview.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"depth": map[string]any{"type": "integer", "description": "Path depth for module grouping (default 2)"},
				"limit": map[string]any{"type": "integer", "description": "Maximum results (default 30)"},
			},
		},
	},
	{
		Name:        "get_node_by_file",
		Description: "Find all symbols defined in a given file. Useful for exploring a specific file's contents.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file":  map[string]any{"type": "string", "description": "File path or pattern to match"},
				"limit": map[string]any{"type": "integer", "description": "Maximum results (default 50)"},
			},
			"required": []string{"file"},
		},
	},
	{
		Name:        "list_processes",
		Description: "List materialized execution flows (processes) detected during build. Each process traces a multi-hop call chain from an entry point. Useful for understanding how features are implemented.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "integer", "description": "Filter by project ID (optional)"},
				"limit":      map[string]any{"type": "integer", "description": "Maximum number of processes (default 50)"},
			},
		},
	},
	{
		Name:        "get_process",
		Description: "Get detailed steps of a specific execution flow (process). Returns ordered call chain with file locations. Use list_processes to find process IDs.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Process ID (e.g. proc_handleBuild_123)"},
			},
			"required": []string{"id"},
		},
	},
	{
		Name:        "read_file",
		Description: "Read the raw content of any file by path. IMPORTANT: 'path' is a REQUIRED parameter. Supports optional line range for partial reads. Content is capped at 512KB. Path must be within the project root (path traversal is blocked). Useful for config files, SQL migrations, READMEs, and other non-indexed files.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string", "description": "REQUIRED. Absolute or relative file path to read."},
				"start_line": map[string]any{"type": "integer", "description": "Optional. First line to read, 1-based inclusive (default: 1)"},
				"end_line":   map[string]any{"type": "integer", "description": "Optional. Last line to read, 1-based inclusive (default: all)"},
			},
			"required": []string{"path"},
		},
	},
	{
		Name:        "smart_read",
		Description: "Intelligently read a file based on its size. Automatically selects the best strategy: small files (<500 lines) are read entirely; medium files (500-2000 lines) are truncated to show head (imports, declarations) and tail; large files (>2000 lines) return an outline and suggest using search tools. Use this instead of read_file for code files when you don't know the file size.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "REQUIRED. Absolute or relative file path to read."},
				"mode": map[string]any{"type": "string", "description": "Optional. Reading mode: 'auto' (default), 'full', 'truncated', 'outline'. Auto selects based on file size."},
			},
			"required": []string{"path"},
		},
	},
	{
		Name:        "write_file",
		Description: "Write content to a file, creating parent directories if needed. IMPORTANT: Both 'path' and 'content' are REQUIRED parameters. The file will be overwritten if it exists. Path must be within the project root (path traversal is blocked). Returns the absolute path and number of bytes written.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "REQUIRED. Absolute or relative file path to write. Parent directories will be created automatically."},
				"content": map[string]any{"type": "string", "description": "REQUIRED. Full content to write to the file. Will overwrite existing content completely."},
			},
			"required": []string{"path", "content"},
		},
	},
	{
		Name:        "run_command",
		Description: "Run a shell command and return stdout, stderr, exit code. IMPORTANT: 'command' is REQUIRED. Only allowlisted commands are permitted: go, python, python3, node, npm, npx, cargo, rustc, javac, java, mvn, gradle, git, make, sh, bash. Timeout defaults to 30s (max 120s). Working directory defaults to project root.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "REQUIRED. Command to run (must be in allowlist)"},
				"args":    map[string]any{"type": "array", "description": "Optional. Command arguments as an array of strings"},
				"cwd":     map[string]any{"type": "string", "description": "Optional. Working directory (default: project root)"},
				"timeout": map[string]any{"type": "integer", "description": "Optional. Timeout in seconds (default: 30, max: 120)"},
			},
			"required": []string{"command"},
		},
	},
	{
		Name:        "delegate_to_agent",
		Description: "Delegate a subtask to a specialized sub-agent and synchronously return its result. Use this to break down complex tasks.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent_id": map[string]any{"type": "string", "description": "Sub-agent ID to delegate to (e.g. 'architect', 'quality', 'impact', 'engineer', or a custom agent ID)"},
				"task":     map[string]any{"type": "string", "description": "Self-contained subtask description for the sub-agent"},
			},
			"required": []string{"agent_id", "task"},
		},
	},
}

// WrapTools creates a tool set from a tool executor
func WrapTools(executor ToolExecutor) map[string]Tool {
	tools := make(map[string]Tool)
	for _, def := range DefaultMCPToolDefinitions {
		tools[def.Name] = NewMCPTool(def.Name, def.Description, def.Parameters, executor)
	}
	return tools
}

// WrapToolsWithFilter creates a specified tool set from a tool executor
func WrapToolsWithFilter(executor ToolExecutor, toolNames []string) map[string]Tool {
	allTools := WrapTools(executor)
	result := make(map[string]Tool)
	for _, name := range toolNames {
		if tool, ok := allTools[name]; ok {
			result[name] = tool
		}
	}
	return result
}

// DelegateAgentTool allows Orchestrator to delegate subtasks to other agents
type DelegateAgentTool struct {
	registry AgentRegistry
}

// NewDelegateAgentTool creates a delegation tool
func NewDelegateAgentTool(registry AgentRegistry) *DelegateAgentTool {
	return &DelegateAgentTool{registry: registry}
}

func (t *DelegateAgentTool) Name() string { return "delegate_to_agent" }

func (t *DelegateAgentTool) Description() string {
	return "Delegate a subtask to a specialized sub-agent and synchronously return its result. Use this to break down complex tasks and leverage domain-specific agents."
}

func (t *DelegateAgentTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id": map[string]any{
				"type":        "string",
				"description": "The ID of the sub-agent to delegate to (e.g. 'architect', 'quality', 'impact', 'engineer', or a custom agent ID)",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "A clear, self-contained description of the subtask for the sub-agent to solve",
			},
		},
		"required": []string{"agent_id", "task"},
	}
}

func (t *DelegateAgentTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	agentID, _ := args["agent_id"].(string)
	task, _ := args["task"].(string)

	if agentID == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	if task == "" {
		return "", fmt.Errorf("task is required")
	}
	// Prevent delegation to self (avoid recursion)
	if agentID == "default" {
		return "", fmt.Errorf("cannot delegate to the orchestrator itself (agent_id: default)")
	}

	// Get project ID and model ID from context
	projectID := GetProjectIDFromContext(ctx)
	modelID := GetModelIDFromContext(ctx)
	logger.S().Infow("[DelegateAgentTool] Delegating to sub-agent",
		"agent_id", agentID,
		"project_id", projectID,
		"model_id", modelID,
		"task_length", len(task),
		"context_err", ctx.Err())
	
	// Check if context is already canceled
	if ctx.Err() != nil {
		logger.S().Errorw("[DelegateAgentTool] Context already canceled before delegation",
			"agent_id", agentID,
			"context_error", ctx.Err())
		return "", fmt.Errorf("context canceled: %v", ctx.Err())
	}
	
	var sub Agent
	var opts []BuildAgentOption
	if projectID != "" {
		opts = append(opts, WithProjectID(projectID))
	}
	if modelID != "" {
		opts = append(opts, WithModelID(modelID))
	}
	sub = t.registry.BuildAgent(agentID, opts...)
	if sub == nil {
		return "", fmt.Errorf("agent %q not found", agentID)
	}

	// Build session_id for sub-agent: mainSessionID#agentID
	// This links sub-agent memory to the main conversation
	mainSessionID := GetSessionIDFromContext(ctx)
	var sessionID string
	if mainSessionID != "" {
		sessionID = fmt.Sprintf("%s#%s", mainSessionID, agentID)
	} else {
		// Fallback if no main session (shouldn't happen in normal flow)
		sessionID = fmt.Sprintf("delegate-%s", agentID)
	}

	logger.S().Debugw("[DelegateAgentTool] Starting sub-agent run",
		"agent_id", agentID,
		"session_id", sessionID,
		"main_session_id", mainSessionID)

	var result string
	eventCount := 0
	for event := range sub.Run(ctx, &RunRequest{
		SessionID: sessionID,
		Message:   task,
	}) {
		eventCount++
		if event.Type == "token" {
			result += event.Content
		}
		if event.Type == "error" && event.Error != "" {
			logger.S().Errorw("[DelegateAgentTool] Sub-agent error",
				"agent_id", agentID,
				"error", event.Error,
				"events_received", eventCount)
			return "", fmt.Errorf("sub-agent %q error: %s", agentID, event.Error)
		}
	}
	logger.S().Infow("[DelegateAgentTool] Sub-agent completed",
		"agent_id", agentID,
		"result_length", len(result),
		"events_received", eventCount,
		"context_err", ctx.Err())
	return result, nil
}