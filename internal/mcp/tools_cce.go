package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mengshi02/axons/internal/cce"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerCCETools registers CCE (Cognitive Context Engine) MCP tools.
func (s *MCPServer) registerCCETools() {
	// get_context - Retrieve and assemble code context
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_context",
		Description: "Retrieve and assemble code context using the Code Context Engine (CCE). Combines semantic search, keyword search, and graph traversal to provide structured, relevant code context for understanding and modifying code. Powered by axons-cce.",
	}, s.handleGetContext)

	// list_context_templates - List available context templates
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_context_templates",
		Description: "List available context templates for the Code Context Engine. Templates define strategies for collecting and organizing code context (e.g., understand_function, change_impact, debug_trace).",
	}, s.handleListContextTemplates)
}

// handleGetContext handles the get_context MCP tool.
func (s *MCPServer) handleGetContext(ctx context.Context, req *mcp.CallToolRequest, args GetContextArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	if args.Query == "" {
		return nil, nil, fmt.Errorf("query parameter is required")
	}

	template := args.Template
	if template == "" {
		template = "general"
	}
	maxTokens := args.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 15
	}
	minScore := float32(args.MinScore)
	if minScore <= 0 {
		minScore = 0.15
	}

	engine := s.getCCEEngine()
	if engine == nil {
		return nil, map[string]interface{}{
			"available": false,
			"message":   "Code Context Engine (CCE) is not available. Ensure embeddings are generated first by running 'axons embed --mode dual'.",
		}, nil
	}

	cceQuery := &cce.RetrievalQuery{
		Query:      args.Query,
		Template:   cce.ContextTemplate(template),
		MaxTokens:  maxTokens,
		MaxResults: maxResults,
		MinScore:   minScore,
	}

	// Detect anchors from the query using FTS5
	anchors := s.detectAnchors(args.Query)
	if len(anchors) > 0 {
		cceQuery.Anchors = anchors
	}

	result, err := engine.GetContext(ctx, cceQuery)
	if err != nil {
		logger.S().Errorw("[MCP-CCE] GetContext failed", "error", err)
		return nil, nil, fmt.Errorf("CCE retrieval failed: %w", err)
	}

	// Format result for MCP
	contextText := result.FormatContextForLLM()
	banner := cce.FormatCCEBanner(cceQuery.Template, len(result.Sources))

	output := fmt.Sprintf("%s\n\n%s", banner, contextText)

	return nil, map[string]interface{}{
		"context":  output,
		"template": string(cceQuery.Template),
		"sources":  len(result.Sources),
		"tokens":   result.TotalTokens,
		"banner":   banner,
	}, nil
}

// handleListContextTemplates handles the list_context_templates MCP tool.
func (s *MCPServer) handleListContextTemplates(_ context.Context, _ *mcp.CallToolRequest, args ListContextTemplatesArgs) (*mcp.CallToolResult, map[string]interface{}, error) {
	templates := cce.BuiltinTemplates()

	data, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal templates: %w", err)
	}

	return nil, map[string]interface{}{
		"templates": string(data),
		"count":     len(templates),
	}, nil
}

// getCCEEngine creates or returns a cached CCE engine.
func (s *MCPServer) getCCEEngine() *cce.Engine {
	if s.embedder == nil || s.repo == nil {
		return nil
	}
	return cce.NewEngine(s.repo, s.embedder, s.rootPath)
}

// detectAnchors uses FTS5 search to find potential anchor symbols.
func (s *MCPServer) detectAnchors(query string) []cce.Anchor {
	if s.repo == nil {
		return nil
	}

	results, err := s.repo.FTS5Search(query, 3)
	if err != nil {
		return nil
	}

	var anchors []cce.Anchor
	for _, r := range results {
		anchors = append(anchors, cce.Anchor{
			NodeID:     r.NodeID,
			SymbolName: r.Name,
			Kind:       r.Kind,
			File:       r.File,
		})
	}
	return anchors
}
