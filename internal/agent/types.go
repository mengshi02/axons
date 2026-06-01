// Package agent provides AI agent functionality for code analysis.
package agent

import (
	"context"

	"github.com/mengshi02/axons/internal/agent/llm"
)

// Agent interface - enables future framework switching
type Agent interface {
	// Run executes a conversation and returns an event stream
	Run(ctx context.Context, req *RunRequest) <-chan Event

	// Close releases resources
	Close() error
}

// RunRequest represents a run request
type RunRequest struct {
	SessionID string   // Session ID for memory
	Message   string   // User message
	Context   string   // Optional context
	Images    []string // Image base64 dataUrl list (multimodal)
}

// Event represents an event type
type Event struct {
	Type          string         `json:"type"`            // token, tool_start, tool_end, done, error, thinking
	Content       string         `json:"content"`         // Content
	ToolName      string         `json:"tool_name"`       // Tool name
	ToolArgs      map[string]any `json:"tool_args"`       // Tool arguments
	ToolResult    string         `json:"tool_result"`     // Tool result
	DurationMs    int64          `json:"duration_ms"`     // Tool execution duration in milliseconds
	Error         string         `json:"error"`           // Error message
	ErrorType     string         `json:"error_type"`      // Error type: rate_limit, auth_error, server_error, unknown
	Retryable     bool           `json:"retryable"`       // Whether the error is retryable
	ModifiedFiles []string       `json:"modified_files"`  // Files modified by tool (e.g., write_file, replace_file)
}

// AgentOptions represents agent configuration
type AgentOptions struct {
	LLM          llm.Client
	Memory       Memory
	Tools        map[string]Tool
	MaxRounds    int    // Maximum number of tool call rounds
	SystemPrompt string
}

// Message represents a message
type Message struct {
	Role      string `json:"role"`       // user, assistant, tool
	Content   string `json:"content"`
	Name      string `json:"name,omitempty"`       // Tool name (when role=tool)
	CreatedAt string `json:"created_at,omitempty"` // Timestamp
}

// SessionInfo represents session metadata
type SessionInfo struct {
	SessionID    string `json:"session_id"`
	AgentID      string `json:"agent_id"`
	MessageCount int    `json:"message_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// Memory interface for conversation memory
type Memory interface {
	// Add adds a message
	Add(ctx context.Context, sessionID, role, content string) error

	// AddWithMeta adds a message with full metadata (projectID, agentID)
	AddWithMeta(ctx context.Context, sessionID, projectID, agentID, role, content string) error

	// GetHistory retrieves historical messages, filtered by projectID and agentID if provided
	GetHistory(ctx context.Context, sessionID, projectID, agentID string, limit int) ([]Message, error)

	// Clear clears session history and its delegated sub-sessions
	Clear(ctx context.Context, sessionID string) error

	// Close releases resources
	Close() error
}

// AgentRegistry enables Orchestrator to discover and invoke sub-agents
type AgentRegistry interface {
	// ListAgentProfiles returns all available agents (built-in + custom)
	ListAgentProfiles() []AgentProfile

	// BuildAgent builds an executable agent instance by ID
	// The optional projectID parameter scopes the agent to a specific project database
	// The optional modelID parameter specifies which LLM model to use
	BuildAgent(agentID string, opts ...BuildAgentOption) Agent
}

// BuildAgentOption is a functional option for BuildAgent
type BuildAgentOption func(*BuildAgentOptions)

// BuildAgentOptions holds optional parameters for BuildAgent
type BuildAgentOptions struct {
	ProjectID string
	ModelID   string
}

// WithProjectID sets the project ID for BuildAgent
func WithProjectID(projectID string) BuildAgentOption {
	return func(o *BuildAgentOptions) {
		o.ProjectID = projectID
	}
}

// WithModelID sets the model ID for BuildAgent
func WithModelID(modelID string) BuildAgentOption {
	return func(o *BuildAgentOptions) {
		o.ModelID = modelID
	}
}

// Tool interface represents a tool
type Tool interface {
	// Name returns the tool name
	Name() string

	// Description returns the tool description
	Description() string

	// Parameters returns the parameter schema (for LLM)
	Parameters() map[string]any

	// Execute executes the tool
	Execute(ctx context.Context, args map[string]any) (string, error)
}