package terminal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewPersistManager(t *testing.T) {
	pm := NewPersistManager("")
	if pm == nil {
		t.Fatal("NewPersistManager returned nil")
	}
	// Default dir should be set
	if pm.Dir() == "" {
		t.Error("expected non-empty default dir")
	}
}

func TestNewPersistManager_CustomDir(t *testing.T) {
	pm := NewPersistManager("/tmp/test-snapshots")
	if pm.Dir() != "/tmp/test-snapshots" {
		t.Errorf("expected /tmp/test-snapshots, got %s", pm.Dir())
	}
}

func TestPersistManager_EnsureDir(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "axons-persist-test-ensure")
	defer os.RemoveAll(dir)

	pm := NewPersistManager(dir)
	err := pm.EnsureDir()
	if err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestPersistManager_WriteAndReadSnapshot(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "axons-persist-test-rw")
	defer os.RemoveAll(dir)

	pm := NewPersistManager(dir)

	snap := &SessionSnapshot{
		ID: "test-session-1",
		ShellLaunchConfig: ShellLaunchConfig{
			Executable: "/bin/zsh",
			Cwd:        "/home/user",
		},
		ReplayEvent: ReplayEvent{
			Events: []ReplayEventEntry{
				{Cols: 80, Rows: 24, Data: "\x1b[0m"},
			},
		},
		Timestamp: time.Now().UnixMilli(),
		Source:    "serialize",
	}

	err := pm.WriteSnapshot(snap)
	if err != nil {
		t.Fatalf("WriteSnapshot failed: %v", err)
	}

	read, err := pm.ReadSnapshot("test-session-1")
	if err != nil {
		t.Fatalf("ReadSnapshot failed: %v", err)
	}
	if read == nil {
		t.Fatal("ReadSnapshot returned nil")
	}
	if read.ID != "test-session-1" {
		t.Errorf("expected ID test-session-1, got %s", read.ID)
	}
	if read.ShellLaunchConfig.Executable != "/bin/zsh" {
		t.Errorf("expected /bin/zsh, got %s", read.ShellLaunchConfig.Executable)
	}
	if read.Source != "serialize" {
		t.Errorf("expected source serialize, got %s", read.Source)
	}
}

func TestPersistManager_ReadSnapshot_NotExist(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "axons-persist-test-noexist")
	defer os.RemoveAll(dir)

	pm := NewPersistManager(dir)

	snap, err := pm.ReadSnapshot("nonexistent")
	if err != nil {
		t.Fatalf("ReadSnapshot should not error for missing file: %v", err)
	}
	if snap != nil {
		t.Error("expected nil for missing snapshot")
	}
}

func TestPersistManager_DeleteSnapshot(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "axons-persist-test-del")
	defer os.RemoveAll(dir)

	pm := NewPersistManager(dir)

	snap := &SessionSnapshot{
		ID:        "del-session",
		Timestamp: time.Now().UnixMilli(),
		Source:    "serialize",
	}

	err := pm.WriteSnapshot(snap)
	if err != nil {
		t.Fatalf("WriteSnapshot failed: %v", err)
	}

	err = pm.DeleteSnapshot("del-session")
	if err != nil {
		t.Fatalf("DeleteSnapshot failed: %v", err)
	}

	// Verify deleted
	read, err := pm.ReadSnapshot("del-session")
	if err != nil {
		t.Fatalf("ReadSnapshot after delete failed: %v", err)
	}
	if read != nil {
		t.Error("expected nil after delete")
	}
}

func TestPersistManager_DeleteSnapshot_NotExist(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "axons-persist-test-delno")
	defer os.RemoveAll(dir)

	pm := NewPersistManager(dir)

	// Deleting nonexistent should not error
	err := pm.DeleteSnapshot("nonexistent")
	if err != nil {
		t.Fatalf("DeleteSnapshot should not error for missing file: %v", err)
	}
}

func TestPersistManager_ReadAllSnapshots(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "axons-persist-test-readall")
	defer os.RemoveAll(dir)

	pm := NewPersistManager(dir)

	// Write multiple snapshots
	for i := 0; i < 3; i++ {
		snap := &SessionSnapshot{
			ID:        fmt.Sprintf("session-%c", 'a'+i),
			Timestamp: time.Now().UnixMilli(),
			Source:    "serialize",
		}
		if err := pm.WriteSnapshot(snap); err != nil {
			t.Fatalf("WriteSnapshot %d failed: %v", i, err)
		}
	}

	snapshots, err := pm.ReadAllSnapshots()
	if err != nil {
		t.Fatalf("ReadAllSnapshots failed: %v", err)
	}
	if len(snapshots) != 3 {
		t.Errorf("expected 3 snapshots, got %d", len(snapshots))
	}
}

func TestPersistManager_CleanupStaleSnapshots(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "axons-persist-test-cleanup")
	defer os.RemoveAll(dir)

	pm := NewPersistManager(dir)

	// Write a stale snapshot (old timestamp)
	staleSnap := &SessionSnapshot{
		ID:        "stale-session",
		Timestamp: time.Now().Add(-48 * time.Hour).UnixMilli(), // 48h ago
		Source:    "serialize",
	}
	if err := pm.WriteSnapshot(staleSnap); err != nil {
		t.Fatalf("WriteSnapshot stale failed: %v", err)
	}

	// Write a fresh snapshot
	freshSnap := &SessionSnapshot{
		ID:        "fresh-session",
		Timestamp: time.Now().UnixMilli(),
		Source:    "serialize",
	}
	if err := pm.WriteSnapshot(freshSnap); err != nil {
		t.Fatalf("WriteSnapshot fresh failed: %v", err)
	}

	// Cleanup snapshots older than 24h
	count := pm.CleanupStaleSnapshots(24 * time.Hour)
	if count != 1 {
		t.Errorf("expected 1 stale snapshot cleaned, got %d", count)
	}

	// Verify fresh snapshot still exists
	read, err := pm.ReadSnapshot("fresh-session")
	if err != nil {
		t.Fatalf("ReadSnapshot fresh failed: %v", err)
	}
	if read == nil {
		t.Error("fresh snapshot should still exist")
	}

	// Verify stale snapshot is gone
	read, err = pm.ReadSnapshot("stale-session")
	if err != nil {
		t.Fatalf("ReadSnapshot stale failed: %v", err)
	}
	if read != nil {
		t.Error("stale snapshot should be deleted")
	}
}

func TestReviveProcessMode_Constants(t *testing.T) {
	if ReviveOnExit != ReviveProcessMode("onExit") {
		t.Error("ReviveOnExit constant mismatch")
	}
	if ReviveOnExitAndWindowClose != ReviveProcessMode("onExitAndWindowClose") {
		t.Error("ReviveOnExitAndWindowClose constant mismatch")
	}
	if ReviveNever != ReviveProcessMode("never") {
		t.Error("ReviveNever constant mismatch")
	}
}