// Package utils provides common utility functions used across the application.
package utils

import (
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// AxonsIgnoreMatcher wraps go-git/v5's gitignore matcher to provide
// path-based matching against a project's .axonsignore rules.
// Priority: .axonsignore > .gitignore > hardcoded blacklist.
// Supports negation patterns (e.g. "!vendor/my-lib/") to unignore
// directories that the hardcoded blacklist would skip.
type AxonsIgnoreMatcher struct {
	rootDir       string
	matcher       gitignore.Matcher
	negateMatcher gitignore.Matcher // matches only ! (negation/unignore) patterns
}

// NewAxonsIgnoreMatcher creates a matcher by loading .axonsignore from rootDir.
// If no .axonsignore exists, returns a matcher that never ignores.
func NewAxonsIgnoreMatcher(rootDir string) *AxonsIgnoreMatcher {
	m := &AxonsIgnoreMatcher{
		rootDir: filepath.Clean(rootDir),
	}
	patterns, negatePatterns := m.loadPatterns()
	m.matcher = gitignore.NewMatcher(patterns)
	m.negateMatcher = gitignore.NewMatcher(negatePatterns)
	return m
}

// loadPatterns reads .axonsignore from rootDir and splits into normal and negate patterns.
func (m *AxonsIgnoreMatcher) loadPatterns() (normal, negate []gitignore.Pattern) {
	return m.readIgnoreFile(m.rootDir, "")
}

// readIgnoreFile reads a single .axonsignore file and returns parsed patterns,
// split into normal patterns and negate (!) patterns.
func (m *AxonsIgnoreMatcher) readIgnoreFile(dir string, domain string) (normal, negate []gitignore.Pattern) {
	ignorePath := filepath.Join(dir, ".axonsignore")
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		return nil, nil
	}

	var domainParts []string
	if domain != "" {
		domainParts = strings.Split(domain, "/")
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pattern := gitignore.ParsePattern(line, domainParts)
		if strings.HasPrefix(line, "!") {
			// Negation pattern — strip the ! prefix for the negate-only matcher
			// so we can detect "unignore" separately from the combined matcher.
			strippedLine := strings.TrimPrefix(line, "!")
			negatePattern := gitignore.ParsePattern(strippedLine, domainParts)
			negate = append(negate, negatePattern)
		}
		normal = append(normal, pattern)
	}
	return normal, negate
}

// Match checks if a path should be ignored according to .axonsignore rules.
// isDir indicates whether the path is a directory.
func (m *AxonsIgnoreMatcher) Match(path string, isDir bool) bool {
	relPath, err := filepath.Rel(m.rootDir, path)
	if err != nil {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	pathParts := strings.Split(relPath, "/")
	return m.matcher.Match(pathParts, isDir)
}

// Unignore checks if a path is explicitly unignored by a ! negation pattern
// in .axonsignore. This allows users to override the hardcoded blacklist
// (e.g. "!vendor/" to unignore the vendor directory).
func (m *AxonsIgnoreMatcher) Unignore(path string, isDir bool) bool {
	if m.negateMatcher == nil {
		return false
	}
	relPath, err := filepath.Rel(m.rootDir, path)
	if err != nil {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	pathParts := strings.Split(relPath, "/")
	return m.negateMatcher.Match(pathParts, isDir)
}