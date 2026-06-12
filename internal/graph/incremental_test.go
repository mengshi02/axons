package graph

import (
	"strings"
	"testing"
	"time"
)

func TestChangeSet_HasChanges(t *testing.T) {
	tests := []struct {
		name string
		cs   ChangeSet
		want bool
	}{
		{"no changes", ChangeSet{}, false},
		{"has added", ChangeSet{Added: []*DetectedChange{{Path: "a.go"}}}, true},
		{"has modified", ChangeSet{Modified: []*DetectedChange{{Path: "b.go"}}}, true},
		{"has deleted", ChangeSet{Deleted: []*DetectedChange{{Path: "c.go"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cs.HasChanges()
			if got != tt.want {
				t.Errorf("HasChanges() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChangeSet_TotalChanges(t *testing.T) {
	cs := ChangeSet{
		Added:    []*DetectedChange{{Path: "a.go"}, {Path: "b.go"}},
		Modified: []*DetectedChange{{Path: "c.go"}},
		Deleted:  []*DetectedChange{{Path: "d.go"}, {Path: "e.go"}, {Path: "f.go"}},
	}
	if cs.TotalChanges() != 6 {
		t.Errorf("TotalChanges() = %d, want 6", cs.TotalChanges())
	}

	cs2 := ChangeSet{}
	if cs2.TotalChanges() != 0 {
		t.Errorf("TotalChanges() empty = %d, want 0", cs2.TotalChanges())
	}
}

func TestChangeSet_GetChangeSummary(t *testing.T) {
	t.Run("full build", func(t *testing.T) {
		cs := ChangeSet{IsFull: true}
		summary := cs.GetChangeSummary()
		if !strings.Contains(summary, "Full build") {
			t.Errorf("GetChangeSummary(IsFull) = %q, should mention full build", summary)
		}
	})

	t.Run("no changes", func(t *testing.T) {
		cs := ChangeSet{}
		summary := cs.GetChangeSummary()
		if !strings.Contains(summary, "No changes") {
			t.Errorf("GetChangeSummary(empty) = %q, should mention no changes", summary)
		}
	})

	t.Run("with changes", func(t *testing.T) {
		cs := ChangeSet{
			Added:    []*DetectedChange{{Path: "a.go"}},
			Modified: []*DetectedChange{{Path: "b.go"}, {Path: "c.go"}},
			Deleted:  []*DetectedChange{{Path: "d.go"}},
		}
		summary := cs.GetChangeSummary()
		if !strings.Contains(summary, "4 files changed") {
			t.Errorf("GetChangeSummary = %q, should contain '4 files changed'", summary)
		}
		if !strings.Contains(summary, "1 added") {
			t.Errorf("GetChangeSummary = %q, should contain '1 added'", summary)
		}
		if !strings.Contains(summary, "2 modified") {
			t.Errorf("GetChangeSummary = %q, should contain '2 modified'", summary)
		}
		if !strings.Contains(summary, "1 deleted") {
			t.Errorf("GetChangeSummary = %q, should contain '1 deleted'", summary)
		}
	})
}

func TestChangeSet_GetChangedFiles(t *testing.T) {
	cs := ChangeSet{
		Added:    []*DetectedChange{{Path: "a.go"}},
		Modified: []*DetectedChange{{Path: "b.go"}},
		Deleted:  []*DetectedChange{{Path: "c.go"}},
	}
	files := cs.GetChangedFiles()
	if len(files) != 3 {
		t.Errorf("GetChangedFiles() = %d, want 3", len(files))
	}
	// Should contain all changed file paths
	expected := []string{"a.go", "b.go", "c.go"}
	for _, exp := range expected {
		found := false
		for _, f := range files {
			if f == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetChangedFiles() missing %q", exp)
		}
	}
}

func TestChangeSet_FilterByExtension(t *testing.T) {
	cs := ChangeSet{
		Added: []*DetectedChange{
			{Path: "main.go"},
			{Path: "app.py"},
			{Path: "index.js"},
		},
		Modified: []*DetectedChange{
			{Path: "utils.go"},
			{Path: "config.yaml"},
		},
		Deleted: []*DetectedChange{
			{Path: "old.go"},
			{Path: "test.py"},
		},
	}

	t.Run("filter .go only", func(t *testing.T) {
		goExt := map[string]bool{".go": true}
		filtered := cs.FilterByExtension(goExt)
		total := len(filtered.Added) + len(filtered.Modified) + len(filtered.Deleted)
		if total != 3 { // main.go, utils.go, old.go
			t.Errorf("FilterByExtension(.go) total = %d, want 3", total)
		}
	})

	t.Run("filter empty map", func(t *testing.T) {
		emptyExt := map[string]bool{}
		filtered := cs.FilterByExtension(emptyExt)
		total := len(filtered.Added) + len(filtered.Modified) + len(filtered.Deleted)
		if total != 0 {
			t.Errorf("FilterByExtension(empty) total = %d, want 0", total)
		}
	})
}

func TestDetectedChange_Fields(t *testing.T) {
	now := time.Now()
	change := &DetectedChange{
		Path:       "internal/foo.go",
		ChangeType: ChangeModified,
		OldHash:    "abc123",
		NewHash:    "def456",
		OldModTime: now.Add(-time.Hour),
		NewModTime: now,
		OldSize:    100,
		NewSize:    150,
		Content:    []byte("package foo"),
	}
	if change.Path != "internal/foo.go" {
		t.Errorf("Path = %q", change.Path)
	}
	if change.ChangeType != ChangeModified {
		t.Errorf("ChangeType = %v", change.ChangeType)
	}
	if change.OldHash != "abc123" {
		t.Errorf("OldHash = %q", change.OldHash)
	}
}