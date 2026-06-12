package terminal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager_Defaults(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.maxSessions != 10 {
		t.Errorf("expected default maxSessions 10, got %d", m.maxSessions)
	}
	if m.reviveMode != ReviveOnExit {
		t.Errorf("expected default reviveMode onExit, got %s", m.reviveMode)
	}
	defer m.Close()
}

func TestNewManager_CustomConfig(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "axons-mgr-test-cfg")
	defer os.RemoveAll(dir)

	m := NewManager(ManagerConfig{
		MaxSessions: 5,
		ReviveMode:  ReviveNever,
		PersistDir:  dir,
	})
	defer m.Close()

	if m.maxSessions != 5 {
		t.Errorf("expected maxSessions 5, got %d", m.maxSessions)
	}
	if m.reviveMode != ReviveNever {
		t.Errorf("expected reviveMode never, got %s", m.reviveMode)
	}
}

func TestManager_CreateSession(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 5})
	defer m.Close()

	session, err := m.CreateSession("", "", 80, 24)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if session == nil {
		t.Fatal("CreateSession returned nil session")
	}
	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if session.Status != "running" {
		t.Errorf("expected status running, got %s", session.Status)
	}
}

func TestManager_GetSession(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 5})
	defer m.Close()

	session, err := m.CreateSession("", "", 80, 24)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	found, err := m.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if found.ID != session.ID {
		t.Errorf("expected session ID %s, got %s", session.ID, found.ID)
	}
}

func TestManager_GetSession_NotFound(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 5})
	defer m.Close()

	_, err := m.GetSession("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestManager_GetSession_EmptyID(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 5})
	defer m.Close()

	_, err := m.GetSession("")
	if err == nil {
		t.Error("expected error for empty session ID")
	}
}

func TestManager_KillSession(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 5})
	defer m.Close()

	session, err := m.CreateSession("", "", 80, 24)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = m.KillSession(session.ID)
	if err != nil {
		t.Fatalf("KillSession failed: %v", err)
	}

	// Verify session is gone
	_, err = m.GetSession(session.ID)
	if err == nil {
		t.Error("expected error after kill")
	}
}

func TestManager_KillSession_NotFound(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 5})
	defer m.Close()

	err := m.KillSession("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestManager_ListSessions(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 5})
	defer m.Close()

	s1, _ := m.CreateSession("", "", 80, 24)
	s2, _ := m.CreateSession("", "", 80, 24)

	sessions := m.ListSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	ids := map[string]bool{s1.ID: true, s2.ID: true}
	for _, s := range sessions {
		if !ids[s.ID] {
			t.Errorf("unexpected session ID: %s", s.ID)
		}
	}
}

func TestManager_SessionCount(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 5})
	defer m.Close()

	if m.SessionCount() != 0 {
		t.Error("expected 0 sessions initially")
	}

	m.CreateSession("", "", 80, 24)
	if m.SessionCount() != 1 {
		t.Errorf("expected 1 session, got %d", m.SessionCount())
	}

	m.CreateSession("", "", 80, 24)
	if m.SessionCount() != 2 {
		t.Errorf("expected 2 sessions, got %d", m.SessionCount())
	}
}

func TestManager_MaxSessions(t *testing.T) {
	m := NewManager(ManagerConfig{MaxSessions: 2})
	defer m.Close()

	_, err1 := m.CreateSession("", "", 80, 24)
	_, err2 := m.CreateSession("", "", 80, 24)
	_, err3 := m.CreateSession("", "", 80, 24)

	if err1 != nil || err2 != nil {
		t.Fatalf("first two sessions should succeed: %v, %v", err1, err2)
	}
	if err3 == nil {
		t.Error("expected error when exceeding max sessions")
	}
}

func TestManager_OrphanReqCh(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Close()

	ch := m.OrphanReqCh()
	if ch == nil {
		t.Error("OrphanReqCh returned nil")
	}
}

func TestManager_DetachCh(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Close()

	ch := m.DetachCh()
	if ch == nil {
		t.Error("DetachCh returned nil")
	}
}

func TestManager_PersistMgr(t *testing.T) {
	m := NewManager(ManagerConfig{})
	defer m.Close()

	pm := m.PersistMgr()
	if pm == nil {
		t.Error("PersistMgr returned nil")
	}
}