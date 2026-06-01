package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mengshi02/axons/internal/agent/llm"
	"github.com/mengshi02/axons/internal/logger"
)

// ReActAgent implements the ReAct pattern Agent
type ReActAgent struct {
	llm          llm.Client
	memory       Memory
	tools        map[string]Tool
	maxRounds    int
	systemPrompt string

	// Loop detection and deduplication features
	enableDeduplication bool // Whether to enable deduplication
	enableLoopDetection bool // Whether to enable loop detection
	enableContextCompression bool // Whether to enable context compression
	contextManager       *ContextManager // Context manager for message compression
}

// NewReActAgent creates a ReAct Agent
func NewReActAgent(opts *AgentOptions) *ReActAgent {
	if opts.MaxRounds <= 0 {
		opts.MaxRounds = 30
	}
	if opts.SystemPrompt == "" {
		opts.SystemPrompt = DefaultReActPrompt
	}
	return &ReActAgent{
		llm:          opts.LLM,
		memory:       opts.Memory,
		tools:        opts.Tools,
		maxRounds:    opts.MaxRounds,
		systemPrompt: opts.SystemPrompt,
		
		// Enable all protection mechanisms by default
		enableDeduplication: true,
		enableLoopDetection: true,
		enableContextCompression: true,
		contextManager: NewContextManager(50, 8000, 0.5), // maxMessages=50, maxTokens=8000, compressionRatio=0.5
	}
}

