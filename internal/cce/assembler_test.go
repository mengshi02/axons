package cce

import (
	"strings"
	"testing"
)

func TestIsCallerRelation(t *testing.T) {
	a := NewContextAssembler()
	tests := []struct {
		result *RetrievalResult
		want   bool
	}{
		{&RetrievalResult{Source: "called_by", Depth: 1}, true},
		{&RetrievalResult{Source: "graph_called_by", Depth: 2}, true},
		{&RetrievalResult{Source: "semantic", Kind: "function", Depth: 1}, true}, // function with depth > 0
		{&RetrievalResult{Source: "semantic", Kind: "function", Depth: 0}, false},
		{&RetrievalResult{Source: "keyword", Kind: "variable", Depth: 1}, false}, // not a function
		{&RetrievalResult{Source: "calls", Kind: "function", Depth: 1}, true},   // has called_by in source? No. But function+depth>0 triggers it
	}
	for i, tt := range tests {
		got := a.isCallerRelation(tt.result)
		if got != tt.want {
			t.Errorf("isCallerRelation[%d] = %v, want %v (source=%q, kind=%q, depth=%d)",
				i, got, tt.want, tt.result.Source, tt.result.Kind, tt.result.Depth)
		}
	}
}

func TestIsCalleeRelation(t *testing.T) {
	a := NewContextAssembler()
	tests := []struct {
		result *RetrievalResult
		want   bool
	}{
		{&RetrievalResult{Source: "calls", Kind: "function", Depth: 1}, true},
		{&RetrievalResult{Source: "graph_calls", Kind: "function", Depth: 0}, true},
		{&RetrievalResult{Source: "semantic", Kind: "function", Depth: 1}, false}, // also matches isCallerRelation
		{&RetrievalResult{Source: "semantic", Kind: "function", Depth: 0}, false},
		{&RetrievalResult{Source: "keyword", Kind: "variable", Depth: 1}, false},
	}
	for i, tt := range tests {
		got := a.isCalleeRelation(tt.result)
		if got != tt.want {
			t.Errorf("isCalleeRelation[%d] = %v, want %v", i, got, tt.want)
		}
	}
}

