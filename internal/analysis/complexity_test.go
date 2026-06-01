// Package analysis provides code analysis capabilities.
package analysis

import (
	"testing"
)

func TestNewComplexityAnalyzer(t *testing.T) {
	analyzer := NewComplexityAnalyzer()
	if analyzer == nil {
		t.Fatal("NewComplexityAnalyzer() returned nil")
	}
}

func TestComplexityAnalyze_Go(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	tests := []struct {
		name           string
		sourceCode     string
		language       string
		minLOC         int // minimum expected lines of code
		wantCyclomatic int
		wantNesting    int
	}{
		{
			name: "simple function",
			sourceCode: `func add(a, b int) int {
	return a + b
}`,
			language:       "go",
			minLOC:         1,
			wantCyclomatic: 1,
			wantNesting:    0,
		},
		{
			name: "function with if statement",
			sourceCode: `func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}`,
			language:       "go",
			minLOC:         3,
			wantCyclomatic: 2,
			wantNesting:    1,
		},
		{
			name: "function with nested conditions",
			sourceCode: `func classify(x int) string {
	if x > 0 {
		if x > 10 {
			if x > 100 {
				return "large"
			}
			return "medium"
		}
		return "small"
	}
	return "zero or negative"
}`,
			language:       "go",
			minLOC:         5,
			wantCyclomatic: 4,
			wantNesting:    3,
		},
		{
			name: "function with for loop",
			sourceCode: `func sum(nums []int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}`,
			language:       "go",
			minLOC:         4,
			wantCyclomatic: 2,
		},
		{
			name: "function with switch",
			sourceCode: `func dayName(day int) string {
	switch day {
	case 1:
		return "Monday"
	case 2:
		return "Tuesday"
	default:
		return "Other"
	}
}`,
			language:       "go",
			minLOC:         5,
			wantCyclomatic: 3,
		},
		{
			name: "function with multiple conditions",
			sourceCode: `func classifyAge(age int) string {
	if age < 0 {
		return "invalid"
	} else if age < 18 {
		return "minor"
	} else if age < 65 {
		return "adult"
	} else {
		return "senior"
	}
}`,
			language:       "go",
			minLOC:         5,
			wantCyclomatic: 4,
		},
		{
			name: "empty function",
			sourceCode: `func empty() {
}`,
			language:       "go",
			minLOC:         1,
			wantCyclomatic: 1,
		},
		{
			name: "function with boolean operators",
			sourceCode: `func isValid(x, y int) bool {
	if x > 0 && y > 0 || x < 0 && y < 0 {
		return true
	}
	return false
}`,
			language:       "go",
			minLOC:         4,
			wantCyclomatic: 4, // if + && + ||
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := analyzer.Analyze(tt.sourceCode, tt.language)

			if metrics == nil {
				t.Fatal("Analyze() returned nil")
			}

			if metrics.LinesOfCode < tt.minLOC {
				t.Errorf("LinesOfCode = %d, want at least %d", metrics.LinesOfCode, tt.minLOC)
			}

			if tt.wantCyclomatic > 0 && metrics.Cyclomatic < tt.wantCyclomatic {
				t.Errorf("Cyclomatic = %d, want at least %d", metrics.Cyclomatic, tt.wantCyclomatic)
			}

			if tt.wantNesting > 0 && metrics.Nesting < tt.wantNesting {
				t.Errorf("Nesting = %d, want at least %d", metrics.Nesting, tt.wantNesting)
			}
		})
	}
}

func TestComplexityAnalyze_JavaScript(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	tests := []struct {
		name       string
		sourceCode string
		language   string
	}{
		{
			name: "simple arrow function",
			sourceCode: `const add = (a, b) => {
	return a + b;
}`,
			language: "javascript",
		},
		{
			name: "function with for loop",
			sourceCode: `function sum(arr) {
	let total = 0;
	for (let i = 0; i < arr.length; i++) {
		total += arr[i];
	}
	return total;
}`,
			language: "javascript",
		},
		{
			name: "function with try-catch",
			sourceCode: `function safeParse(json) {
	try {
		return JSON.parse(json);
	} catch (e) {
		return null;
	}
}`,
			language: "javascript",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := analyzer.Analyze(tt.sourceCode, tt.language)

			if metrics == nil {
				t.Fatal("Analyze() returned nil")
			}

			if metrics.LinesOfCode <= 0 {
				t.Errorf("LinesOfCode should be positive, got %d", metrics.LinesOfCode)
			}
		})
	}
}

