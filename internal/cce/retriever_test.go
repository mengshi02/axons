package cce

import (
	"testing"
)

func TestMatchFilePattern(t *testing.T) {
	tests := []struct {
		file    string
		pattern string
		want    bool
	}{
		{"internal/cce/engine.go", "cce", true},
		{"internal/cce/engine.go", "CCE", true}, // case-insensitive
		{"internal/graph/resolve.go", "cce", false},
		{"src/utils/helper.go", "helper", true},
		{"", "anything", false},
		{"file.go", "", true}, // empty pattern matches everything
	}
	for _, tt := range tests {
		got := matchFilePattern(tt.file, tt.pattern)
		if got != tt.want {
			t.Errorf("matchFilePattern(%q, %q) = %v, want %v", tt.file, tt.pattern, got, tt.want)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		file string
		want bool
	}{
		{"engine_test.go", true},
		{"test_foo.py", false},     // pattern is _test.py, not test_ prefix
		{"bar_test.js", true},
		{"component.test.ts", true},
		{"spec.module.ts", false}, // pattern is .spec.ts, not spec. prefix
		{"src/test/helper.go", true},
		{"src/tests/main.go", true},
		{"src/__tests__/app.tsx", true},
		{"src/main.go", false},
		{"testing.go", false},       // "testing" contains "test" but not the pattern
		{"testdata/fixture.json", false},
	}
	for _, tt := range tests {
		got := isTestFile(tt.file)
		if got != tt.want {
			t.Errorf("isTestFile(%q) = %v, want %v", tt.file, got, tt.want)
		}
	}
}

func TestSanitizeFTS5Query(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "hello world", "hello world"},
		{"special chars", "func(name: string)", "func name string"},
		{"brackets and operators", "a && b || c", "a b c"},
		{"quotes", `"hello" world's`, "hello world s"}, // ' is stripped, 's becomes separate
		{"multiple spaces", "a   b    c", "a b c"},
		{"empty", "", ""},
		{"unicode", "函数调用", ""}, // CJK not matched by \w in Go regex
		{"mixed", "pkg.Func(var *Type) error", "pkg Func var Type error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFTS5Query(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFTS5Query(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestApplyFilters(t *testing.T) {
	retriever := &ContextRetriever{}

	t.Run("no filters", func(t *testing.T) {
		results := []*RetrievalResult{
			{NodeID: 1, Name: "Foo", File: "internal/foo.go"},
			{NodeID: 2, Name: "Bar", File: "internal/bar.go"},
		}
		query := &RetrievalQuery{}
		filtered := retriever.applyFilters(results, query)
		if len(filtered) != 2 {
			t.Errorf("applyFilters no filters = %d, want 2", len(filtered))
		}
	})

	t.Run("file filter", func(t *testing.T) {
		results := []*RetrievalResult{
			{NodeID: 1, Name: "Foo", File: "internal/cce/engine.go"},
			{NodeID: 2, Name: "Bar", File: "internal/graph/resolve.go"},
		}
		query := &RetrievalQuery{FileFilter: "cce"}
		filtered := retriever.applyFilters(results, query)
		if len(filtered) != 1 {
			t.Errorf("applyFilters file filter = %d, want 1", len(filtered))
		}
		if filtered[0].Name != "Foo" {
			t.Errorf("filtered result = %q, want Foo", filtered[0].Name)
		}
	})

	t.Run("exclude tests", func(t *testing.T) {
		results := []*RetrievalResult{
			{NodeID: 1, Name: "Foo", File: "internal/foo.go"},
			{NodeID: 2, Name: "TestFoo", File: "internal/foo_test.go"},
			{NodeID: 3, Name: "Bar", File: "src/__tests__/bar.ts"},
		}
		query := &RetrievalQuery{NoTests: true}
		filtered := retriever.applyFilters(results, query)
		if len(filtered) != 1 {
			t.Errorf("applyFilters exclude tests = %d, want 1", len(filtered))
		}
		if filtered[0].Name != "Foo" {
			t.Errorf("filtered result = %q, want Foo", filtered[0].Name)
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		results := []*RetrievalResult{
			{NodeID: 1, Name: "Foo", File: "internal/cce/engine.go"},
			{NodeID: 2, Name: "Bar", File: "internal/cce/engine_test.go"},
			{NodeID: 3, Name: "Baz", File: "internal/graph/resolve.go"},
		}
		query := &RetrievalQuery{FileFilter: "cce", NoTests: true}
		filtered := retriever.applyFilters(results, query)
		if len(filtered) != 1 {
			t.Errorf("applyFilters combined = %d, want 1", len(filtered))
		}
		if filtered[0].Name != "Foo" {
			t.Errorf("filtered result = %q, want Foo", filtered[0].Name)
		}
	})
}

func TestMergeWithRRF(t *testing.T) {
	retriever := &ContextRetriever{}
	k := 60

	t.Run("single source", func(t *testing.T) {
		results := []*RetrievalResult{
			{NodeID: 1, Score: 0.9, Source: "semantic"},
			{NodeID: 2, Score: 0.7, Source: "semantic"},
		}
		merged := retriever.mergeWithRRF(results, k)
		if len(merged) != 2 {
			t.Errorf("mergeWithRRF single source = %d results, want 2", len(merged))
		}
		// Higher score should have higher RRF score
		if merged[0].Score < merged[1].Score {
			t.Error("higher original score should have higher RRF score")
		}
	})

	t.Run("multiple sources merge same node", func(t *testing.T) {
		results := []*RetrievalResult{
			{NodeID: 1, Score: 0.9, Source: "semantic"},
			{NodeID: 1, Score: 0.8, Source: "keyword"},
			{NodeID: 2, Score: 0.7, Source: "semantic"},
		}
		merged := retriever.mergeWithRRF(results, k)
		// Node 1 from two sources should be merged into one
		nodeIDs := make(map[int64]bool)
		for _, r := range merged {
			nodeIDs[r.NodeID] = true
		}
		if len(nodeIDs) != 2 {
			t.Errorf("mergeWithRRF merged = %d unique nodes, want 2", len(nodeIDs))
		}
		// Node 1 should have combined source label
		for _, r := range merged {
			if r.NodeID == 1 {
				if r.Source == "semantic" || r.Source == "keyword" {
					t.Errorf("merged node should have combined source, got %q", r.Source)
				}
			}
		}
	})

	t.Run("empty input", func(t *testing.T) {
		merged := retriever.mergeWithRRF([]*RetrievalResult{}, k)
		if len(merged) != 0 {
			t.Errorf("mergeWithRRF empty = %d, want 0", len(merged))
		}
	})

	t.Run("RRF score calculation", func(t *testing.T) {
		// Node appearing in rank 0 of source A and rank 0 of source B
		// should have score = 1/(k+1) + 1/(k+1) = 2/(k+1)
		results := []*RetrievalResult{
			{NodeID: 1, Score: 1.0, Source: "semantic"},
			{NodeID: 2, Score: 0.5, Source: "semantic"},
			{NodeID: 1, Score: 1.0, Source: "keyword"},
		}
		merged := retriever.mergeWithRRF(results, k)
		var node1Score float32
		for _, r := range merged {
			if r.NodeID == 1 {
				node1Score = r.Score
			}
		}
		expected := float32(2.0) / float32(k+1)
		if node1Score != expected {
			t.Errorf("RRF score for node 1 = %f, want %f", node1Score, expected)
		}
	})
}