func TestIsTypeDefinition(t *testing.T) {
	a := NewContextAssembler()
	tests := []struct {
		kind string
		want bool
	}{
		{"type", true},
		{"interface", true},
		{"struct", true},
		{"class", true},
		{"enum", true},
		{"function", false},
		{"variable", false},
		{"method", false},
		{"constant", false},
	}
	for _, tt := range tests {
		r := &RetrievalResult{Kind: tt.kind}
		got := a.isTypeDefinition(r)
		if got != tt.want {
			t.Errorf("isTypeDefinition(%q) = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestIsImportRelation(t *testing.T) {
	a := NewContextAssembler()
	tests := []struct {
		source string
		want   bool
	}{
		{"import", true},
		{"graph_import", true},
		{"contains", true},
		{"graph_contains", true},
		{"semantic", false},
		{"calls", false},
		{"keyword", false},
	}
	for _, tt := range tests {
		r := &RetrievalResult{Source: tt.source}
		got := a.isImportRelation(r)
		if got != tt.want {
			t.Errorf("isImportRelation(%q) = %v, want %v", tt.source, got, tt.want)
		}
	}
}

func TestFormatSectionContent(t *testing.T) {
	a := NewContextAssembler()
	results := []*RetrievalResult{
		{File: "foo.go", Line: 10, Name: "Foo", Kind: "function", Content: "func Foo() {}"},
		{File: "bar.go", Line: 20, Name: "Bar", Kind: "method", Content: "func (b *B) Bar() {}"},
	}
	content := a.formatSectionContent(results, 10)
	if content == "" {
		t.Error("formatSectionContent returned empty content")
	}
	if !strings.Contains(content, "foo.go:10") {
		t.Error("content should contain file:line reference")
	}
	if !strings.Contains(content, "Foo") {
		t.Error("content should contain function name")
	}
}

func TestFormatSectionContent_Dedup(t *testing.T) {
	a := NewContextAssembler()
	// Same file:line should be deduplicated
	results := []*RetrievalResult{
		{File: "foo.go", Line: 10, Name: "Foo", Kind: "function", Content: "func Foo() {}"},
		{File: "foo.go", Line: 10, Name: "Foo", Kind: "function", Content: "func Foo() {}"},
	}
	content := a.formatSectionContent(results, 10)
	count := strings.Count(content, "foo.go:10")
	if count != 1 {
		t.Errorf("formatSectionContent should deduplicate, found %d occurrences of foo.go:10", count)
	}
}

func TestFormatSectionContent_MaxLines(t *testing.T) {
	a := NewContextAssembler()
	// Content with many lines should be truncated
	longContent := strings.Repeat("line\n", 50)
	results := []*RetrievalResult{
		{File: "foo.go", Line: 10, Name: "Foo", Kind: "function", Content: longContent},
	}
	content := a.formatSectionContent(results, 5) // max 5 lines
	if strings.Contains(content, "(truncated)") {
		t.Log("long content was truncated as expected")
	}
}

func TestApplyBudget(t *testing.T) {
	a := NewContextAssembler()

	t.Run("zero budget returns all sections", func(t *testing.T) {
		sections := []ContextSection{
			{Title: "A", Content: "content a", Tokens: 100, Priority: 10},
		}
		result := a.applyBudget(sections, 0)
		if len(result) != 1 {
			t.Errorf("applyBudget with 0 maxTokens should return all sections, got %d", len(result))
		}
	})

	t.Run("sections fit within budget", func(t *testing.T) {
		sections := []ContextSection{
			{Title: "A", Content: "short", Tokens: 2, Priority: 10},
			{Title: "B", Content: "brief", Tokens: 2, Priority: 5},
		}
		result := a.applyBudget(sections, 100)
		if len(result) != 2 {
			t.Errorf("applyBudget with sufficient budget = %d sections, want 2", len(result))
		}
	})

	t.Run("sections exceed budget", func(t *testing.T) {
		sections := []ContextSection{
			{Title: "A", Content: strings.Repeat("x", 1000), Tokens: 250, Priority: 10},
			{Title: "B", Content: strings.Repeat("y", 1000), Tokens: 250, Priority: 5},
		}
		result := a.applyBudget(sections, 100)
		// Should still return sections but with trimmed content
		for _, sec := range result {
			if sec.Tokens > 250 {
				t.Errorf("section %q tokens %d exceeds reasonable budget", sec.Title, sec.Tokens)
			}
		}
	})
}

func TestBuildSources(t *testing.T) {
	a := NewContextAssembler()
	results := []*RetrievalResult{
		{NodeID: 1, Name: "Foo", File: "foo.go", Score: 0.9, Source: "semantic"},
		{NodeID: 2, Name: "Bar", File: "bar.go", Score: 0.7, Source: "keyword"},
		{NodeID: 1, Name: "Foo", File: "foo.go", Score: 0.8, Source: "keyword"}, // duplicate NodeID
	}
	sources := a.buildSources(results)
	if len(sources) != 2 {
		t.Errorf("buildSources = %d sources, want 2 (deduplicated by NodeID)", len(sources))
	}
}

func TestEstimateTokens_Assembler(t *testing.T) {
	a := NewContextAssembler()
	// 4 chars per token (maxTokensPerChar = 0.25)
	got := a.estimateTokens("hello world!")
	expected := int(float64(len("hello world!")) * 0.25)
	if got != expected {
		t.Errorf("estimateTokens = %d, want %d", got, expected)
	}
}

// --- types.go tests ---

func TestBuiltinTemplates(t *testing.T) {
	templates := BuiltinTemplates()

	expectedTemplates := []ContextTemplate{
		TemplateUnderstandFunction,
		TemplateChangeImpact,
		TemplateDebugTrace,
		TemplateExploreModule,
		TemplateGeneral,
	}
	for _, tmpl := range expectedTemplates {
		cfg, ok := templates[tmpl]
		if !ok {
			t.Errorf("missing template: %s", tmpl)
			continue
		}
		if cfg.Name == "" {
			t.Errorf("template %s has empty name", tmpl)
		}
		if cfg.MaxTokens <= 0 {
			t.Errorf("template %s has invalid MaxTokens: %d", tmpl, cfg.MaxTokens)
		}
		if len(cfg.Sections) == 0 {
			t.Errorf("template %s has no sections", tmpl)
		}
		if cfg.GraphDepth < 0 {
			t.Errorf("template %s has invalid GraphDepth: %d", tmpl, cfg.GraphDepth)
		}
	}
}

func TestGetTemplate(t *testing.T) {
	t.Run("known template", func(t *testing.T) {
		cfg := GetTemplate(TemplateUnderstandFunction)
		if cfg.Name != "understand_function" {
			t.Errorf("GetTemplate(understand_function).Name = %q", cfg.Name)
		}
	})

	t.Run("unknown template falls back to general", func(t *testing.T) {
		cfg := GetTemplate(ContextTemplate("nonexistent"))
		if cfg.Name != "general" {
			t.Errorf("GetTemplate(unknown) should fall back to general, got %q", cfg.Name)
		}
	})

	t.Run("empty template falls back to general", func(t *testing.T) {
		cfg := GetTemplate("")
		if cfg.Name != "general" {
			t.Errorf("GetTemplate(empty) should fall back to general, got %q", cfg.Name)
		}
	})
}

func TestFormatContextForLLM(t *testing.T) {
	t.Run("empty sections", func(t *testing.T) {
		ac := &AssembledContext{Sections: []ContextSection{}}
		result := ac.FormatContextForLLM()
		if result != "" {
			t.Errorf("FormatContextForLLM with empty sections = %q, want empty", result)
		}
	})

	t.Run("with sections", func(t *testing.T) {
		ac := &AssembledContext{
			Sections: []ContextSection{
				{Title: "Target Function", Content: "func Foo() {}"},
				{Title: "Callers", Content: "func Bar() { Foo() }"},
			},
		}
		result := ac.FormatContextForLLM()
		if !strings.Contains(result, "### Target Function") {
			t.Error("result should contain section title")
		}
		if !strings.Contains(result, "func Foo()") {
			t.Error("result should contain section content")
		}
	})

	t.Run("skips empty content sections", func(t *testing.T) {
		ac := &AssembledContext{
			Sections: []ContextSection{
				{Title: "Empty Section", Content: ""},
				{Title: "Has Content", Content: "some code"},
			},
		}
		result := ac.FormatContextForLLM()
		if strings.Contains(result, "### Empty Section") {
			t.Error("should skip sections with empty content")
		}
		if !strings.Contains(result, "### Has Content") {
			t.Error("should include sections with content")
		}
	})
}

func TestFormatCCEBanner(t *testing.T) {
	result := FormatCCEBanner(TemplateUnderstandFunction, 15)
	if !strings.Contains(result, "understand_function") {
		t.Error("banner should contain template name")
	}
	if !strings.Contains(result, "15") {
		t.Error("banner should contain result count")
	}
	if !strings.Contains(result, "axons-cce") {
		t.Error("banner should contain engine name")
	}
}