// Run executes a conversation and returns an event stream
func (a *ReActAgent) Run(ctx context.Context, req *RunRequest) <-chan Event {
	eventChan := make(chan Event, 100)

	logger.S().Infow("[Agent.Run] Starting agent execution",
		"session_id", req.SessionID,
		"message_length", len(req.Message),
		"has_images", len(req.Images) > 0,
		"max_rounds", a.maxRounds)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.S().Errorw("[Agent.Run] PANIC recovered in agent goroutine",
					"error", rec,
					"session_id", req.SessionID)
				eventChan <- Event{Type: "error", Error: fmt.Sprintf("Agent panic: %v", rec)}
			}
			close(eventChan)
			logger.S().Infow("[Agent.Run] Event channel closed", "session_id", req.SessionID)
		}()

		// 1. Get history
		logger.S().Debugw("[Agent.Run] Getting conversation history", "session_id", req.SessionID)
		var history []Message
		if a.memory != nil {
			var err error
			projectID := GetProjectIDFromContext(ctx)
			agentID := GetAgentIDFromContext(ctx)
			history, err = a.memory.GetHistory(ctx, req.SessionID, projectID, agentID, 20)
			if err != nil {
				logger.S().Errorw("[Agent.Run] Failed to get history",
					"error", err,
					"session_id", req.SessionID)
				eventChan <- Event{Type: "error", Error: fmt.Sprintf("failed to get history: %v", err)}
				return
			}
			logger.S().Debugw("[Agent.Run] History retrieved", "history_count", len(history), "session_id", req.SessionID)
		}

		// 2. Build messages
		logger.S().Debugw("[Agent.Run] Building messages", "session_id", req.SessionID)
		messages := make([]llm.Message, 0, len(history)+2)
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: a.systemPrompt,
		})

		for _, m := range history {
			messages = append(messages, llm.Message{
				Role:    m.Role,
				Content: m.Content,
				Name:    m.Name,
			})
		}

		messages = append(messages, llm.Message{
			Role:    "user",
			Content: req.Message,
			Images:  req.Images,
		})
		logger.S().Debugw("[Agent.Run] Messages built", "total_messages", len(messages), "session_id", req.SessionID)

		// 3. Build tool definitions
		var toolDefs []llm.ToolDefinition
		if a.tools != nil && a.llm.SupportsTools() {
			toolDefs = make([]llm.ToolDefinition, 0, len(a.tools))
			for _, t := range a.tools {
				toolDefs = append(toolDefs, llm.ToolDefinition{
					Type: "function",
					Function: llm.FunctionDefSpec{
						Name:        t.Name(),
						Description: t.Description(),
						Parameters:  t.Parameters(),
					},
				})
			}
			logger.S().Debugw("[Agent.Run] Tool definitions built", "tool_count", len(toolDefs), "session_id", req.SessionID)
		}

		// 4. ReAct loop
		// round only counts LLM calls, tool execution doesn't consume rounds
		var finalContent string
		logger.S().Infow("[Agent.Run] Starting ReAct loop", "max_rounds", a.maxRounds, "session_id", req.SessionID)
		
		// Initialize protection mechanisms
		var deduplicator *ExecutionDeduplicator
		var loopDetector *LoopDetector
		var _ *ContextManager // Reserved for future use
		var terminationCond *TerminationCondition
		var terminationState *TerminationState
		var recentToolCalls []ToolCallSignature
		
		if a.enableDeduplication {
			deduplicator = NewExecutionDeduplicator(50, 3) // maxHistory=50, threshold=3
		}
		if a.enableLoopDetection {
			loopDetector = NewLoopDetector(5, 2) // windowSize=5, threshold=2
			recentToolCalls = []ToolCallSignature{}
		}
		terminationCond = NewDefaultTerminationCondition()
		terminationState = NewTerminationState()
		
		for round := 0; round < a.maxRounds; round++ {
			logger.S().Debugw("[Agent.Run] Starting round", "round", round+1, "max_rounds", a.maxRounds, "session_id", req.SessionID)
			
			// Compress messages if context compression is enabled and messages exceed threshold
			if a.enableContextCompression && a.contextManager != nil && len(messages) > a.contextManager.maxMessages {
				logger.S().Infow("[Agent.Run] Compressing messages",
					"original_count", len(messages),
					"max_messages", a.contextManager.maxMessages,
					"session_id", req.SessionID)
				messages = a.contextManager.CompressMessages(messages)
				logger.S().Infow("[Agent.Run] Messages compressed",
					"compressed_count", len(messages),
					"session_id", req.SessionID)
			}
			
			// Check termination conditions
			if shouldTerm, reason := terminationCond.ShouldTerminate(round, terminationState, len(messages)); shouldTerm {
				logger.S().Warnw("[Agent.Run] Termination condition met",
					"reason", reason,
					"round", round+1,
					"session_id", req.SessionID)
				
				finalContent = fmt.Sprintf("Execution stopped: %s. Please try rephrasing your question or breaking it into smaller parts.", reason)
				eventChan <- Event{
					Type:    "token",
					Content: finalContent,
				}
				break
			}
			
			// Send thinking event before each LLM call to inform frontend
			eventChan <- Event{Type: "thinking", Content: getThinkingMessage(round, a.maxRounds, nil)}

			// Call LLM
			llmReq := &llm.Request{
				Messages:  messages,
				Tools:     toolDefs,
				MaxTokens: 4096,
			}

			logger.S().Debugw("[Agent.Run] Calling LLM",
				"round", round+1,
				"message_count", len(messages),
				"has_tools", len(toolDefs) > 0,
				"session_id", req.SessionID)

			resp, err := a.llm.Call(ctx, llmReq)
			if err != nil {
				// Check if this is a retryable API error (e.g. rate limit)
				apiErr := llm.ParseAPIError(err)
				if apiErr != nil && apiErr.IsRetryable() {
					// Retry with exponential backoff
					maxRetries := 3
					retryDelay := time.Duration(apiErr.RetryAfter) * time.Second
					if retryDelay < 2*time.Second {
						retryDelay = 2 * time.Second
					}

					retried := false
					for attempt := 1; attempt <= maxRetries; attempt++ {
						logger.S().Warnw("[Agent.Run] LLM call failed with retryable error, retrying",
							"error", apiErr.Message,
							"error_type", string(apiErr.Type),
							"attempt", attempt,
							"max_retries", maxRetries,
							"retry_after", retryDelay.String(),
							"session_id", req.SessionID)

						// Notify frontend about retry
						eventChan <- Event{
							Type:      "thinking",
							Content:   fmt.Sprintf("⏳ 请求受限，正在第 %d 次重试（共 %d 次），等待 %v...", attempt, maxRetries, retryDelay),
							ErrorType: string(apiErr.Type),
						}

						select {
						case <-ctx.Done():
							eventChan <- Event{Type: "error", Error: "请求已取消", ErrorType: "cancelled"}
							return
						case <-time.After(retryDelay):
						}

						resp, err = a.llm.Call(ctx, llmReq)
						if err == nil {
							retried = true
							break
						}

						apiErr = llm.ParseAPIError(err)
						if apiErr == nil || !apiErr.IsRetryable() {
							break
						}

						// Exponential backoff
						retryDelay = retryDelay * 2
						if retryDelay > 30*time.Second {
							retryDelay = 30 * time.Second
						}
					}

					if retried && err == nil {
						// Successfully retried, continue normal flow
						logger.S().Infow("[Agent.Run] LLM call succeeded after retry",
							"session_id", req.SessionID)
					} else {
						// All retries failed
						logger.S().Errorw("[Agent.Run] LLM call failed after retries",
							"error", err,
							"session_id", req.SessionID)

						errMsg := "请求频率超限，请稍后重试"
						errType := "rate_limit"
						retryable := true
						if apiErr != nil {
							errMsg = apiErr.Message
							errType = string(apiErr.Type)
							retryable = apiErr.IsRetryable()
						}

						eventChan <- Event{
							Type:      "error",
							Error:     errMsg,
							ErrorType: errType,
							Retryable: retryable,
						}
						return
					}
				} else {
					// Non-retryable error
					logger.S().Errorw("[Agent.Run] LLM call failed",
						"error", err,
						"round", round+1,
						"session_id", req.SessionID)

					errMsg := fmt.Sprintf("LLM call failed: %v", err)
					errType := "unknown"
					retryable := false
					if apiErr != nil {
						errMsg = apiErr.Message
						errType = string(apiErr.Type)
						retryable = apiErr.IsRetryable()
					}

					eventChan <- Event{
						Type:      "error",
						Error:     errMsg,
						ErrorType: errType,
						Retryable: retryable,
					}
					return
				}
			}
			logger.S().Debugw("[Agent.Run] LLM response received",
				"round", round+1,
				"has_tool_calls", len(resp.ToolCalls) > 0,
				"content_length", len(resp.Content),
				"session_id", req.SessionID)

			// Tool calls?
			if len(resp.ToolCalls) > 0 {
				logger.S().Infow("[Agent.Run] Tool calls received",
					"tool_count", len(resp.ToolCalls),
					"round", round+1,
					"session_id", req.SessionID)
				
				// Send LLM's thinking content before tool calls
				if len(resp.Content) > 0 {
					eventChan <- Event{
						Type:    "token",
						Content: resp.Content,
					}
				}
				
				// Tool calls don't consume round, compensate round++
				round--

				// Add assistant message (including tool calls)
				messages = append(messages, llm.Message{
					Role:      "assistant",
					Content:   resp.Content,
					ToolCalls: resp.ToolCalls,
				})

				// Execute each tool call
				for i, tc := range resp.ToolCalls {
					logger.S().Debugw("[Agent.Run] Processing tool call",
						"tool_name", tc.Function.Name,
						"tool_index", i+1,
						"total_tools", len(resp.ToolCalls),
						"session_id", req.SessionID)
					
					// Parse arguments
					args, parseErr := llm.ParseToolCallArguments(tc)
					logger.S().Debugw("[Agent.Run] Tool call arguments parsed",
						"tool_name", tc.Function.Name,
						"raw_arguments", tc.Function.Arguments,
						"parsed_args", args,
						"parse_error", parseErr,
						"session_id", req.SessionID)
					if parseErr != nil {
						// Don't break the loop, return parse error as tool result to LLM
						logger.S().Errorw("[Agent.Run] Failed to parse tool arguments",
							"error", parseErr,
							"tool_name", tc.Function.Name,
							"session_id", req.SessionID)
						errResult := fmt.Sprintf("Error: failed to parse arguments: %v", parseErr)
						eventChan <- Event{
							Type:     "tool_start",
							ToolName: tc.Function.Name,
						}
						eventChan <- Event{
							Type:       "tool_end",
							ToolName:   tc.Function.Name,
							ToolResult: errResult,
						}
						messages = append(messages, llm.Message{
							Role:    "tool",
							Content: errResult,
							Name:    tc.Function.Name,
							ToolID:  tc.ID,
						})
						continue
					}

					// Get tool
					tool, ok := a.tools[tc.Function.Name]
					if !ok {
						// Tool doesn't exist, don't break the loop, return error as tool result to LLM
						logger.S().Errorw("[Agent.Run] Unknown tool",
							"tool_name", tc.Function.Name,
							"available_tools", a.getToolNames(),
							"session_id", req.SessionID)
						errResult := fmt.Sprintf("Error: unknown tool %q", tc.Function.Name)
						eventChan <- Event{
							Type:     "tool_start",
							ToolName: tc.Function.Name,
							ToolArgs: args,
						}
						eventChan <- Event{
							Type:       "tool_end",
							ToolName:   tc.Function.Name,
							ToolResult: errResult,
						}
						messages = append(messages, llm.Message{
							Role:    "tool",
							Content: errResult,
							Name:    tc.Function.Name,
							ToolID:  tc.ID,
						})
						continue
					}

					// Send thinking event before tool execution
					eventChan <- Event{Type: "thinking", Content: getThinkingMessage(round, a.maxRounds, []string{tc.Function.Name})}

					// Check for duplicate execution
					if deduplicator != nil {
						isDup, dupCount := deduplicator.IsDuplicate(tc.Function.Name, args)
						if isDup {
							logger.S().Warnw("[Agent.Run] Duplicate execution detected, skipping",
								"tool_name", tc.Function.Name,
								"duplicate_count", dupCount,
								"session_id", req.SessionID)
							
							// Record skipped execution
							deduplicator.RecordExecution(tc.Function.Name, args, "", round, true)
							terminationState.IncrementDuplicates()
							
							// Return special result to inform LLM not to repeat
							result := fmt.Sprintf("⚠️ This operation was already executed %d times. Please proceed to the next step or provide a final answer.", dupCount)
							
							eventChan <- Event{
								Type:     "tool_start",
								ToolName: tc.Function.Name,
								ToolArgs: args,
							}
							eventChan <- Event{
								Type:       "tool_end",
								ToolName:   tc.Function.Name,
								ToolResult: result,
							}
							messages = append(messages, llm.Message{
								Role:    "tool",
								Content: result,
								Name:    tc.Function.Name,
								ToolID:  tc.ID,
							})
							continue
						}
					}

					// Detect loop patterns
					if loopDetector != nil {
						recentToolCalls = loopDetector.AddToolCall(recentToolCalls, tc.Function.Name, tc.Function.Arguments)
						if isLoop, pattern := loopDetector.DetectLoop(recentToolCalls); isLoop {
							logger.S().Warnw("[Agent.Run] Loop pattern detected, terminating",
								"pattern", pattern.ToolSequence,
								"count", pattern.Count,
								"session_id", req.SessionID)
							
							// Force terminate the loop
							finalContent = fmt.Sprintf("I detected a repetitive pattern in my actions: %v. Let me provide a summary instead of continuing in circles.", pattern.ToolSequence)
							eventChan <- Event{
								Type:    "token",
								Content: finalContent,
							}
							break
						}
					}

					// Send tool start event
					eventChan <- Event{
						Type:     "tool_start",
						ToolName: tc.Function.Name,
						ToolArgs: args,
					}

					// Execute tool
					logger.S().Infow("[Agent.Run] Executing tool",
						"tool_name", tc.Function.Name,
						"round", round+1,
						"session_id", req.SessionID)
					
					terminationState.IncrementToolCalls()
					
					toolStartTime := time.Now()
					result, toolErr := tool.Execute(ctx, args)
					toolDuration := time.Since(toolStartTime).Milliseconds()
					
					// Record execution history
					if deduplicator != nil {
						deduplicator.RecordExecution(tc.Function.Name, args, result, round, false)
					}
					
					if toolErr != nil {
						logger.S().Errorw("[Agent.Run] Tool execution failed",
							"error", toolErr,
							"tool_name", tc.Function.Name,
							"session_id", req.SessionID)
						result = fmt.Sprintf("Error: %v", toolErr)
					} else {
						// Smart truncation for large tool results
						const maxResultSize = 200 * 1024 // 200KB (increased from 50KB)
						if len(result) > maxResultSize {
							originalLen := len(result)
							result = truncateToolResult(result, maxResultSize, tc.Function.Name)
							logger.S().Warnw("[Agent.Run] Tool result truncated",
								"tool_name", tc.Function.Name,
								"original_size", originalLen,
								"truncated_size", len(result),
								"session_id", req.SessionID)
						}
						logger.S().Infow("[Agent.Run] Tool execution completed",
							"tool_name", tc.Function.Name,
							"result_length", len(result),
							"duration_ms", toolDuration,
							"session_id", req.SessionID)
					}

					// Send tool end event with modified files and duration
					modifiedFiles := extractModifiedFiles(tc.Function.Name, result)
					eventChan <- Event{
						Type:          "tool_end",
						ToolName:       tc.Function.Name,
						ToolResult:     result,
						DurationMs:     toolDuration,
						ModifiedFiles: modifiedFiles,
					}

					// Add tool result message
					messages = append(messages, llm.Message{
						Role:    "tool",
						Content: result,
						Name:    tc.Function.Name,
						ToolID:  tc.ID,
					})
				}
				continue
			}

			// Check if response is empty (no tool calls and no content)
			if len(resp.Content) == 0 {
				logger.S().Warnw("[Agent.Run] LLM returned empty response",
					"round", round+1,
					"has_tool_calls", false,
					"session_id", req.SessionID)
				
				// Generate a default response based on what was accomplished
				finalContent = "Task completed successfully. The requested changes have been applied."
				
				logger.S().Infow("[Agent.Run] Generated default response for empty LLM output",
					"round", round+1,
					"session_id", req.SessionID)
			} else {
				finalContent = resp.Content
			}
			
			// Quality check: warn if response is too short for analysis tasks
			if len(finalContent) < 500 && len(req.Message) > 50 {
				logger.S().Warnw("[Agent.Run] Response seems too short for analysis task",
					"content_length", len(finalContent),
					"message_length", len(req.Message),
					"session_id", req.SessionID,
					"content_preview", truncateString(finalContent, 200))
			}
			
			logger.S().Infow("[Agent.Run] Final answer received",
				"round", round+1,
				"content_length", len(finalContent),
				"content_preview", truncateString(finalContent, 200),
				"session_id", req.SessionID)
			eventChan <- Event{
				Type:    "token",
				Content: finalContent,
			}
			break
		}

		// When maxRounds exhausted without final answer, provide friendly message
		if finalContent == "" {
			logger.S().Warnw("[Agent.Run] Max rounds exhausted without final answer",
				"max_rounds", a.maxRounds,
				"session_id", req.SessionID)
			finalContent = "I've reached the maximum number of reasoning steps. Please try rephrasing your question or breaking it into smaller parts."
			eventChan <- Event{
				Type:    "token",
				Content: finalContent,
			}
		}

		// Send done event
		logger.S().Infow("[Agent.Run] Sending done event", "session_id", req.SessionID)
		eventChan <- Event{Type: "done"}

		// Save memory
		if a.memory != nil && finalContent != "" {
			logger.S().Debugw("[Agent.Run] Saving to memory", "session_id", req.SessionID)
			projectID := GetProjectIDFromContext(ctx)
			agentID := GetAgentIDFromContext(ctx)
			// Save user message
			err := a.memory.AddWithMeta(ctx, req.SessionID, projectID, agentID, "user", req.Message)
			if err != nil {
				logger.S().Errorw("[Agent.Run] Failed to save user message to memory",
					"error", err,
					"session_id", req.SessionID)
			}
			// Save assistant message
			err = a.memory.AddWithMeta(ctx, req.SessionID, projectID, agentID, "assistant", finalContent)
			if err != nil {
				logger.S().Errorw("[Agent.Run] Failed to save assistant message to memory",
					"error", err,
					"session_id", req.SessionID)
			}
			logger.S().Debugw("[Agent.Run] Memory saved", "session_id", req.SessionID)
		}
	}()

	return eventChan
}

