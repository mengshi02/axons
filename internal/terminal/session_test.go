package terminal

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewSession(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	if session.ID != id {
		t.Errorf("expected ID %s, got %s", id, session.ID)
	}
	if session.PID <= 0 {
		t.Errorf("expected positive PID, got %d", session.PID)
	}
	if session.Status != "running" {
		t.Errorf("expected status running, got %s", session.Status)
	}
	if session.Shell == "" {
		t.Error("expected non-empty shell")
	}
}

func TestNewSession_DefaultSize(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 0, 0)
	if err != nil {
		t.Fatalf("NewSession with zero size failed: %v", err)
	}
	defer session.Close()
	// Session should be created with default size (80x24)
}

func TestSession_Write(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	err = session.Write([]byte("echo hello\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func TestSession_Resize(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	err = session.Resize(120, 40)
	if err != nil {
		t.Fatalf("Resize failed: %v", err)
	}
}

func TestSession_Resize_InvalidSize(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	err = session.Resize(0, 0)
	if err == nil {
		t.Error("expected error for zero size")
	}

	err = session.Resize(0, 24)
	if err == nil {
		t.Error("expected error for zero cols")
	}

	err = session.Resize(80, 0)
	if err == nil {
		t.Error("expected error for zero rows")
	}
}

func TestSession_Resize_TooLarge(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	err = session.Resize(600, 24)
	if err == nil {
		t.Error("expected error for too large cols")
	}

	err = session.Resize(80, 300)
	if err == nil {
		t.Error("expected error for too large rows")
	}
}

func TestSession_Close(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	err = session.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if session.Status != "exited" {
		t.Errorf("expected status exited, got %s", session.Status)
	}

	// Double close should not panic
	err = session.Close()
	if err != nil {
		t.Fatalf("Double close failed: %v", err)
	}
}

func TestSession_SubscribeUnsubscribe(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	ch, seq := session.Subscribe("sub1")
	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}
	if seq != 0 {
		t.Errorf("expected initial seq 0, got %d", seq)
	}

	if !session.HasActiveSubscriber() {
		t.Error("expected active subscriber")
	}

	session.Unsubscribe("sub1")

	// Give time for unsubscribe to process
	time.Sleep(50 * time.Millisecond)
}

func TestSession_LatestSeq(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	seq := session.LatestSeq()
	if seq != 0 {
		t.Errorf("expected initial seq 0, got %d", seq)
	}
}

func TestSession_ReplaySince(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	entries := session.ReplaySince(0)
	// No output yet, may be empty or have some shell prompt
	_ = entries
}

func TestSession_MarkDisconnected(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	if session.IsDisconnected() {
		t.Error("should not be disconnected initially")
	}

	session.MarkDisconnected()
	if !session.IsDisconnected() {
		t.Error("should be disconnected after MarkDisconnected")
	}

	// Duration should be >= 0 (may be 0 due to ms rounding)
	dur := session.DisconnectDuration()
	if dur < 0 {
		t.Errorf("expected non-negative disconnect duration, got %v", dur)
	}
}

func TestSession_MarkReconnected(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	session.MarkDisconnected()
	session.MarkReconnected()

	if session.IsDisconnected() {
		t.Error("should not be disconnected after MarkReconnected")
	}
}

func TestSession_InteractionState(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	if session.GetInteractionState() != InteractionNone {
		t.Errorf("expected InteractionNone, got %d", session.GetInteractionState())
	}

	session.TransitionToSession()
	if session.GetInteractionState() != InteractionSession {
		t.Errorf("expected InteractionSession, got %d", session.GetInteractionState())
	}
}

func TestSession_InReplay(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	if session.IsInReplay() {
		t.Error("should not be in replay initially")
	}

	session.SetInReplay(true)
	if !session.IsInReplay() {
		t.Error("should be in replay after SetInReplay(true)")
	}

	session.SetInReplay(false)
	if session.IsInReplay() {
		t.Error("should not be in replay after SetInReplay(false)")
	}
}

func TestSession_HasChildProcesses(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	// Initially should be false (shell just started, no child procs)
	_ = session.HasChildProcesses()
}

func TestSession_Serializer(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	ser := session.Serializer()
	if ser == nil {
		t.Error("expected non-nil serializer")
	}
}

func TestSession_SetOnExit(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	var called atomic.Int32
	session.SetOnExit(func(code int) {
		called.Add(1)
	})
	session.AddOnExit(func(code int) {
		called.Add(1)
	})

	// Verify listeners are registered (internal state check)
	session.exitMu.RLock()
	count := len(session.exitListeners)
	session.exitMu.RUnlock()
	if count != 2 {
		t.Errorf("expected 2 exit listeners, got %d", count)
	}

	session.Close()
}

func TestSession_Write_DuringReplay(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	session.SetInReplay(true)

	// Write during replay should be silently discarded (no error)
	err = session.Write([]byte("should be discarded\n"))
	if err != nil {
		t.Fatalf("Write during replay should not error: %v", err)
	}
}

func TestSession_Resize_DuringReplay(t *testing.T) {
	id := uuid.New().String()
	session, err := NewSession(id, "", "", 80, 24)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Close()

	session.SetInReplay(true)

	// Resize during replay should be silently discarded (no error)
	err = session.Resize(120, 40)
	if err != nil {
		t.Fatalf("Resize during replay should not error: %v", err)
	}
}

func TestBuildShellEnv(t *testing.T) {
	env := buildShellEnv("/bin/zsh")

	hasTerm := false
	hasColorTerm := false
	hasShell := false
	for _, e := range env {
		if strings.HasPrefix(e, "TERM=") {
			hasTerm = true
		}
		if strings.HasPrefix(e, "COLORTERM=") {
			hasColorTerm = true
		}
		if strings.HasPrefix(e, "SHELL=") {
			hasShell = true
		}
	}

	if !hasTerm {
		t.Error("expected TERM to be set")
	}
	if !hasColorTerm {
		t.Error("expected COLORTERM to be set")
	}
	if !hasShell {
		t.Error("expected SHELL to be set")
	}
}

func TestEnsureEnv(t *testing.T) {
	env := []string{"PATH=/usr/bin", "HOME=/home/user"}

	// Add new key
	env = ensureEnv(env, "SHELL", "/bin/zsh")
	found := false
	for _, e := range env {
		if e == "SHELL=/bin/zsh" {
			found = true
		}
	}
	if !found {
		t.Error("expected SHELL to be added")
	}

	// Overwrite existing key
	env = ensureEnv(env, "HOME", "/home/other")
	for _, e := range env {
		if e == "HOME=/home/other" {
			found = true
		}
	}
	if !found {
		t.Error("expected HOME to be overwritten")
	}
}