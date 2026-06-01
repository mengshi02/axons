package agent

import (
	"encoding/json"
	"testing"
)

func TestHashArguments(t *testing.T) {
	tests := []struct {
		name     string
		args1    string
		args2    string
		wantSame bool
	}{
		{
			name:     "Empty arguments",
			args1:    "",
			args2:    "",
			wantSame: true,
		},
		{
			name:     "Identical arguments",
			args1:    `{"command": "go build"}`,
			args2:    `{"command": "go build"}`,
			wantSame: true,
		},
		{
			name:     "Same arguments different key order",
			args1:    `{"command": "go build", "timeout": 30}`,
			args2:    `{"timeout": 30, "command": "go build"}`,
			wantSame: true,
		},
		{
			name:     "Different arguments",
			args1:    `{"command": "go build"}`,
			args2:    `{"command": "go test"}`,
			wantSame: false,
		},
		{
			name:     "Different timeout values",
			args1:    `{"command": "go build", "timeout": 30}`,
			args2:    `{"command": "go build", "timeout": 60}`,
			wantSame: false,
		},
		{
			name:     "Invalid JSON fallback",
			args1:    `{invalid json}`,
			args2:    `{invalid json}`,
			wantSame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := hashArguments(tt.args1)
			hash2 := hashArguments(tt.args2)

			if tt.wantSame && hash1 != hash2 {
				t.Errorf("hashArguments() expected same hash, got %q != %q", hash1, hash2)
			}
			if !tt.wantSame && hash1 == hash2 {
				t.Errorf("hashArguments() expected different hashes, got same hash %q", hash1)
			}
		})
	}
}

func TestHashArguments_Normalization(t *testing.T) {
	// Test that JSON normalization works correctly
	args := []string{
		`{"a":1,"b":2,"c":3}`,
		`{"c":3,"b":2,"a":1}`,
		`{"b":2,"a":1,"c":3}`,
	}

	hashes := make(map[string]bool)
	for _, arg := range args {
		hash := hashArguments(arg)
		hashes[hash] = true
	}

	if len(hashes) != 1 {
		t.Errorf("Expected all key order variations to produce same hash, got %d different hashes", len(hashes))
	}
}

func TestToolCallSignature(t *testing.T) {
	sig1 := ToolCallSignature{
		ToolName: "run_command",
		ArgHash:  "abc123",
	}
	sig2 := ToolCallSignature{
		ToolName: "run_command",
		ArgHash:  "abc123",
	}
	sig3 := ToolCallSignature{
		ToolName: "run_command",
		ArgHash:  "def456",
	}

	if sig1 != sig2 {
		t.Errorf("Identical signatures should be equal")
	}
	if sig1 == sig3 {
		t.Errorf("Signatures with different ArgHash should not be equal")
	}
}

func TestLoopDetector_WithArguments(t *testing.T) {
	detector := NewLoopDetector(5, 2)

	// Simulate tool calls with different arguments
	args1 := `{"command": "go build"}`
	args2 := `{"command": "go test"}`

	var recentCalls []ToolCallSignature

	// Add 5 tool calls to fill the window
	recentCalls = detector.AddToolCall(recentCalls, "run_command", args1)
	recentCalls = detector.AddToolCall(recentCalls, "run_command", args2)
	recentCalls = detector.AddToolCall(recentCalls, "run_command", args1)
	recentCalls = detector.AddToolCall(recentCalls, "run_command", args2)
	recentCalls = detector.AddToolCall(recentCalls, "run_command", args1)

	// First detection - should not trigger
	isLoop, _ := detector.DetectLoop(recentCalls)
	if isLoop {
		t.Errorf("First detection should not trigger loop")
	}

	// Add same sequence again
	recentCalls = detector.AddToolCall(recentCalls, "run_command", args2)
	recentCalls = detector.AddToolCall(recentCalls, "run_command", args1)

	// Second detection - should trigger (same sequence repeated)
	isLoop, pattern := detector.DetectLoop(recentCalls)
	if !isLoop {
		t.Errorf("Second detection should trigger loop for repeated sequence")
	}
	if pattern == nil {
		t.Errorf("Pattern should not be nil when loop detected")
	}
}

