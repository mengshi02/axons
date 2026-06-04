package agent

import (
	"strings"
	"testing"

	"github.com/mengshi02/axons/internal/agent/llm"
)

// TestCompressMessages_PreservesUserMessage reproduces the production failure
// where compression dropped all user messages and the LLM returned 400
// "No user query found in messages."
func TestCompressMessages_PreservesUserMessage(t *testing.T) {
	cm := NewContextManager(10, 8000, 0.5)

	msgs := []llm.Message{
		{Role: "system", Content: "you are an assistant"},
		{Role: "user", Content: "first user question"},
	}
	// Append a long tail of assistant + tool turns with NO further user messages.
	for i := 0; i < 30; i++ {
		msgs = append(msgs, llm.Message{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{ID: "t1", Type: "function", Function: llm.FunctionCall{Name: "noop"}},
			},
		})
		msgs = append(msgs, llm.Message{
			Role:    "tool",
			Name:    "noop",
			ToolID:  "t1",
			Content: "result",
		})
	}

	out := cm.CompressMessages(msgs)

	if len(out) >= len(msgs) {
		t.Fatalf("expected compression to shrink the slice, got %d -> %d", len(msgs), len(out))
	}
	if !containsUser(out) {
		t.Fatalf("compressed slice has no user message: %+v", roleSeq(out))
	}
}

// TestCompressMessages_NoOrphanToolAtBoundary ensures the kept window does not
// start with a role=tool message that has no parent assistant in scope.
func TestCompressMessages_NoOrphanToolAtBoundary(t *testing.T) {
	cm := NewContextManager(6, 8000, 0.5)

	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "a"}}},
		{Role: "tool", ToolID: "a", Content: "ra"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "b"}}},
		{Role: "tool", ToolID: "b", Content: "rb"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c"}}},
		{Role: "tool", ToolID: "c", Content: "rc"},
		{Role: "assistant", Content: "final"},
	}

	out := cm.CompressMessages(msgs)

	// Find the first non-system, non-summary message in the output.
	for _, m := range out {
		if m.Role == "system" {
			continue
		}
		if m.Role == "tool" {
			t.Fatalf("kept window starts with orphan tool message; sequence=%v", roleSeq(out))
		}
		break
	}
}

// TestCompressMessages_BelowThresholdNoop verifies short conversations are
// returned unchanged.
func TestCompressMessages_BelowThresholdNoop(t *testing.T) {
	cm := NewContextManager(50, 8000, 0.5)
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "a"},
	}
	out := cm.CompressMessages(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("short conversation should not be compressed, got %d -> %d", len(msgs), len(out))
	}
}

// TestCompressMessages_NoUserAtAllPreservesInput covers the safety net: if the
// caller somehow built a conversation with no user messages whatsoever, we
// return the input unchanged rather than emitting an invalid request.
func TestCompressMessages_NoUserAtAllPreservesInput(t *testing.T) {
	cm := NewContextManager(5, 8000, 0.5)
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
	}
	for i := 0; i < 20; i++ {
		msgs = append(msgs, llm.Message{Role: "assistant", Content: "x"})
	}
	out := cm.CompressMessages(msgs)
	// Without any user message we cannot fix the request — must return as-is.
	if len(out) != len(msgs) {
		t.Fatalf("safety net should return input unchanged when no user message exists; got %d -> %d", len(msgs), len(out))
	}
}

// TestCompressMessages_KeepsSystemFirst checks system message is preserved at
// position 0.
func TestCompressMessages_KeepsSystemFirst(t *testing.T) {
	cm := NewContextManager(4, 8000, 0.5)
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: "a3"},
	}
	out := cm.CompressMessages(msgs)
	if len(out) == 0 || out[0].Role != "system" {
		t.Fatalf("system message must be at index 0; got sequence=%v", roleSeq(out))
	}
	if !containsUser(out) {
		t.Fatalf("compressed slice missing user message; got sequence=%v", roleSeq(out))
	}
}

// roleSeq is a tiny helper used by tests for clearer failure output.
func roleSeq(msgs []llm.Message) string {
	var sb strings.Builder
	for i, m := range msgs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(m.Role)
	}
	return sb.String()
}