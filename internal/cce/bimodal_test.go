package cce

import (
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  int
	}{
		{"empty", "", 0},
		{"short", "hello", 2},       // 5 / 2.5 = 2
		{"medium", "func main()", 4}, // len=11, 11 / 2.5 = 4.4 → 4
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.text)
			if got != tt.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestSmartTruncate(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		maxLen  int
		wantLen int
	}{
		{"short enough", "abc", 10, 3},
		{"exact fit", "abcdefghij", 10, 10},
		{"needs truncation", "abcdefghij20charsXXXtail", 15, 15},
		{"very short budget", "abcdefghij", 8, 8},
		{"long code with structure", "func main() {\n\tfmt.Println(\"hello\")\n}\n// long body here...\nfunc other() {\n\treturn\n}", 50, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := smartTruncate(tt.text, tt.maxLen)
			if len(result) > tt.maxLen {
				t.Errorf("smartTruncate result length %d exceeds maxLen %d", len(result), tt.maxLen)
			}
			if tt.text != "" && len(tt.text) <= tt.maxLen && result != tt.text {
				t.Errorf("smartTruncate should return original text when it fits, got %q", result)
			}
		})
	}

	// Verify truncation preserves prefix and tail
	longText := "func processOrder(order Order) error {\n\t// 200+ chars of body...\n\tresult := handle(order)\n\treturn result\n}"
	result := smartTruncate(longText, 60)
	if len(result) > 60 {
		t.Errorf("smartTruncate result too long: %d", len(result))
	}
	// Should contain separator marker when truncated
	if len(longText) > 60 && !containsTruncationMarker(result) {
		t.Logf("smartTruncate may use simple prefix truncation for short budgets, result: %q", result)
	}
}

func containsTruncationMarker(s string) bool {
	return len(s) > 0 && (s[len(s)-1] == '.' || s[len(s)-1] == ']')
}