func TestComplexityAnalyze_Python(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	tests := []struct {
		name       string
		sourceCode string
		language   string
	}{
		{
			name: "simple function",
			sourceCode: `def add(a, b):
	return a + b`,
			language: "python",
		},
		{
			name: "function with if-elif-else",
			sourceCode: `def classify(x):
	if x > 0:
		return "positive"
	elif x < 0:
		return "negative"
	else:
		return "zero"`,
			language: "python",
		},
		{
			name: "function with for loop",
			sourceCode: `def sum_list(nums):
	total = 0
	for n in nums:
		total += n
	return total`,
			language: "python",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := analyzer.Analyze(tt.sourceCode, tt.language)

			if metrics == nil {
				t.Fatal("Analyze() returned nil")
			}

			if metrics.LinesOfCode <= 0 {
				t.Errorf("LinesOfCode should be positive, got %d", metrics.LinesOfCode)
			}
		})
	}
}

func TestCountLinesOfCode(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	tests := []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "empty string",
			source: "",
			want:   0,
		},
		{
			name:   "single line",
			source: "return 1",
			want:   1,
		},
		{
			name:   "multiple lines with empty",
			source: "line1\n\nline2\n\nline3",
			want:   3,
		},
		{
			name:   "lines with single line comments",
			source: "// comment\ncode\n// another\ncode2",
			want:   2, // only non-comment lines
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.countLinesOfCode(tt.source)
			if got != tt.want {
				t.Errorf("countLinesOfCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCyclomaticComplexity(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	tests := []struct {
		name   string
		source string
		want   int // minimum expected
	}{
		{
			name:   "no control flow",
			source: "return 1",
			want:   1,
		},
		{
			name:   "single if",
			source: "if x > 0 {\n\treturn x\n}",
			want:   2,
		},
		{
			name:   "if-else",
			source: "if x > 0 {\n\treturn 1\n} else {\n\treturn 2\n}",
			want:   2,
		},
		{
			name:   "multiple conditions with &&",
			source: "if a && b {\n\treturn true\n}",
			want:   3,
		},
		{
			name:   "multiple conditions with ||",
			source: "if a || b {\n\treturn true\n}",
			want:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.calculateCyclomatic(tt.source, "go")
			if got < tt.want {
				t.Errorf("calculateCyclomatic() = %d, want at least %d", got, tt.want)
			}
		})
	}
}

func TestNestingDepth(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	tests := []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "no nesting",
			source: "return 1",
			want:   0,
		},
		{
			name:   "single level",
			source: "if x > 0 {\n\treturn x\n}",
			want:   1,
		},
		{
			name:   "double nesting",
			source: "if x > 0 {\n\tif y > 0 {\n\t\treturn x + y\n\t}\n}",
			want:   2,
		},
		{
			name:   "triple nesting",
			source: "if a {\n\tif b {\n\t\tif c {\n\t\t\treturn 1\n\t\t}\n\t}\n}",
			want:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzer.calculateNesting(tt.source, "go")
			if got < tt.want {
				t.Errorf("calculateNesting() = %d, want at least %d", got, tt.want)
			}
		})
	}
}

func TestHalsteadMetrics(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	source := `func add(a, b int) int {
	return a + b
}`

	metrics := &ComplexityMetrics{}
	analyzer.calculateHalstead(source, "go", metrics)

	// Basic checks - Halstead metrics should be calculated
	if metrics.TotalOperators < 0 {
		t.Error("TotalOperators should not be negative")
	}
	if metrics.TotalOperands < 0 {
		t.Error("TotalOperands should not be negative")
	}
	if metrics.HalsteadVolume < 0 {
		t.Error("HalsteadVolume should not be negative")
	}
}

func TestGetOperators(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	tests := []struct {
		language      string
		wantOperators int // minimum number of operators
	}{
		{"go", 10},
		{"javascript", 10},
		{"python", 5},
		{"java", 10},
		{"unknown", 5},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			operators := analyzer.getOperators(tt.language)
			if len(operators) < tt.wantOperators {
				t.Errorf("getOperators(%q) returned %d operators, want at least %d",
					tt.language, len(operators), tt.wantOperators)
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	analyzer := NewComplexityAnalyzer()

	tests := []struct {
		name   string
		source string
		want   int // minimum number of tokens
	}{
		{"simple", "a + b", 3},
		{"assignment", "x := 10", 3},
		{"function call", "fmt.Println(x)", 4},
		{"comparison", "x > 0 && y < 10", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := analyzer.tokenize(tt.source)
			if len(tokens) < tt.want {
				t.Errorf("tokenize() returned %d tokens, want at least %d", len(tokens), tt.want)
			}
		})
	}
}