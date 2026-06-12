package stages

import (
	"testing"
)

func TestMatchExcludeGlobs(t *testing.T) {
	tests := []struct {
		name     string
		rootDir  string
		path     string
		patterns []string
		expected bool
	}{
		{
			"no_patterns",
			"/project",
			"/project/foo.go",
			nil,
			false,
		},
		{
			"empty_patterns",
			"/project",
			"/project/foo.go",
			[]string{},
			false,
		},
		{
			"basename_match",
			"/project",
			"/project/foo.go",
			[]string{"foo.go"},
			true,
		},
		{
			"basename_no_match",
			"/project",
			"/project/bar.go",
			[]string{"foo.go"},
			false,
		},
		{
			"glob_star_match",
			"/project",
			"/project/foo.go",
			[]string{"*.go"},
			true,
		},
		{
			"glob_star_no_match",
			"/project",
			"/project/foo.ts",
			[]string{"*.go"},
			false,
		},
		{
			"pattern_with_slash",
			"/project",
			"/project/internal/foo.go",
			[]string{"internal/foo.go"},
			true,
		},
		{
			"pattern_with_slash_no_match",
			"/project",
			"/project/external/foo.go",
			[]string{"internal/foo.go"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchExcludeGlobs(tt.rootDir, tt.path, tt.patterns); got != tt.expected {
				t.Errorf("matchExcludeGlobs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMatchDoubleStar(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		expected bool
	}{
		{
			"no_double_star",
			"foo.go",
			"foo.go",
			false,
		},
		{
			"prefix_glob",
			"src/**/foo.go",
			"src/dir/foo.go",
			true,
		},
		{
			"prefix_glob_nested",
			"src/**/foo.go",
			"src/a/b/c/foo.go",
			true,
		},
		{
			"prefix_glob_no_match",
			"src/**/bar.go",
			"src/dir/foo.go",
			false,
		},
		{
			"prefix_only",
			"src/**",
			"src/dir/foo.go",
			true,
		},
		{
			"prefix_only_no_match",
			"lib/**",
			"src/dir/foo.go",
			false,
		},
		{
			"suffix_glob",
			"**/foo.go",
			"src/dir/foo.go",
			true,
		},
		{
			"suffix_glob_no_match",
			"**/bar.go",
			"src/dir/foo.go",
			false,
		},
		{
			"double_star_only",
			"**",
			"src/dir/foo.go",
			true,
		},
		{
			"prefix_mismatch",
			"lib/**/foo.go",
			"src/dir/foo.go",
			false,
		},
		{
			"multiple_double_star_uses_first",
			"src/**/dir/**/foo.go",
			"src/other/dir/sub/foo.go",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchDoubleStar(tt.pattern, tt.path); got != tt.expected {
				t.Errorf("matchDoubleStar() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSumLanguageStats(t *testing.T) {
	tests := []struct {
		name     string
		stats    map[string]int
		expected int
	}{
		{"nil", nil, 0},
		{"empty", map[string]int{}, 0},
		{"single", map[string]int{"Go": 5}, 5},
		{"multiple", map[string]int{"Go": 5, "Python": 3, "TypeScript": 2}, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sumLanguageStats(tt.stats); got != tt.expected {
				t.Errorf("sumLanguageStats() = %v, want %v", got, tt.expected)
			}
		})
	}
}