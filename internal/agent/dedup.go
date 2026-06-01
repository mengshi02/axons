package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// ToolExecutionRecord records detailed information of tool execution
type ToolExecutionRecord struct {
	ToolName    string
	ArgsHash    string // Hash of arguments
	ResultHash  string // Hash of result
	Timestamp   time.Time
	Round       int
	Skipped     bool // Whether execution was skipped (duplicate)
}

// ExecutionDeduplicator deduplicates tool executions
type ExecutionDeduplicator struct {
	history           []ToolExecutionRecord
	maxHistorySize    int // Maximum history size
	duplicateThreshold int // Duplicate threshold, skip execution when exceeded
}

// NewExecutionDeduplicator creates a deduplicator
func NewExecutionDeduplicator(maxHistorySize, duplicateThreshold int) *ExecutionDeduplicator {
	if maxHistorySize <= 0 {
		maxHistorySize = 50
	}
	if duplicateThreshold <= 0 {
		duplicateThreshold = 3
	}
	return &ExecutionDeduplicator{
		history:           make([]ToolExecutionRecord, 0),
		maxHistorySize:    maxHistorySize,
		duplicateThreshold: duplicateThreshold,
	}
}

// IsDuplicate checks if this is a duplicate execution
// Returns: whether duplicate, duplicate count
func (d *ExecutionDeduplicator) IsDuplicate(toolName string, args map[string]any) (bool, int) {
	argsHash := hashArgs(args)
	
	duplicateCount := 0
	// Search backwards, count identical operations
	for i := len(d.history) - 1; i >= 0; i-- {
		record := d.history[i]
		if record.ToolName == toolName && record.ArgsHash == argsHash {
			duplicateCount++
			// If threshold reached, return true
			if duplicateCount >= d.duplicateThreshold {
				return true, duplicateCount
			}
		}
	}
	
	return false, duplicateCount
}

// RecordExecution records a tool execution
func (d *ExecutionDeduplicator) RecordExecution(toolName string, args map[string]any, result string, round int, skipped bool) {
	argsHash := hashArgs(args)
	resultHash := hashString(result)
	
	record := ToolExecutionRecord{
		ToolName:    toolName,
		ArgsHash:    argsHash,
		ResultHash:  resultHash,
		Timestamp:   time.Now(),
		Round:       round,
		Skipped:     skipped,
	}
	
	d.history = append(d.history, record)
	
	// Limit history size, remove oldest record
	if len(d.history) > d.maxHistorySize {
		d.history = d.history[1:]
	}
}

// GetStats gets execution statistics
func (d *ExecutionDeduplicator) GetStats() map[string]interface{} {
	totalExecutions := len(d.history)
	skippedExecutions := 0
	uniqueTools := make(map[string]int)
	
	for _, record := range d.history {
		if record.Skipped {
			skippedExecutions++
		}
		uniqueTools[record.ToolName]++
	}
	
	return map[string]interface{}{
		"total_executions":   totalExecutions,
		"skipped_executions": skippedExecutions,
		"unique_tools":       len(uniqueTools),
		"tool_distribution":  uniqueTools,
	}
}

// hashArgs calculates hash of arguments
func hashArgs(args map[string]any) string {
	if args == nil {
		return ""
	}
	
	// Serialize arguments to JSON
	data, err := json.Marshal(args)
	if err != nil {
		// If serialization fails, use string representation
		return hashString(fmt.Sprintf("%v", args))
	}
	
	return hashString(string(data))
}

// hashString calculates SHA256 hash of a string
func hashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))[:16] // Only take first 16 characters
}