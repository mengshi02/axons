package terminal

import (
	"testing"
	"time"
)

func TestNewNativeSerializer(t *testing.T) {
	s := NewNativeSerializer()
	if s == nil {
		t.Fatal("NewNativeSerializer returned nil")
	}
	if len(s.terms) != 0 {
		t.Errorf("expected empty terms map, got %d entries", len(s.terms))
	}
}

func TestNativeSerializer_Create(t *testing.T) {
	s := NewNativeSerializer()

	err := s.Create("test-session", 80, 24, 1000)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify terminal exists
	s.mu.RLock()
	_, exists := s.terms["test-session"]
	s.mu.RUnlock()
	if !exists {
		t.Error("terminal not found after Create")
	}
}

func TestNativeSerializer_Create_Duplicate(t *testing.T) {
	s := NewNativeSerializer()

	err := s.Create("dup-session", 80, 24, 1000)
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	err = s.Create("dup-session", 80, 24, 1000)
	if err == nil {
		t.Error("expected error for duplicate Create, got nil")
	}
}

func TestNativeSerializer_Write(t *testing.T) {
	s := NewNativeSerializer()
	err := s.Create("write-test", 80, 24, 1000)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = s.Write("write-test", []byte("hello world"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

func TestNativeSerializer_Write_NotFound(t *testing.T) {
	s := NewNativeSerializer()

	err := s.Write("nonexistent", []byte("data"))
	if err == nil {
		t.Error("expected error for nonexistent terminal, got nil")
	}
}

func TestNativeSerializer_Resize(t *testing.T) {
	s := NewNativeSerializer()
	err := s.Create("resize-test", 80, 24, 1000)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = s.Resize("resize-test", 120, 40)
	if err != nil {
		t.Fatalf("Resize failed: %v", err)
	}

	// Verify size
	cols, rows, ok := s.Size("resize-test")
	if !ok {
		t.Fatal("Size returned not ok")
	}
	if cols != 120 || rows != 40 {
		t.Errorf("expected 120x40, got %dx%d", cols, rows)
	}
}

func TestNativeSerializer_Resize_NotFound(t *testing.T) {
	s := NewNativeSerializer()

	err := s.Resize("nonexistent", 80, 24)
	if err == nil {
		t.Error("expected error for nonexistent terminal, got nil")
	}
}

func TestNativeSerializer_Size_NotFound(t *testing.T) {
	s := NewNativeSerializer()

	cols, rows, ok := s.Size("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent terminal")
	}
	if cols != 0 || rows != 0 {
		t.Errorf("expected 0x0, got %dx%d", cols, rows)
	}
}

func TestNativeSerializer_Serialize(t *testing.T) {
	s := NewNativeSerializer()
	err := s.Create("serialize-test", 80, 24, 1000)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	resultCh := s.Serialize("serialize-test", true)
	select {
	case result := <-resultCh:
		if result.Err != nil {
			t.Fatalf("Serialize failed: %v", result.Err)
		}
		if result.Data == "" {
			t.Error("expected non-empty serialized data")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serialize timed out")
	}
}

func TestNativeSerializer_Serialize_NotFound(t *testing.T) {
	s := NewNativeSerializer()

	resultCh := s.Serialize("nonexistent", true)
	select {
	case result := <-resultCh:
		if result.Err == nil {
			t.Error("expected error for nonexistent terminal, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serialize timed out")
	}
}

func TestNativeSerializer_Destroy(t *testing.T) {
	s := NewNativeSerializer()
	err := s.Create("destroy-test", 80, 24, 1000)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = s.Destroy("destroy-test")
	if err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// Verify terminal is gone
	s.mu.RLock()
	_, exists := s.terms["destroy-test"]
	s.mu.RUnlock()
	if exists {
		t.Error("terminal still exists after Destroy")
	}
}

func TestNativeSerializer_Destroy_NotFound(t *testing.T) {
	s := NewNativeSerializer()

	err := s.Destroy("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent terminal, got nil")
	}
}