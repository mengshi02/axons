package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ToolCallSignature represents a unique signature for a tool call
type ToolCallSignature struct {
	ToolName string // Tool name
	ArgHash  string // Hash of normalized arguments
}

// LoopPattern defines a loop pattern
type LoopPattern struct {
	ToolSequence []ToolCallSignature // Tool call sequence with signatures
	Count        int                 // Repetition count
	FirstSeen    time.Time
	LastSeen     time.Time
}

// LoopDetector detects loop patterns
type LoopDetector struct {
	patterns   map[string]*LoopPattern // key: hash of sequence
	windowSize int                     // Detection window size
	threshold  int                     // Repetition threshold
}

// NewLoopDetector creates a loop detector
func NewLoopDetector(windowSize, threshold int) *LoopDetector {
	if windowSize <= 0 {
		windowSize = 5
	}
	if threshold <= 0 {
		threshold = 2
	}
	return &LoopDetector{
		patterns:   make(map[string]*LoopPattern),
		windowSize: windowSize,
		threshold:  threshold,
	}
}

// hashArguments calculates a hash of tool call arguments
// It normalizes the JSON to ensure consistent hashing regardless of key order
func hashArguments(args string) string {
	if args == "" {
		return ""
	}

	// Parse and normalize JSON to handle key order variations
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		// If parsing fails, use original string (fallback)
		return fmt.Sprintf("%x", sha256.Sum256([]byte(args)))[:16]
	}

	// Re-marshal to get normalized JSON with consistent key ordering
	normalized, err := json.Marshal(parsed)
	if err != nil {
		return fmt.Sprintf("%x", sha256.Sum256([]byte(args)))[:16]
	}

	// Return first 16 characters of SHA256 hash
	return fmt.Sprintf("%x", sha256.Sum256(normalized))[:16]
}

// DetectLoop detects loop patterns
// Returns: whether loop detected, loop pattern details
func (ld *LoopDetector) DetectLoop(recentTools []ToolCallSignature) (bool, *LoopPattern) {
	if len(recentTools) < ld.windowSize {
		return false, nil
	}

	// Take recent window
	window := recentTools[len(recentTools)-ld.windowSize:]
	windowHash := hashToolCallSequence(window)

	if pattern, exists := ld.patterns[windowHash]; exists {
		pattern.Count++
		pattern.LastSeen = time.Now()

		// Return detected loop if threshold reached
		if pattern.Count >= ld.threshold {
			return true, pattern
		}
	} else {
		// New pattern
		ld.patterns[windowHash] = &LoopPattern{
			ToolSequence: window,
			Count:        1,
			FirstSeen:    time.Now(),
			LastSeen:     time.Now(),
		}
	}

	return false, nil
}

// AddToolCall adds a tool call record with arguments
// This is a helper method for maintaining tool call list
func (ld *LoopDetector) AddToolCall(recentTools []ToolCallSignature, toolName, args string) []ToolCallSignature {
	signature := ToolCallSignature{
		ToolName: toolName,
		ArgHash:  hashArguments(args),
	}
	updated := append(recentTools, signature)
	// Keep recent 2 * windowSize calls for detection
	maxSize := ld.windowSize * 2
	if len(updated) > maxSize {
		updated = updated[len(updated)-maxSize:]
	}
	return updated
}

// Reset resets detector state
func (ld *LoopDetector) Reset() {
	ld.patterns = make(map[string]*LoopPattern)
}

// GetPatterns gets all detected patterns
func (ld *LoopDetector) GetPatterns() map[string]*LoopPattern {
	return ld.patterns
}

// hashToolCallSequence calculates hash of tool call signature sequence
func hashToolCallSequence(signatures []ToolCallSignature) string {
	var parts []string
	for _, sig := range signatures {
		parts = append(parts, fmt.Sprintf("%s:%s", sig.ToolName, sig.ArgHash))
	}
	return strings.Join(parts, "|")
}