func TestLoopDetector_DifferentArgumentsNoLoop(t *testing.T) {
	detector := NewLoopDetector(5, 2)

	var recentCalls []ToolCallSignature

	// Add 10 tool calls with ALL DIFFERENT arguments
	for i := 0; i < 10; i++ {
		args := json.RawMessage(`{"command": "cmd` + string(rune('0'+i)) + `"}`)
		recentCalls = detector.AddToolCall(recentCalls, "run_command", string(args))
	}

	// Should NOT detect loop because all arguments are different
	isLoop, _ := detector.DetectLoop(recentCalls)
	if isLoop {
		t.Errorf("Should not detect loop when all arguments are different")
	}
}

func TestLoopDetector_SameArgumentsLoop(t *testing.T) {
	detector := NewLoopDetector(5, 2)

	var recentCalls []ToolCallSignature

	// Add tool calls with SAME arguments repeatedly
	args := `{"command": "go build"}`

	// Fill window with same calls
	for i := 0; i < 5; i++ {
		recentCalls = detector.AddToolCall(recentCalls, "run_command", args)
	}

	// First check
	isLoop, _ := detector.DetectLoop(recentCalls)
	if isLoop {
		t.Errorf("First check should not trigger")
	}

	// Add more same calls
	for i := 0; i < 3; i++ {
		recentCalls = detector.AddToolCall(recentCalls, "run_command", args)
	}

	// Should detect loop now (same tool + same args repeated)
	isLoop, pattern := detector.DetectLoop(recentCalls)
	if !isLoop {
		t.Errorf("Should detect loop when same tool+args repeated")
	}
	if pattern != nil && pattern.Count < 2 {
		t.Errorf("Pattern count should be >= 2, got %d", pattern.Count)
	}
}

func TestLoopDetector_MixedToolsAndArgs(t *testing.T) {
	detector := NewLoopDetector(5, 2)

	var recentCalls []ToolCallSignature

	// Mix of different tools and arguments - should NOT trigger loop
	recentCalls = detector.AddToolCall(recentCalls, "read_file", `{"path": "file1.go"}`)
	recentCalls = detector.AddToolCall(recentCalls, "write_file", `{"path": "file2.go"}`)
	recentCalls = detector.AddToolCall(recentCalls, "run_command", `{"command": "go build"}`)
	recentCalls = detector.AddToolCall(recentCalls, "read_file", `{"path": "file3.go"}`)
	recentCalls = detector.AddToolCall(recentCalls, "write_file", `{"path": "file4.go"}`)

	isLoop, _ := detector.DetectLoop(recentCalls)
	if isLoop {
		t.Errorf("Should not detect loop with mixed tools and arguments")
	}
}

func TestHashArguments_Performance(t *testing.T) {
	// Performance test - ensure hashing is fast enough
	args := `{"command": "go build", "timeout": 30, "env": ["GOPATH=/go", "GOROOT=/usr/local/go"], "dir": "/project"}`

	// Run 1000 iterations
	for i := 0; i < 1000; i++ {
		_ = hashArguments(args)
	}

	// This test just ensures no panic and reasonable performance
	// Real benchmark would use testing.B
}

func TestLoopDetector_Reset(t *testing.T) {
	detector := NewLoopDetector(5, 2)

	var recentCalls []ToolCallSignature
	args := `{"command": "go build"}`

	// Add calls and trigger detection
	for i := 0; i < 10; i++ {
		recentCalls = detector.AddToolCall(recentCalls, "run_command", args)
	}

	// DetectLoop must be called to populate patterns
	detector.DetectLoop(recentCalls)

	// Should have patterns stored
	patterns := detector.GetPatterns()
	if len(patterns) == 0 {
		t.Errorf("Should have patterns after tool calls")
	}

	// Reset
	detector.Reset()

	// Should have no patterns after reset
	patterns = detector.GetPatterns()
	if len(patterns) != 0 {
		t.Errorf("Should have no patterns after reset, got %d", len(patterns))
	}
}