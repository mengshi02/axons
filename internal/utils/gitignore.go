// Package utils provides common utility functions used across the application.
package utils

import (
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// GitIgnoreMatcher wraps go-git/v5's gitignore matcher to provide
// path-based matching against a project's .gitignore rules.
// It supports nested .gitignore files and negation patterns (e.g. "!path/").
type GitIgnoreMatcher struct {
	rootDir string
	matcher gitignore.Matcher
}

// NewGitIgnoreMatcher creates a matcher by loading .gitignore from rootDir
// and recursively scanning for nested .gitignore files in subdirectories.
// If no .gitignore exists at root, returns a matcher that never ignores.
func NewGitIgnoreMatcher(rootDir string) *GitIgnoreMatcher {
	m := &GitIgnoreMatcher{
		rootDir: filepath.Clean(rootDir),
	}
	patterns := m.loadPatterns(rootDir)
	m.matcher = gitignore.NewMatcher(patterns)
	return m
}

// loadPatterns reads .gitignore from rootDir and recursively from subdirectories,
// using go-git/v5's ReadPatterns approach but with os filesystem directly
// (avoiding billy.Filesystem dependency for simplicity).
func (m *GitIgnoreMatcher) loadPatterns(rootDir string) []gitignore.Pattern {
	var patterns []gitignore.Pattern

	// Load root .gitignore
	patterns = append(patterns, m.readGitIgnoreFile(rootDir, "")...)

	// Recursively load nested .gitignore files
	m.loadNestedPatterns(rootDir, "", &patterns)

	return patterns
}

// readGitIgnoreFile reads a single .gitignore file and returns parsed patterns.
// domain is the relative path prefix for the patterns (e.g. "apps/desk").
func (m *GitIgnoreMatcher) readGitIgnoreFile(dir string, domain string) []gitignore.Pattern {
	gitignorePath := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		return nil
	}

	var domainParts []string
	if domain != "" {
		domainParts = strings.Split(domain, "/")
	}

	var patterns []gitignore.Pattern
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimRight(line, " ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, gitignore.ParsePattern(line, domainParts))
	}
	return patterns
}

// loadNestedPatterns recursively walks subdirectories to find .gitignore files.
// It respects the parent gitignore rules — directories already ignored are skipped.
func (m *GitIgnoreMatcher) loadNestedPatterns(dir string, relDir string, patterns *[]gitignore.Pattern) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Build a temporary matcher from current patterns to skip ignored dirs
	tempMatcher := gitignore.NewMatcher(*patterns)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Skip well-known non-project directories early
		if name == ".git" || name == ".svn" || name == ".hg" {
			continue
		}

		childRel := name
		if relDir != "" {
			childRel = relDir + "/" + name
		}

		// Check if this directory is already ignored by existing patterns
		pathParts := strings.Split(childRel, "/")
		if tempMatcher.Match(pathParts, true) {
			continue
		}

		childAbs := filepath.Join(dir, name)

		// Load .gitignore from this subdirectory
		subPatterns := m.readGitIgnoreFile(childAbs, childRel)
		if len(subPatterns) > 0 {
			*patterns = append(*patterns, subPatterns...)
		}

		// Recurse into subdirectory
		m.loadNestedPatterns(childAbs, childRel, patterns)
	}
}

// Match checks whether an absolute path should be ignored according to
// the loaded .gitignore rules. Returns true if the path should be ignored.
//
// isDir should be true when checking a directory path.
func (m *GitIgnoreMatcher) Match(absPath string, isDir bool) bool {
	if m.matcher == nil {
		return false
	}

	relPath, err := filepath.Rel(m.rootDir, absPath)
	if err != nil {
		relPath = absPath
	}

	// Normalize to forward slashes and split into path segments
	relPath = filepath.ToSlash(relPath)
	if relPath == "." {
		return false
	}

	pathParts := strings.Split(relPath, "/")
	return m.matcher.Match(pathParts, isDir)
}
