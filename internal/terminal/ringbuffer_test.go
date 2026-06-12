package terminal

import (
	"testing"
)

func TestNewRingBuffer(t *testing.T) {
	rb := NewRingBuffer(10)
	if rb == nil {
		t.Fatal("NewRingBuffer returned nil")
	}
	if rb.cap != 10 {
		t.Errorf("expected cap 10, got %d", rb.cap)
	}
}

func TestNewRingBuffer_DefaultCap(t *testing.T) {
	rb := NewRingBuffer(0)
	if rb.cap != 1024 {
		t.Errorf("expected default cap 1024, got %d", rb.cap)
	}
	rb2 := NewRingBuffer(-1)
	if rb2.cap != 1024 {
		t.Errorf("expected default cap 1024, got %d", rb2.cap)
	}
}

func TestRingBuffer_WriteAndReadSince(t *testing.T) {
	rb := NewRingBuffer(5)

	// Write 3 entries
	rb.Write(OutputEntry{Seq: 1, Data: []byte("a")})
	rb.Write(OutputEntry{Seq: 2, Data: []byte("b")})
	rb.Write(OutputEntry{Seq: 3, Data: []byte("c")})

	// ReadSince(0) should return all 3
	entries := rb.ReadSince(0)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Seq != 1 || entries[1].Seq != 2 || entries[2].Seq != 3 {
		t.Errorf("unexpected seq order: %v", entries)
	}

	// ReadSince(1) should return entries with seq > 1
	entries = rb.ReadSince(1)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Seq != 2 || entries[1].Seq != 3 {
		t.Errorf("unexpected seq order: %v", entries)
	}

	// ReadSince(3) should return 0 entries
	entries = rb.ReadSince(3)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestRingBuffer_Overwrite(t *testing.T) {
	rb := NewRingBuffer(3)

	rb.Write(OutputEntry{Seq: 1, Data: []byte("a")})
	rb.Write(OutputEntry{Seq: 2, Data: []byte("b")})
	rb.Write(OutputEntry{Seq: 3, Data: []byte("c")})
	// Buffer is full, next write overwrites oldest
	rb.Write(OutputEntry{Seq: 4, Data: []byte("d")})

	// Should have entries 2, 3, 4
	entries := rb.ReadSince(0)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Seq != 2 || entries[1].Seq != 3 || entries[2].Seq != 4 {
		t.Errorf("unexpected entries after overwrite: %v", entries)
	}

	// minSeq should be updated to 2
	if rb.minSeq != 2 {
		t.Errorf("expected minSeq 2, got %d", rb.minSeq)
	}
}

func TestRingBuffer_LatestSeq(t *testing.T) {
	rb := NewRingBuffer(5)

	// Empty buffer
	if rb.LatestSeq() != 0 {
		t.Errorf("expected 0 for empty buffer, got %d", rb.LatestSeq())
	}

	rb.Write(OutputEntry{Seq: 1, Data: []byte("a")})
	rb.Write(OutputEntry{Seq: 2, Data: []byte("b")})
	rb.Write(OutputEntry{Seq: 5, Data: []byte("c")})

	if rb.LatestSeq() != 5 {
		t.Errorf("expected latest seq 5, got %d", rb.LatestSeq())
	}
}

func TestRingBuffer_ReadSince_Empty(t *testing.T) {
	rb := NewRingBuffer(5)
	entries := rb.ReadSince(0)
	if entries != nil {
		t.Errorf("expected nil for empty buffer, got %v", entries)
	}
}

func TestRingBuffer_SingleEntry(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Write(OutputEntry{Seq: 42, Data: []byte("x")})

	if rb.LatestSeq() != 42 {
		t.Errorf("expected latest seq 42, got %d", rb.LatestSeq())
	}

	entries := rb.ReadSince(41)
	if len(entries) != 1 || entries[0].Seq != 42 {
		t.Errorf("expected [42], got %v", entries)
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	rb := NewRingBuffer(4)

	// Fill and wrap around multiple times
	for i := uint64(1); i <= 12; i++ {
		rb.Write(OutputEntry{Seq: i, Data: []byte{byte(i)}})
	}

	// Should have last 4 entries: 9, 10, 11, 12
	entries := rb.ReadSince(0)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Seq != 9 || entries[3].Seq != 12 {
		t.Errorf("unexpected entries after wrap: %v", entries)
	}

	if rb.LatestSeq() != 12 {
		t.Errorf("expected latest seq 12, got %d", rb.LatestSeq())
	}
}