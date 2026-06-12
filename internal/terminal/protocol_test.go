package terminal

import (
	"encoding/json"
	"testing"
)

func TestEncodeDecodeMessage(t *testing.T) {
	msg := Message{
		Type: MessageTypeInput,
		Data: "hello",
		Cols: 80,
		Rows: 24,
	}

	data, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage failed: %v", err)
	}

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage failed: %v", err)
	}

	if decoded.Type != MessageTypeInput {
		t.Errorf("expected type %s, got %s", MessageTypeInput, decoded.Type)
	}
	if decoded.Data != "hello" {
		t.Errorf("expected data 'hello', got %q", decoded.Data)
	}
	if decoded.Cols != 80 || decoded.Rows != 24 {
		t.Errorf("expected 80x24, got %dx%d", decoded.Cols, decoded.Rows)
	}
}

func TestNewInputMessage(t *testing.T) {
	msg := NewInputMessage("ls -la")
	if msg.Type != MessageTypeInput {
		t.Errorf("expected type input, got %s", msg.Type)
	}
	if msg.Data != "ls -la" {
		t.Errorf("expected data 'ls -la', got %q", msg.Data)
	}
}

func TestNewOutputMessage(t *testing.T) {
	msg := NewOutputMessage("output data")
	if msg.Type != MessageTypeOutput {
		t.Errorf("expected type output, got %s", msg.Type)
	}
	if msg.Data != "output data" {
		t.Errorf("expected data 'output data', got %q", msg.Data)
	}
}

func TestNewResizeMessage(t *testing.T) {
	msg := NewResizeMessage(120, 40)
	if msg.Type != MessageTypeResize {
		t.Errorf("expected type resize, got %s", msg.Type)
	}
	if msg.Cols != 120 || msg.Rows != 40 {
		t.Errorf("expected 120x40, got %dx%d", msg.Cols, msg.Rows)
	}
}

func TestNewExitMessage(t *testing.T) {
	msg := NewExitMessage(0)
	if msg.Type != MessageTypeExit {
		t.Errorf("expected type exit, got %s", msg.Type)
	}
	if msg.Code != 0 {
		t.Errorf("expected code 0, got %d", msg.Code)
	}
}

func TestNewErrorMessage(t *testing.T) {
	msg := NewErrorMessage("something failed")
	if msg.Type != MessageTypeError {
		t.Errorf("expected type error, got %s", msg.Type)
	}
	if msg.Data != "something failed" {
		t.Errorf("expected data 'something failed', got %q", msg.Data)
	}
}

func TestNewResumeMessage(t *testing.T) {
	msg := NewResumeMessage(42)
	if msg.Type != MessageTypeResume {
		t.Errorf("expected type resume, got %s", msg.Type)
	}
	if msg.Seq != 42 {
		t.Errorf("expected seq 42, got %d", msg.Seq)
	}
}

func TestNewReplayMessage(t *testing.T) {
	msg := NewReplayMessage("replay data")
	if msg.Type != MessageTypeReplay {
		t.Errorf("expected type replay, got %s", msg.Type)
	}
	if msg.Data != "replay data" {
		t.Errorf("expected data 'replay data', got %q", msg.Data)
	}
}

func TestNewSyncMessage(t *testing.T) {
	msg := NewSyncMessage(100)
	if msg.Type != MessageTypeSync {
		t.Errorf("expected type sync, got %s", msg.Type)
	}
	if msg.Seq != 100 {
		t.Errorf("expected seq 100, got %d", msg.Seq)
	}
}

func TestNewOrphanReqMessage(t *testing.T) {
	msg := NewOrphanReqMessage()
	if msg.Type != MessageTypeOrphanReq {
		t.Errorf("expected type orphan_req, got %s", msg.Type)
	}
}

func TestNewOrphanAckMessage(t *testing.T) {
	msg := NewOrphanAckMessage()
	if msg.Type != MessageTypeOrphanAck {
		t.Errorf("expected type orphan_ack, got %s", msg.Type)
	}
}

func TestNewDetachMessage(t *testing.T) {
	msg := NewDetachMessage()
	if msg.Type != MessageTypeDetach {
		t.Errorf("expected type detach, got %s", msg.Type)
	}
}

func TestNewSerializeMessage(t *testing.T) {
	msg := NewSerializeMessage("\x1b[0m", 80, 24)
	if msg.Type != MessageTypeSerialize {
		t.Errorf("expected type serialize, got %s", msg.Type)
	}
	if msg.Data != "\x1b[0m" {
		t.Errorf("unexpected data")
	}
	if msg.Cols != 80 || msg.Rows != 24 {
		t.Errorf("expected 80x24, got %dx%d", msg.Cols, msg.Rows)
	}
}

func TestNewHasChildProcessesMessage(t *testing.T) {
	msg := NewHasChildProcessesMessage(true)
	if msg.Type != MessageTypeHasChildProcesses {
		t.Errorf("expected type has_child_processes, got %s", msg.Type)
	}
	if msg.Data != "true" {
		t.Errorf("expected data 'true', got %q", msg.Data)
	}

	msg2 := NewHasChildProcessesMessage(false)
	if msg2.Data != "false" {
		t.Errorf("expected data 'false', got %q", msg2.Data)
	}
}

func TestMessageRoundtrip(t *testing.T) {
	tests := []Message{
		NewInputMessage("test"),
		NewOutputMessage("out"),
		NewResizeMessage(80, 24),
		NewExitMessage(1),
		NewErrorMessage("err"),
		NewResumeMessage(50),
		NewReplayMessage("replay"),
		NewSyncMessage(99),
		NewOrphanReqMessage(),
		NewOrphanAckMessage(),
		NewDetachMessage(),
		NewSerializeMessage("data", 100, 30),
		NewHasChildProcessesMessage(true),
	}

	for i, msg := range tests {
		data, err := EncodeMessage(msg)
		if err != nil {
			t.Errorf("test %d: EncodeMessage failed: %v", i, err)
			continue
		}

		// Verify it's valid JSON
		if !json.Valid(data) {
			t.Errorf("test %d: output is not valid JSON", i)
		}

		decoded, err := DecodeMessage(data)
		if err != nil {
			t.Errorf("test %d: DecodeMessage failed: %v", i, err)
			continue
		}

		if decoded.Type != msg.Type {
			t.Errorf("test %d: type mismatch: expected %s, got %s", i, msg.Type, decoded.Type)
		}
	}
}