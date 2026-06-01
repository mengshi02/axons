package agent

import (
	"fmt"
	"time"
)

// TerminationReason represents termination reason
type TerminationReason string

const (
	TerminationNone               TerminationReason = ""
	TerminationMaxRounds          TerminationReason = "max_rounds_reached"
	TerminationMaxToolCalls       TerminationReason = "max_tool_calls_exceeded"
	TerminationMaxDuplicates      TerminationReason = "excessive_duplicates"
	TerminationTimeout            TerminationReason = "timeout"
	TerminationLoopDetected       TerminationReason = "loop_detected"
	TerminationContextTooLarge    TerminationReason = "context_too_large"
)

// TerminationCondition represents termination condition configuration
type TerminationCondition struct {
	MaxRounds           int           // Maximum rounds
	MaxToolCalls        int           // Maximum tool call count
	MaxDuplicates       int           // Maximum duplicate operation count
	Timeout             time.Duration // Timeout duration
	MaxContextSize      int           // Maximum context size (message count)
}

// NewDefaultTerminationCondition creates default termination condition
func NewDefaultTerminationCondition() *TerminationCondition {
	return &TerminationCondition{
		MaxRounds:        300,
		MaxToolCalls:     150,
		MaxDuplicates:    5,
		Timeout:          30 * time.Minute,
		MaxContextSize:   200,
	}
}

// TerminationState tracks termination state
type TerminationState struct {
	TotalToolCalls  int
	DuplicateCount  int
	StartTime       time.Time
	LastCheckTime   time.Time
}

// NewTerminationState creates termination state
func NewTerminationState() *TerminationState {
	return &TerminationState{
		StartTime: time.Now(),
	}
}

// ShouldTerminate checks if should terminate
// Returns: whether to terminate, termination reason
func (tc *TerminationCondition) ShouldTerminate(
	round int,
	state *TerminationState,
	messageCount int,
) (bool, TerminationReason) {
	// Check round limit
	if round >= tc.MaxRounds {
		return true, TerminationMaxRounds
	}

	// Check total tool calls
	if state.TotalToolCalls > tc.MaxToolCalls {
		return true, TerminationMaxToolCalls
	}

	// Check duplicate operation count
	if state.DuplicateCount > tc.MaxDuplicates {
		return true, TerminationMaxDuplicates
	}

	// Check timeout
	if time.Since(state.StartTime) > tc.Timeout {
		return true, TerminationTimeout
	}

	// Check context size
	if messageCount > tc.MaxContextSize {
		return true, TerminationContextTooLarge
	}

	return false, TerminationNone
}

// IncrementToolCalls increments tool call count
func (s *TerminationState) IncrementToolCalls() {
	s.TotalToolCalls++
}

// IncrementDuplicates increments duplicate operation count
func (s *TerminationState) IncrementDuplicates() {
	s.DuplicateCount++
}

// GetElapsedTime gets elapsed time
func (s *TerminationState) GetElapsedTime() time.Duration {
	return time.Since(s.StartTime)
}

// GetProgressMessage gets progress message
func (tc *TerminationCondition) GetProgressMessage(round int, state *TerminationState) string {
	return fmt.Sprintf(
		"Round %d/%d, Tool calls %d/%d, Duplicates %d/%d, Elapsed %v",
		round, tc.MaxRounds,
		state.TotalToolCalls, tc.MaxToolCalls,
		state.DuplicateCount, tc.MaxDuplicates,
		state.GetElapsedTime().Round(time.Second),
	)
}