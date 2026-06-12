package graph

import (
	"testing"
	"time"
)

func TestJournalData_HasChanges(t *testing.T) {
	tests := []struct {
		name     string
		data     JournalData
		expected bool
	}{
		{"no_changes", JournalData{Changed: nil, Removed: nil}, false},
		{"has_changed", JournalData{Changed: []string{"a.go"}, Removed: nil}, true},
		{"has_removed", JournalData{Changed: nil, Removed: []string{"b.go"}}, true},
		{"both", JournalData{Changed: []string{"a.go"}, Removed: []string{"b.go"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.data.HasChanges(); got != tt.expected {
				t.Errorf("HasChanges() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJournalData_TotalChanges(t *testing.T) {
	tests := []struct {
		name     string
		data     JournalData
		expected int
	}{
		{"no_changes", JournalData{}, 0},
		{"only_changed", JournalData{Changed: []string{"a.go", "b.go"}}, 2},
		{"only_removed", JournalData{Removed: []string{"c.go"}}, 1},
		{"mixed", JournalData{Changed: []string{"a.go"}, Removed: []string{"b.go", "c.go"}}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.data.TotalChanges(); got != tt.expected {
				t.Errorf("TotalChanges() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestJournalData_IsFresh(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		data     JournalData
		maxAge   time.Duration
		expected bool
	}{
		{
			"invalid_not_fresh",
			JournalData{Valid: false, Timestamp: now.Unix()},
			1 * time.Hour,
			false,
		},
		{
			"recent_fresh",
			JournalData{Valid: true, Timestamp: now.Unix()},
			1 * time.Hour,
			true,
		},
		{
			"old_not_fresh",
			JournalData{Valid: true, Timestamp: now.Add(-2 * time.Hour).Unix()},
			1 * time.Hour,
			false,
		},
		{
			"just_at_boundary",
			JournalData{Valid: true, Timestamp: now.Add(-59 * time.Minute).Unix()},
			1 * time.Hour,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.data.IsFresh(tt.maxAge); got != tt.expected {
				t.Errorf("IsFresh() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseContent(t *testing.T) {
	j := &Journal{}

	tests := []struct {
		name          string
		content       string
		expectValid   bool
		expectChanged []string
		expectRemoved []string
	}{
		{
			"empty_content",
			"",
			false,
			nil,
			nil,
		},
		{
			"invalid_header",
			"# something else\nfile.go",
			false,
			nil,
			nil,
		},
		{
			"header_no_timestamp",
			"# axons-journal v1 \nfile.go",
			false,
			nil,
			nil,
		},
		{
			"header_zero_timestamp",
			"# axons-journal v1 0\nfile.go",
			false,
			nil,
			nil,
		},
		{
			"valid_header_only",
			"# axons-journal v1 1700000000\n",
			true,
			[]string{},
			[]string{},
		},
		{
			"valid_with_changed_files",
			"# axons-journal v1 1700000000\nfoo.go\nbar.go",
			true,
			[]string{"foo.go", "bar.go"},
			[]string{},
		},
		{
			"valid_with_deleted_files",
			"# axons-journal v1 1700000000\nDELETED old.go\nDELETED gone.go",
			true,
			[]string{},
			[]string{"old.go", "gone.go"},
		},
		{
			"valid_mixed_changes",
			"# axons-journal v1 1700000000\nfoo.go\nDELETED old.go\nbar.go\nDELETED gone.go",
			true,
			[]string{"foo.go", "bar.go"},
			[]string{"old.go", "gone.go"},
		},
		{
			"skips_comments_and_empty_lines",
			"# axons-journal v1 1700000000\n# comment\n\nfoo.go\n  \nbar.go",
			true,
			[]string{"foo.go", "bar.go"},
			[]string{},
		},
		{
			"deduplication_changed",
			"# axons-journal v1 1700000000\nfoo.go\nfoo.go",
			true,
			[]string{"foo.go"},
			[]string{},
		},
		{
			"deduplication_removed",
			"# axons-journal v1 1700000000\nDELETED old.go\nDELETED old.go",
			true,
			[]string{},
			[]string{"old.go"},
		},
		{
			"DELETED_with_trailing_space_goes_to_changed",
			"# axons-journal v1 1700000000\nDELETED \nfoo.go",
			true,
			[]string{"DELETED", "foo.go"},
			[]string{},
		},
		{
			"empty_path_skipped",
			"# axons-journal v1 1700000000\n   \nfoo.go",
			true,
			[]string{"foo.go"},
			[]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := j.parseContent(tt.content)
			if err != nil {
				t.Fatalf("parseContent() error: %v", err)
			}
			if data.Valid != tt.expectValid {
				t.Errorf("Valid = %v, want %v", data.Valid, tt.expectValid)
			}
			if !data.Valid {
				return
			}
			if len(data.Changed) != len(tt.expectChanged) {
				t.Errorf("Changed len = %d, want %d (got=%v, want=%v)", len(data.Changed), len(tt.expectChanged), data.Changed, tt.expectChanged)
			} else {
				for i, v := range data.Changed {
					if v != tt.expectChanged[i] {
						t.Errorf("Changed[%d] = %v, want %v", i, v, tt.expectChanged[i])
					}
				}
			}
			if len(data.Removed) != len(tt.expectRemoved) {
				t.Errorf("Removed len = %d, want %d (got=%v, want=%v)", len(data.Removed), len(tt.expectRemoved), data.Removed, tt.expectRemoved)
			} else {
				for i, v := range data.Removed {
					if v != tt.expectRemoved[i] {
						t.Errorf("Removed[%d] = %v, want %v", i, v, tt.expectRemoved[i])
					}
				}
			}
		})
	}
}

func TestJournalEntry(t *testing.T) {
	entry := JournalEntry{File: "main.go", Deleted: false}
	if entry.File != "main.go" {
		t.Errorf("File = %v, want main.go", entry.File)
	}
	if entry.Deleted {
		t.Errorf("Deleted = true, want false")
	}

	delEntry := JournalEntry{File: "old.go", Deleted: true}
	if delEntry.File != "old.go" {
		t.Errorf("File = %v, want old.go", delEntry.File)
	}
	if !delEntry.Deleted {
		t.Errorf("Deleted = false, want true")
	}
}