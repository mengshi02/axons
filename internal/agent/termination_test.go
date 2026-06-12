package agent

import (
	"strings"
	"testing"
	"time"
)

func TestShouldTerminate_MaxRounds(t *testing.T) {
	tc := &TerminationCondition{
		MaxRounds:        5,
		MaxToolCalls:     100,
		MaxDuplicates:    10,
		Timeout:          1 * time.Hour,
		MaxContextSize:   200,
	}
	state := NewTerminationState()
	should, reason := tc.ShouldTerminate(5, state, 10)
	if !should {
		t.Error("expected termination at MaxRounds boundary")
	}
	if reason != TerminationMaxRounds {
		t.Errorf("reason = %v, want %v", reason, TerminationMaxRounds)
	}

	// Below max rounds should not terminate
	should, reason = tc.ShouldTerminate(4, state, 10)
	if should {
		t.Error("should not terminate below MaxRounds")
	}
	if reason != TerminationNone {
		t.Errorf("reason = %v, want %v", reason, TerminationNone)
	}
}

func TestShouldTerminate_MaxToolCalls(t *testing.T) {
	tc := &TerminationCondition{
		MaxRounds:        300,
		MaxToolCalls:     10,
		MaxDuplicates:    10,
		Timeout:          1 * time.Hour,
		MaxContextSize:   200,
	}
	state := NewTerminationState()
	state.TotalToolCalls = 11
	should, reason := tc.ShouldTerminate(1, state, 10)
	if !should {
		t.Error("expected termination when TotalToolCalls > MaxToolCalls")
	}
	if reason != TerminationMaxToolCalls {
		t.Errorf("reason = %v, want %v", reason, TerminationMaxToolCalls)
	}

	// At exactly MaxToolCalls should not terminate (> not >=)
	state.TotalToolCalls = 10
	should, _ = tc.ShouldTerminate(1, state, 10)
	if should {
		t.Error("should not terminate when TotalToolCalls == MaxToolCalls")
	}
}

func TestShouldTerminate_MaxDuplicates(t *testing.T) {
	tc := &TerminationCondition{
		MaxRounds:        300,
		MaxToolCalls:     150,
		MaxDuplicates:    5,
		Timeout:          1 * time.Hour,
		MaxContextSize:   200,
	}
	state := NewTerminationState()
	state.DuplicateCount = 6
	should, reason := tc.ShouldTerminate(1, state, 10)
	if !should {
		t.Error("expected termination when DuplicateCount > MaxDuplicates")
	}
	if reason != TerminationMaxDuplicates {
		t.Errorf("reason = %v, want %v", reason, TerminationMaxDuplicates)
	}

	// At exactly MaxDuplicates should not terminate
	state.DuplicateCount = 5
	should, _ = tc.ShouldTerminate(1, state, 10)
	if should {
		t.Error("should not terminate when DuplicateCount == MaxDuplicates")
	}
}

func TestShouldTerminate_Timeout(t *testing.T) {
	tc := &TerminationCondition{
		MaxRounds:        300,
		MaxToolCalls:     150,
		MaxDuplicates:    10,
		Timeout:          1 * time.Nanosecond,
		MaxContextSize:   200,
	}
	state := NewTerminationState()
	// StartTime is set to time.Now() in NewTerminationState, so it's already past
	time.Sleep(1 * time.Millisecond)
	should, reason := tc.ShouldTerminate(1, state, 10)
	if !should {
		t.Error("expected termination after timeout")
	}
	if reason != TerminationTimeout {
		t.Errorf("reason = %v, want %v", reason, TerminationTimeout)
	}
}

func TestShouldTerminate_ContextTooLarge(t *testing.T) {
	tc := &TerminationCondition{
		MaxRounds:        300,
		MaxToolCalls:     150,
		MaxDuplicates:    10,
		Timeout:          1 * time.Hour,
		MaxContextSize:   50,
	}
	state := NewTerminationState()
	should, reason := tc.ShouldTerminate(1, state, 51)
	if !should {
		t.Error("expected termination when messageCount > MaxContextSize")
	}
	if reason != TerminationContextTooLarge {
		t.Errorf("reason = %v, want %v", reason, TerminationContextTooLarge)
	}

	// At exactly MaxContextSize should not terminate
	should, _ = tc.ShouldTerminate(1, state, 50)
	if should {
		t.Error("should not terminate when messageCount == MaxContextSize")
	}
}