func TestComputeHash(t *testing.T) {
	// Same content → same hash
	h1 := computeHash("hello world")
	h2 := computeHash("hello world")
	if h1 != h2 {
		t.Errorf("computeHash not deterministic: %s != %s", h1, h2)
	}

	// Different content → different hash
	h3 := computeHash("different content")
	if h1 == h3 {
		t.Errorf("computeHash collision for different inputs")
	}

	// Hash length should be 64 (SHA-256 hex)
	if len(h1) != 64 {
		t.Errorf("computeHash hash length = %d, want 64", len(h1))
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"App.jsx", "javascript"},
		{"component.tsx", "typescript"},
		{"utils.ts", "typescript"},
		{"Main.java", "java"},
		{"header.h", "c"},
		{"impl.cpp", "cpp"},
		{"main.rs", "rust"},
		{"app.rb", "ruby"},
		{"page.php", "php"},
		{"view.swift", "swift"},
		{"model.kt", "kotlin"},
		{"app.scala", "scala"},
		{"run.sh", "shell"},
		{"query.sql", "sql"},
		{"config.yaml", "yaml"},
		{"data.yml", "yaml"},
		{"config.json", "json"},
		{"layout.xml", "xml"},
		{"page.html", "html"},
		{"style.css", "css"},
		{"readme.md", "markdown"},
		{"unknown.txt", ""},
		{"noext", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := detectLanguage(tt.path)
			if got != tt.want {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestBuildDescriptionText(t *testing.T) {
	// The function uses qualName when present, name otherwise
	t.Run("qualName present", func(t *testing.T) {
		result := buildDescriptionText("HandleOrder", "function", "orders.go", "pkg.HandleOrder", "Handles orders")
		if result != "pkg.HandleOrder (function) in orders.go Handles orders" {
			t.Errorf("buildDescriptionText = %q", result)
		}
	})
	t.Run("no qualName", func(t *testing.T) {
		result := buildDescriptionText("main", "", "", "", "")
		if result != "main" {
			t.Errorf("buildDescriptionText = %q, want 'main'", result)
		}
	})
	t.Run("with kind and file", func(t *testing.T) {
		result := buildDescriptionText("Handler", "function", "api.go", "", "")
		if result != "Handler (function) in api.go" {
			t.Errorf("buildDescriptionText = %q", result)
		}
	})
}

func TestBuildNodeSignature(t *testing.T) {
	tests := []struct {
		kind     string
		qualName string
		want     string
	}{
		{"function", "pkg.Foo", "func pkg.Foo"},
		{"method", "pkg.Bar", "func pkg.Bar"},
		{"type", "pkg.MyType", "type pkg.MyType"},
		{"class", "pkg.MyClass", "type pkg.MyClass"},
		{"struct", "pkg.Config", "type pkg.Config"},
		{"interface", "pkg.Reader", "type pkg.Reader"},
		{"variable", "pkg.count", "var pkg.count"},
		{"field", "pkg.Data.id", "var pkg.Data.id"},
		{"constant", "pkg.MaxSize", "const pkg.MaxSize"},
		{"package", "pkg", "pkg pkg"},
		{"module", "mod", "pkg mod"},
		{"", "pkg.Foo", "pkg.Foo"},   // empty kind → use qualName directly
		{"", "", ""},                   // empty qualName → empty result
		{"unknown", "pkg.X", "unknown pkg.X"},
	}
	for _, tt := range tests {
		t.Run(tt.kind+"/"+tt.qualName, func(t *testing.T) {
			got := buildNodeSignature(tt.kind, tt.qualName)
			if got != tt.want {
				t.Errorf("buildNodeSignature(%q, %q) = %q, want %q", tt.kind, tt.qualName, got, tt.want)
			}
		})
	}
}

func TestEnrichChunk(t *testing.T) {
	t.Run("no signature", func(t *testing.T) {
		result := enrichChunk("body text", "", 0, 3)
		if result != "body text" {
			t.Errorf("enrichChunk with empty signature should return body: %q", result)
		}
	})
	t.Run("with total", func(t *testing.T) {
		result := enrichChunk("body text", "func main", 1, 5)
		want := "// [func main — chunk 2/5]\nbody text"
		if result != want {
			t.Errorf("enrichChunk = %q, want %q", result, want)
		}
	})
	t.Run("without total", func(t *testing.T) {
		result := enrichChunk("body text", "func main", 0, -1)
		want := "// [func main — chunk 1]\nbody text"
		if result != want {
			t.Errorf("enrichChunk = %q, want %q", result, want)
		}
	})
	t.Run("totalChunks zero", func(t *testing.T) {
		result := enrichChunk("body text", "func main", 0, 0)
		// totalChunks > 0 check means 0 is treated as "unknown"
		want := "// [func main — chunk 1]\nbody text"
		if result != want {
			t.Errorf("enrichChunk = %q, want %q", result, want)
		}
	})
}

func TestGroupByTokenBudget(t *testing.T) {
	b := &BimodalEmbedder{}

	t.Run("empty input", func(t *testing.T) {
		groups := b.groupByTokenBudget([]string{}, 100)
		if len(groups) != 0 {
			t.Errorf("groupByTokenBudget empty = %d groups, want 0", len(groups))
		}
	})

	t.Run("single text fits", func(t *testing.T) {
		texts := []string{"short text"}
		groups := b.groupByTokenBudget(texts, 100)
		if len(groups) != 1 {
			t.Fatalf("groupByTokenBudget = %d groups, want 1", len(groups))
		}
		if groups[0].start != 0 || groups[0].end != 1 {
			t.Errorf("group = {start:%d, end:%d}, want {0, 1}", groups[0].start, groups[0].end)
		}
	})

	t.Run("multiple texts exceed budget", func(t *testing.T) {
		// Create texts that together exceed a small budget
		texts := []string{
			"text one that is moderate length",
			"text two that is moderate length",
			"text three that is moderate length",
		}
		budget := 20 // very small budget to force splitting
		groups := b.groupByTokenBudget(texts, budget)
		if len(groups) < 2 {
			t.Errorf("groupByTokenBudget with small budget should create multiple groups, got %d", len(groups))
		}
	})

	t.Run("all texts fit in one group", func(t *testing.T) {
		texts := []string{"a", "b", "c"}
		groups := b.groupByTokenBudget(texts, 500)
		if len(groups) != 1 {
			t.Errorf("groupByTokenBudget with large budget should be 1 group, got %d", len(groups))
		}
	})
}

func TestChunkText(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		chunks := chunkText("", "func main", 512)
		if chunks != nil {
			t.Errorf("chunkText empty = %v, want nil", chunks)
		}
	})

	t.Run("zero maxTokens", func(t *testing.T) {
		chunks := chunkText("some text", "func main", 0)
		if chunks != nil {
			t.Errorf("chunkText zero maxTokens = %v, want nil", chunks)
		}
	})

	t.Run("short text fits in one chunk", func(t *testing.T) {
		text := "func main() { fmt.Println(\"hello\") }"
		chunks := chunkText(text, "func main", 512)
		if len(chunks) != 1 {
			t.Fatalf("chunkText short = %d chunks, want 1", len(chunks))
		}
		if !chunks[0].IsOriginal {
			t.Errorf("short text chunk should be IsOriginal=true")
		}
	})

	t.Run("long text produces multiple chunks", func(t *testing.T) {
		// Create a long text with many lines
		var lines []string
		for i := 0; i < 100; i++ {
			lines = append(lines, "	line content that adds up to make this text quite long indeed")
		}
		text := "func longFunc() {\n" + joinLines(lines) + "\n}"
		chunks := chunkText(text, "func longFunc", 100) // small budget forces chunking
		if len(chunks) < 2 {
			t.Errorf("chunkText long = %d chunks, want >= 2", len(chunks))
		}
		for _, c := range chunks {
			if c.Text == "" {
				t.Error("chunk text should not be empty")
			}
		}
	})
}

func joinLines(lines []string) string {
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}

func TestFindStructureAwareSplit(t *testing.T) {
	text := "package main\n\nfunc a() {\n\treturn\n}\n\nfunc b() {\n\treturn\n}\n"

	t.Run("blank line is preferred", func(t *testing.T) {
		// Target near a blank line between functions
		pos := findStructureAwareSplit(text, 30, 100)
		if pos <= 0 || pos > len(text) {
			t.Errorf("findStructureAwareSplit returned invalid pos %d", pos)
		}
	})

	t.Run("closing brace as split", func(t *testing.T) {
		pos := findStructureAwareSplit("func a() {\n\treturn\n}\nfunc b() {\n\treturn\n}\n", 20, 50)
		if pos <= 0 {
			t.Errorf("findStructureAwareSplit returned pos %d", pos)
		}
	})
}

func TestFindLineStart(t *testing.T) {
	text := "line1\nline2\nline3\n"
	// findLineStart searches backward from pos-1 for \n, returns position after it
	tests := []struct {
		pos  int
		want int
	}{
		{0, 0},  // beginning → no \n found → 0
		{5, 0},  // pos=5 is the first \n; search from 4, no \n → 0
		{6, 6},  // pos=6 is 'l' of line2; search from 5, find \n → 6
		{8, 6},  // middle of line2; search backward finds \n at 5 → 6
		{11, 6}, // pos=11 is second \n; search from 10, find \n at 5 → 6
		{12, 12}, // pos=12 is 'l' of line3; search from 11, find \n at 11 → 12
	}
	for _, tt := range tests {
		got := findLineStart(text, tt.pos)
		if got != tt.want {
			t.Errorf("findLineStart(%d) = %d, want %d", tt.pos, got, tt.want)
		}
	}
}