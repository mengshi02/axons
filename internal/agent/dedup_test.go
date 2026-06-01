package agent

import (
	"testing"
)

func TestExecutionDeduplicator_IsDuplicate(t *testing.T) {
	dedup := NewExecutionDeduplicator(50, 3)

	args := map[string]any{
		"path":    "/test/file.go",
		"content": "test content",
	}

	// 第一次执行,不应该重复
	isDup, count := dedup.IsDuplicate("write_file", args)
	if isDup {
		t.Errorf("First execution should not be duplicate, got isDup=%v, count=%d", isDup, count)
	}
	if count != 0 {
		t.Errorf("First execution count should be 0, got %d", count)
	}

	// 记录第一次执行
	dedup.RecordExecution("write_file", args, "result1", 0, false)

	// 第二次执行相同操作
	isDup, count = dedup.IsDuplicate("write_file", args)
	if isDup {
		t.Errorf("Second execution should not be duplicate yet, got isDup=%v, count=%d", isDup, count)
	}
	if count != 1 {
		t.Errorf("Second execution count should be 1, got %d", count)
	}

	// 记录第二次执行
	dedup.RecordExecution("write_file", args, "result2", 0, false)

	// 第三次执行相同操作
	isDup, count = dedup.IsDuplicate("write_file", args)
	if isDup {
		t.Errorf("Third execution should not be duplicate yet, got isDup=%v, count=%d", isDup, count)
	}
	if count != 2 {
		t.Errorf("Third execution count should be 2, got %d", count)
	}

	// 记录第三次执行
	dedup.RecordExecution("write_file", args, "result3", 0, false)

	// 第四次执行,应该被检测为重复
	isDup, count = dedup.IsDuplicate("write_file", args)
	if !isDup {
		t.Errorf("Fourth execution should be duplicate, got isDup=%v, count=%d", isDup, count)
	}
	if count != 3 {
		t.Errorf("Fourth execution count should be 3, got %d", count)
	}
}

func TestExecutionDeduplicator_DifferentArgs(t *testing.T) {
	dedup := NewExecutionDeduplicator(50, 3)

	args1 := map[string]any{
		"path":    "/test/file1.go",
		"content": "content1",
	}
	args2 := map[string]any{
		"path":    "/test/file2.go",
		"content": "content2",
	}

	// 记录多次相同操作
	dedup.RecordExecution("write_file", args1, "result1", 0, false)
	dedup.RecordExecution("write_file", args1, "result2", 0, false)
	dedup.RecordExecution("write_file", args1, "result3", 0, false)

	// args1 应该被检测为重复
	isDup, _ := dedup.IsDuplicate("write_file", args1)
	if !isDup {
		t.Errorf("args1 should be duplicate after 3 executions")
	}

	// args2 不应该被检测为重复(不同的参数)
	isDup, _ = dedup.IsDuplicate("write_file", args2)
	if isDup {
		t.Errorf("args2 should not be duplicate (different args)")
	}
}

func TestLoopDetector_DetectLoop(t *testing.T) {
	detector := NewLoopDetector(5, 2)

	var tools []ToolCallSignature

	// Add initial tool calls with empty arguments
	tools = detector.AddToolCall(tools, "read_file", "")
	tools = detector.AddToolCall(tools, "write_file", "")
	tools = detector.AddToolCall(tools, "build", "")
	tools = detector.AddToolCall(tools, "test", "")

	// 第一次序列,不应该检测到循环
	isLoop, pattern := detector.DetectLoop(tools)
	if isLoop {
		t.Errorf("First sequence should not be loop, got isLoop=%v", isLoop)
	}

	// 添加更多工具调用
	tools = detector.AddToolCall(tools, "read_file", "")
	tools = detector.AddToolCall(tools, "write_file", "")
	tools = detector.AddToolCall(tools, "build", "")
	tools = detector.AddToolCall(tools, "test", "")

	// 第二次相同序列
	isLoop, pattern = detector.DetectLoop(tools)
	if isLoop {
		t.Errorf("Second sequence should not be loop yet, got isLoop=%v", isLoop)
	}

	// 第三次相同序列,应该检测到循环
	isLoop, pattern = detector.DetectLoop(tools)
	if !isLoop {
		t.Errorf("Third sequence should be loop, got isLoop=%v", isLoop)
	}
	if pattern == nil {
		t.Errorf("Pattern should not be nil when loop detected")
	} else if pattern.Count < 2 {
		t.Errorf("Pattern count should be >= 2, got %d", pattern.Count)
	}
}

func TestTerminationCondition(t *testing.T) {
	cond := NewDefaultTerminationCondition()
	state := NewTerminationState()

	// 初始状态不应该终止
	shouldTerm, reason := cond.ShouldTerminate(0, state, 10)
	if shouldTerm {
		t.Errorf("Initial state should not terminate, reason=%s", reason)
	}

	// 达到最大工具调用数
	for i := 0; i < cond.MaxToolCalls+1; i++ {
		state.IncrementToolCalls()
	}
	shouldTerm, reason = cond.ShouldTerminate(0, state, 10)
	if !shouldTerm {
		t.Errorf("Should terminate when max tool calls exceeded")
	}
	if reason != TerminationMaxToolCalls {
		t.Errorf("Reason should be max_tool_calls_exceeded, got %s", reason)
	}

	// 测试重复操作
	state2 := NewTerminationState()
	for i := 0; i < 6; i++ {
		state2.IncrementDuplicates()
	}
	shouldTerm, reason = cond.ShouldTerminate(0, state2, 10)
	if !shouldTerm {
		t.Errorf("Should terminate when max duplicates exceeded")
	}
	if reason != TerminationMaxDuplicates {
		t.Errorf("Reason should be excessive_duplicates, got %s", reason)
	}
}