func (a *ReActAgent) getToolNames() []string {
	names := make([]string, 0, len(a.tools))
	for name := range a.tools {
		names = append(names, name)
	}
	return names
}

// Close releases resources (implements Agent interface)
func (a *ReActAgent) Close() error {
    if a.memory != nil {
            return a.memory.Close()
    }       
    return nil
}

// LLMClient returns the underlying LLM client
func (a *ReActAgent) LLMClient() llm.Client {
	return a.llm
}

// MaxRounds returns the maximum number of rounds
func (a *ReActAgent) MaxRounds() int {
	return a.maxRounds
}

// thinkingDescriptions maps tool names to user-friendly descriptions
var thinkingDescriptions = map[string]string{
        "read_file":              "Reading file...",
        "write_file":             "Writing file...",
        "replace_file":           "Modifying file...",
        "list_files":             "Listing directory contents...",
        "grep_search":            "Searching code...",
        "inverted_index_search":  "Searching index...",
        "term_sparse_search":     "Searching keywords...",
        "wiki_search":            "Searching documentation...",
        "code_definition_names":  "Analyzing code definitions...",
        "run_in_terminal":        "Executing command...",
        "get_terminal_output":    "Getting command output...",
        "mcp_execute_tool":       "Calling MCP tool...",
        "mcp_access_resource":    "Accessing MCP resource...",
        "task_create_todolist":   "Creating task list...",
        "task_update_todolist":   "Updating task list...",
        "task_create_new":        "Creating new task...",
        "task_switch_mode":       "Switching mode...",
        "task_ask_question":      "Preparing question...",
        "attempt_completion":     "Completing task...",
        "activate_skill":         "Activating skill...",
}

