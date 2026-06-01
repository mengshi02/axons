package agent

import (
	"context"
)

// contextKey is a type for context keys in the agent package
type contextKey string

const (
	// ProjectIDKey is the context key for project ID
	ProjectIDKey contextKey = "project_id"
	// ModelIDKey is the context key for model ID
	ModelIDKey contextKey = "model_id"
	// SessionIDKey is the context key for session ID (main conversation session)
	SessionIDKey contextKey = "session_id"
	// AgentIDKey is the context key for agent ID
	AgentIDKey contextKey = "agent_id"
)

// GetProjectIDFromContext extracts project ID from context
func GetProjectIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if projectID, ok := ctx.Value(ProjectIDKey).(string); ok {
		return projectID
	}
	return ""
}

// GetModelIDFromContext extracts model ID from context
func GetModelIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if modelID, ok := ctx.Value(ModelIDKey).(string); ok {
		return modelID
	}
	return ""
}

// GetSessionIDFromContext extracts session ID from context
func GetSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if sessionID, ok := ctx.Value(SessionIDKey).(string); ok {
		return sessionID
	}
	return ""
}

// GetAgentIDFromContext extracts agent ID from context
func GetAgentIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if agentID, ok := ctx.Value(AgentIDKey).(string); ok {
		return agentID
	}
	return ""
}