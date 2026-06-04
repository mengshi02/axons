package agent

import (
	"fmt"
	"strings"

	"github.com/mengshi02/axons/internal/agent/llm"
)

// ContextManager manages context for conversation history
// Used to optimize message history and prevent context bloat
type ContextManager struct {
	maxMessages      int     // Maximum number of messages
	maxTokens        int     // Maximum token count estimation
	compressionRatio float64 // Compression ratio (ratio of recent messages to keep)
}

// NewContextManager creates a context manager
func NewContextManager(maxMessages, maxTokens int, compressionRatio float64) *ContextManager {
	if maxMessages <= 0 {
		maxMessages = 50
	}
	if maxTokens <= 0 {
		maxTokens = 8000
	}
	if compressionRatio <= 0 || compressionRatio > 1 {
		compressionRatio = 0.5
	}
	return &ContextManager{
		maxMessages:      maxMessages,
		maxTokens:        maxTokens,
		compressionRatio: compressionRatio,
	}
}

// CompressMessages compresses message history while preserving structural validity
// required by LLM APIs:
//
//  1. The first system message (if any) is always kept.
//  2. The compressed slice MUST contain at least one role=user message — many
//     gateways (including the one in handlers_chat.go) reject requests
//     without a user query ("No user query found in messages").
//  3. tool_calls/tool messages must remain paired: a role=tool message must be
//     preceded (somewhere earlier in the slice) by an assistant message whose
//     ToolCalls includes its ToolID. We never start the kept window with a
//     role=tool message orphaned from its assistant parent.
//
// Strategy:
//   - Keep system[0] (if present).
//   - Pick a recent tail window of size `recentCount`, then expand it leftward
//     to a turn boundary so it does not start with an orphan role=tool message.
//   - Replace dropped middle messages with a single system summary.
//   - If the recent window does not contain any user message, splice the most
//     recent user message from the dropped middle BEFORE the recent window so
//     the LLM still sees a user query.
func (cm *ContextManager) CompressMessages(messages []llm.Message) []llm.Message {
	if len(messages) <= cm.maxMessages {
		return messages
	}

	// Identify system prefix (only first system message, conventionally messages[0])
	var systemMsg *llm.Message
	body := messages
	if len(messages) > 0 && messages[0].Role == "system" {
		s := messages[0]
		systemMsg = &s
		body = messages[1:]
	}

	if len(body) == 0 {
		// Edge case: only system message present
		return messages
	}

	// Final safety net: if the full conversation has no user message whatsoever,
	// we cannot synthesize one — return as-is rather than emit an invalid request.
	if !containsUser(body) {
		return messages
	}

	// Desired tail size (excluding system).
	recentCount := int(float64(cm.maxMessages) * cm.compressionRatio)
	if recentCount < 1 {
		recentCount = 1
	}
	if recentCount > len(body) {
		recentCount = len(body)
	}

	startIdx := len(body) - recentCount

	// Expand leftward so the kept window does not start with an orphan tool
	// message (its parent assistant must be in scope).
	startIdx = adjustStartForToolPairing(body, startIdx)

	dropped := body[:startIdx]
	recent := body[startIdx:]

	// If recent window has no user message, lift the most recent user message
	// from the dropped middle and place it just before recent. This is what
	// the LLM gateway requires.
	var liftedUser *llm.Message
	if !containsUser(recent) {
		for i := len(dropped) - 1; i >= 0; i-- {
			if dropped[i].Role == "user" {
				u := dropped[i]
				liftedUser = &u
				break
			}
		}
	}

	// Build compressed result.
	out := make([]llm.Message, 0, len(recent)+3)
	if systemMsg != nil {
		out = append(out, *systemMsg)
	}
	if summary := cm.summarizeMessages(dropped); summary != "" {
		out = append(out, llm.Message{
			Role:    "system",
			Content: fmt.Sprintf("[Context Summary: %s]", summary),
		})
	}
	if liftedUser != nil {
		out = append(out, *liftedUser)
	}
	out = append(out, recent...)

	// Defensive post-check (should not happen given the lift above, but the
	// safety net keeps us from ever shipping an invalid request).
	if !containsUser(out) {
		return messages
	}

	return out
}