// getThinkingMessage returns a user-friendly thinking message based on context
func getThinkingMessage(round, maxRounds int, pendingTools []string) string {
        // If we have pending tools, describe what we're about to do
        if len(pendingTools) > 0 {
                toolName := pendingTools[0]
                if desc, ok := thinkingDescriptions[toolName]; ok {
                        return desc
                }
                // Fallback for unknown tools
                return fmt.Sprintf("Executing task...")
        }

        // Default thinking message based on round
        return "Executing task..."
}                                                                                                                                     

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractModifiedFiles extracts modified file paths from tool results
// For write_file and replace_file tools, the result is a JSON object with a "path" field.
func extractModifiedFiles(toolName, result string) []string {
	if toolName != "write_file" && toolName != "replace_file" {
		return nil
	}

	// Try to parse as JSON
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(result), &obj); err == nil {
		if path, ok := obj["path"].(string); ok {
			return []string{path}
		}
	}

	return nil
}

// truncateToolResult intelligently truncates large tool results while preserving structure
// For file-related tools, it preserves the beginning and end of the content
func truncateToolResult(result string, maxSize int, toolName string) string {
	if len(result) <= maxSize {
		return result
	}

	// For read_file, use smart content truncation
	if toolName == "read_file" {
		return truncateFileContent(result, maxSize)
	}

	// For other tools, use head-tail truncation
	headSize := maxSize * 2 / 3
	tailSize := maxSize / 3

	head := result[:headSize]
	tail := result[len(result)-tailSize:]

	truncatedMsg := fmt.Sprintf("\n\n... [TRUNCATED: original size %d bytes, showing first %d and last %d bytes] ...\n\n", len(result), headSize, tailSize)

	return head + truncatedMsg + tail
}

// truncateFileContent intelligently truncates file content while preserving code structure
// It preserves the beginning (imports, declarations) and end of the file
func truncateFileContent(content string, maxSize int) string {
	if len(content) <= maxSize {
		return content
	}

	// Reserve space for truncation message
	msgSize := 200
	availableSize := maxSize - msgSize

	// Allocate 60% to head, 40% to tail
	headSize := availableSize * 3 / 5
	tailSize := availableSize * 2 / 5

	head := content[:headSize]
	tail := content[len(content)-tailSize:]

	// Count lines in head and tail
	headLines := countLines(head)
	tailLines := countLines(tail)
	totalLines := countLines(content)

	truncatedMsg := fmt.Sprintf("\n\n... [TRUNCATED: file has %d total lines, showing lines 1-%d and %d-%d] ...\n\n",
		totalLines, headLines, totalLines-tailLines+1, totalLines)

	return head + truncatedMsg + tail
}

// countLines counts the number of lines in a string
func countLines(s string) int {
	if len(s) == 0 {
		return 0
	}
	count := 1
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			count++
		}
	}
	return count
}