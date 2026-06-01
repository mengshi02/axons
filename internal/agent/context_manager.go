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

// CompressMessages compresses message history
// When message count exceeds limit, keeps system message and recent messages, summarizes the middle part
func (cm *ContextManager) CompressMessages(messages []llm.Message) []llm.Message {
	if len(messages) <= cm.maxMessages {
		return messages
	}

	// Keep system message (first one)
	if len(messages) == 0 || messages[0].Role != "system" {
		// No system message, directly take recent messages
		return messages[len(messages)-cm.maxMessages:]
	}

	systemMsg := messages[0]
	remaining := messages[1:]

	// Calculate message range to compress
	recentCount := int(float64(cm.maxMessages) * cm.compressionRatio)
	if recentCount < 1 {
		recentCount = 1
	}

	// Keep recent messages
	recent := remaining
	middle := remaining
	if len(remaining) > recentCount {
		recent = remaining[len(remaining)-recentCount:]
		middle = remaining[:len(remaining)-recentCount]
	} else {
		// When remaining messages are fewer than recentCount,
		// keep all remaining as recent messages, no middle messages to summarize
		middle = nil
	}

	// Summarize middle messages
	summary := cm.summarizeMessages(middle)

	// Rebuild message list
	compressed := []llm.Message{systemMsg}
	if summary != "" {
		compressed = append(compressed, llm.Message{
			Role:    "system",
			Content: fmt.Sprintf("[Context Summary: %s]", summary),
		})
	}
	compressed = append(compressed, recent...)

	return compressed
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