// adjustStartForToolPairing moves startIdx left until body[startIdx] is not a
// role=tool message. A role=tool message must be preceded by its parent
// assistant message in the same slice, otherwise the LLM API rejects the
// payload (orphan tool result).
//
// In practice we step the boundary left past any leading tool messages, then
// past the assistant message that owns them — so the kept window starts on a
// clean turn boundary.
func adjustStartForToolPairing(body []llm.Message, startIdx int) int {
	if startIdx <= 0 {
		return 0
	}
	// Walk left while the boundary is a tool message.
	for startIdx > 0 && body[startIdx].Role == "tool" {
		startIdx--
	}
	// If we landed on an assistant message that has tool_calls, also include
	// the assistant itself (it should already be at startIdx). If somehow the
	// assistant lives further left (e.g. interleaving), step left until we
	// find it. Safe upper bound: stop at 0.
	if startIdx > 0 && body[startIdx].Role == "tool" {
		// Should not happen given the loop above, but defensive.
		startIdx = 0
	}
	return startIdx
}

func containsUser(msgs []llm.Message) bool {
	for _, m := range msgs {
		if m.Role == "user" {
			return true
		}
	}
	return false
}

// summarizeMessages summarizes messages
func (cm *ContextManager) summarizeMessages(messages []llm.Message) string {
	if len(messages) == 0 {
		return ""
	}

	toolCalls := 0
	filesRead := make(map[string]bool)
	filesWritten := make(map[string]bool)
	commandsRun := 0

	for _, msg := range messages {
		if msg.Role == "tool" {
			toolCalls++

			// Analyze tool call results and extract key information
			content := msg.Content
			name := msg.Name

			switch name {
			case "read_file":
				// Extract read files
				if path := extractFilePath(content); path != "" {
					filesRead[path] = true
				}
			case "write_file", "replace_file":
				// Extract written files
				if path := extractFilePath(content); path != "" {
					filesWritten[path] = true
				}
			case "run_command":
				commandsRun++
			}
		}
	}

	// Build summary
	var parts []string
	parts = append(parts, fmt.Sprintf("%d tool calls executed", toolCalls))

	if len(filesRead) > 0 {
		parts = append(parts, fmt.Sprintf("%d files read", len(filesRead)))
	}
	if len(filesWritten) > 0 {
		parts = append(parts, fmt.Sprintf("%d files written", len(filesWritten)))
	}
	if commandsRun > 0 {
		parts = append(parts, fmt.Sprintf("%d commands run", commandsRun))
	}

	return strings.Join(parts, ", ")
}

// extractFilePath extracts file path from tool result
// This is a simple implementation that can be optimized based on actual tool return format
func extractFilePath(content string) string {
	// Common formats:
	// - "File written to: /path/to/file"
	// - "Read file: /path/to/file"
	// - "path": "/path/to/file"

	// Try to extract "path": "..." format
	if strings.Contains(content, `"path"`) {
		// Simple extraction
		parts := strings.Split(content, `"path"`)
		if len(parts) > 1 {
			pathPart := parts[1]
			// Extract content in quotes
			if idx := strings.Index(pathPart, `"`); idx != -1 {
				pathPart = pathPart[idx+1:]
				if idx := strings.Index(pathPart, `"`); idx != -1 {
					return pathPart[:idx]
				}
			}
		}
	}

	// Try to extract "File ... to: /path" format
	keywords := []string{"written to:", "read:", "modified:", "created:"}
	for _, keyword := range keywords {
		if idx := strings.Index(strings.ToLower(content), keyword); idx != -1 {
			pathPart := content[idx+len(keyword):]
			pathPart = strings.TrimSpace(pathPart)
			// Extract first line
			if idx := strings.Index(pathPart, "\n"); idx != -1 {
				pathPart = pathPart[:idx]
			}
			return strings.TrimSpace(pathPart)
		}
	}

	return ""
}

// EstimateTokens estimates the token count of messages
// This is a rough estimate, actual token count depends on the specific tokenizer
func (cm *ContextManager) EstimateTokens(messages []llm.Message) int {
	totalTokens := 0
	for _, msg := range messages {
		// Simple estimation: each token approximately equals 4 characters
		content := msg.Content
		tokens := len(content) / 4
		if tokens == 0 {
			tokens = 1
		}
		totalTokens += tokens

		// Add role overhead
		totalTokens += 4 // role + formatting

		// If there are tool calls, add extra tokens
		if len(msg.ToolCalls) > 0 {
			totalTokens += len(msg.ToolCalls) * 10
		}
	}
	return totalTokens
}