func TestShouldTerminate_NoTermination(t *testing.T) {
	tc := &TerminationCondition{
		MaxRounds:        300,
		MaxToolCalls:     150,
		MaxDuplicates:    10,
		Timeout:          1 * time.Hour,
		MaxContextSize:   200,
	}
	state := NewTerminationState()
	should, reason := tc.ShouldTerminate(1, state, 10)
	if should {
		t.Error("should not terminate under normal conditions")
	}
	if reason != TerminationNone {
		t.Errorf("reason = %v, want %v", reason, TerminationNone)
	}
}

func TestShouldTerminate_PriorityOrder(t *testing.T) {
	// MaxRounds is checked first, should win over other conditions
	tc := &TerminationCondition{
		MaxRounds:        5,
		MaxToolCalls:     10,
		MaxDuplicates:    5,
		Timeout:          1 * time.Hour,
		MaxContextSize:   50,
	}
	state := NewTerminationState()
	state.TotalToolCalls = 20  // exceeds MaxToolCalls
	state.DuplicateCount = 10  // exceeds MaxDuplicates
	should, reason := tc.ShouldTerminate(5, state, 100) // also exceeds MaxRounds
	if !should {
		t.Error("expected termination")
	}
	if reason != TerminationMaxRounds {
		t.Errorf("reason = %v, want %v (MaxRounds should have priority)", reason, TerminationMaxRounds)
	}
}

func TestNewDefaultTerminationCondition(t *testing.T) {
	tc := NewDefaultTerminationCondition()
	if tc.MaxRounds != 300 {
		t.Errorf("MaxRounds = %d, want 300", tc.MaxRounds)
	}
	if tc.MaxToolCalls != 150 {
		t.Errorf("MaxToolCalls = %d, want 150", tc.MaxToolCalls)
	}
	if tc.MaxDuplicates != 5 {
		t.Errorf("MaxDuplicates = %d, want 5", tc.MaxDuplicates)
	}
	if tc.Timeout != 30*time.Minute {
		t.Errorf("Timeout = %v, want 30m", tc.Timeout)
	}
	if tc.MaxContextSize != 200 {
		t.Errorf("MaxContextSize = %d, want 200", tc.MaxContextSize)
	}
}

func TestTerminationState_IncrementMethods(t *testing.T) {
	state := NewTerminationState()
	if state.TotalToolCalls != 0 {
		t.Errorf("initial TotalToolCalls = %d, want 0", state.TotalToolCalls)
	}
	if state.DuplicateCount != 0 {
		t.Errorf("initial DuplicateCount = %d, want 0", state.DuplicateCount)
	}

	state.IncrementToolCalls()
	if state.TotalToolCalls != 1 {
		t.Errorf("after increment, TotalToolCalls = %d, want 1", state.TotalToolCalls)
	}

	state.IncrementDuplicates()
	if state.DuplicateCount != 1 {
		t.Errorf("after increment, DuplicateCount = %d, want 1", state.DuplicateCount)
	}

	state.IncrementToolCalls()
	state.IncrementToolCalls()
	if state.TotalToolCalls != 3 {
		t.Errorf("after 3 total increments, TotalToolCalls = %d, want 3", state.TotalToolCalls)
	}
}

func TestGetProgressMessage(t *testing.T) {
	tc := &TerminationCondition{
		MaxRounds:      100,
		MaxToolCalls:   50,
		MaxDuplicates:  10,
	}
	state := NewTerminationState()
	state.TotalToolCalls = 5
	state.DuplicateCount = 2

	msg := tc.GetProgressMessage(10, state)

	if !strings.Contains(msg, "Round 10/100") {
		t.Errorf("progress message should contain 'Round 10/100', got: %s", msg)
	}
	if !strings.Contains(msg, "Tool calls 5/50") {
		t.Errorf("progress message should contain 'Tool calls 5/50', got: %s", msg)
	}
	if !strings.Contains(msg, "Duplicates 2/10") {
		t.Errorf("progress message should contain 'Duplicates 2/10', got: %s", msg)
	}
}

func TestGetElapsedTime(t *testing.T) {
	state := NewTerminationState()
	elapsed := state.GetElapsedTime()
	if elapsed < 0 {
		t.Errorf("elapsed time should be non-negative, got %v", elapsed)
	}
	// Should be very small since we just created it
	if elapsed > 5*time.Second {
		t.Errorf("elapsed time should be small for freshly created state, got %v", elapsed)
	}
}