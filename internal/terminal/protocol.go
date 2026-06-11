package terminal

import (
	"encoding/json"
	"fmt"
)

// MessageType defines the type of WebSocket message.
type MessageType string

const (
	MessageTypeInput    MessageType = "input"    // User input
	MessageTypeOutput   MessageType = "output"   // Terminal output
	MessageTypeResize   MessageType = "resize"   // Window resize
	MessageTypeExit     MessageType = "exit"     // Process exit
	MessageTypeError    MessageType = "error"    // Error message
	MessageTypePing     MessageType = "ping"     // Heartbeat ping
	MessageTypePong     MessageType = "pong"     // Heartbeat pong
	MessageTypeClose    MessageType = "close"    // User initiated close (distinguishes from accidental disconnect)
	MessageTypeResume   MessageType = "resume"   // Client requests replay from a sequence number
	MessageTypeReplay   MessageType = "replay"   // Server sends replayed output (batch of historical data)
	MessageTypeSync     MessageType = "sync"     // Server sends current seq after replay completes
	MessageTypeOrphanReq MessageType = "orphan_req" // Server→Client: asking if frontend is still alive
	MessageTypeOrphanAck MessageType = "orphan_ack" // Client→Server: confirming frontend is alive
	MessageTypeDetach    MessageType = "detach"     // Server→Client: session will be killed (grace expired)
	MessageTypeSerialize MessageType = "serialize"  // Server→Client: ANSI stream to reconstruct terminal state
	MessageTypeHasChildProcesses MessageType = "has_child_processes" // Server→Client: child process state change
	MessageTypeReplayComplete  MessageType = "replay_complete" // Client→Server: replay finished, resume normal I/O
)

// Message represents a WebSocket message.
type Message struct {
	Type MessageType `json:"type"`
	Data string      `json:"data,omitempty"`
	Cols uint16      `json:"cols,omitempty"`
	Rows uint16      `json:"rows,omitempty"`
	Code int         `json:"code,omitempty"`  // Exit code
	Seq  uint64      `json:"seq,omitempty"`   // Sequence number (for resume/replay/sync)
}

// EncodeMessage encodes a message to JSON.
func EncodeMessage(msg Message) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeMessage decodes a message from JSON.
func DecodeMessage(data []byte) (Message, error) {
	var msg Message
	err := json.Unmarshal(data, &msg)
	return msg, err
}

// NewInputMessage creates an input message.
func NewInputMessage(data string) Message {
	return Message{
		Type: MessageTypeInput,
		Data: data,
	}
}

// NewOutputMessage creates an output message.
func NewOutputMessage(data string) Message {
	return Message{
		Type: MessageTypeOutput,
		Data: data,
	}
}

// NewResizeMessage creates a resize message.
func NewResizeMessage(cols, rows uint16) Message {
	return Message{
		Type: MessageTypeResize,
		Cols: cols,
		Rows: rows,
	}
}

// NewExitMessage creates an exit message.
func NewExitMessage(code int) Message {
	return Message{
		Type: MessageTypeExit,
		Code: code,
	}
}

// NewErrorMessage creates an error message.
func NewErrorMessage(err string) Message {
	return Message{
		Type: MessageTypeError,
		Data: err,
	}
}

// NewResumeMessage creates a resume message (client -> server).
// The client sends this on reconnection with the last sequence number it received.
func NewResumeMessage(lastSeq uint64) Message {
	return Message{
		Type: MessageTypeResume,
		Seq:  lastSeq,
	}
}

// NewReplayMessage creates a replay message (server -> client).
// Contains a batch of historical output data for reconnection replay.
func NewReplayMessage(data string) Message {
	return Message{
		Type: MessageTypeReplay,
		Data: data,
	}
}

// NewSyncMessage creates a sync message (server -> client).
// Sent after replay completes to inform client of the current sequence number.
func NewSyncMessage(currentSeq uint64) Message {
	return Message{
		Type: MessageTypeSync,
		Seq:  currentSeq,
	}
}

// NewOrphanReqMessage creates an orphan request message (server -> client).
// Sent when long grace timer expires, asking if the frontend is still alive.
// Aligns with IDE AutoOpenBarrier(4000) pattern.
func NewOrphanReqMessage() Message {
	return Message{
		Type: MessageTypeOrphanReq,
	}
}

// NewOrphanAckMessage creates an orphan acknowledgment message (client -> server).
// Sent by frontend to confirm it's alive when receiving orphan_req.
func NewOrphanAckMessage() Message {
	return Message{
		Type: MessageTypeOrphanAck,
	}
}

// NewDetachMessage creates a detach message (server -> client).
// Sent before killing the session when grace time expires.
// Frontend should disable input and stop reconnecting on receipt.
func NewDetachMessage() Message {
	return Message{
		Type: MessageTypeDetach,
	}
}

// NewSerializeMessage creates a serialize message (server -> client).
// Contains an ANSI sequence stream that reconstructs the full terminal state
// (scrollback, viewport, cursor, modes) when fed to xterm.js.
// Aligns with IDE's XtermSerializer.generateReplayEvent().
// cols/rows are the terminal dimensions at the time of serialization,
// used by frontend to set OverrideDimensions{forceExactSize: true} during replay.
func NewSerializeMessage(data string, cols, rows uint16) Message {
	return Message{
		Type: MessageTypeSerialize,
		Data: data,
		Cols: cols,
		Rows: rows,
	}
}

// NewHasChildProcessesMessage creates a has_child_processes message (server -> client).
// Sent when the child process monitor detects a change in child process state.
// Aligns with IDE ProcessPropertyType.HasChildProcesses.
func NewHasChildProcessesMessage(has bool) Message {
	return Message{
		Type: MessageTypeHasChildProcesses,
		Data: fmt.Sprintf("%v", has),
	}
}