// Package utils provides common utility functions used across the application.
package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultIgnorePatterns is the list of common directory/file patterns to ignore during file scanning.
var DefaultIgnorePatterns = []string{
	"node_modules",
	"vendor",
	".git",
	".svn",
	"dist",
	"build",
	"target",
	"bin",
	"__pycache__",
	".pytest_cache",
	".mypy_cache",
	"*.min.js",
	"*.min.css",
}

// DefaultIgnoreDirs is the list of common directory names to ignore when watching files.
var DefaultIgnoreDirs = []string{
	"node_modules", "vendor", ".git", ".svn", ".hg",
	"dist", "build", "target", "bin", "out",
	"__pycache__", ".pytest_cache", ".mypy_cache",
	".idea", ".vscode", ".axons",
}

// decoyDirSuffixes matches directory names that are typically "decoy" directories —
// they exist inside a project but are not source code that should be parsed for a code graph.
// These contain bundled runtimes, pre-compiled binaries, test data, or generated artifacts.
var decoyDirSuffixes = []string{
	// Portable/embedded runtimes
	"-portable",
	"-runtime",
	// Pre-built binaries and packages
	"prebuild_",
	"prebuilt-",
	"packbuild",
	// Data and dataset directories (not source code)
	"-datasets",
	"-data",
	"-assets",
	"-resources",
	// Downloaded/cloned third-party repos
	"_repos",
	"-repos",
	"downloaded",
}

// decoyDirExactNames matches exact directory names that are decoy directories.
var decoyDirExactNames = map[string]bool{
	// Portable runtimes
	"python-portable": true,
	"node-portable":   true,
	// Generated code
	"quickapp-generated": true,
	// Data/knowledge files (not code)
	"knowledgefiles": true,
	// Eval and test data
	"eval-datasets": true,
	"eval-logs":     true,
	// Pre-built binaries
	"prebuild_binaries": true,
	// Downloaded/cloned third-party repos and packages
	"external_repos":       true,
	"awesome-skills-downloaded": true,
}

// IsDecoyDir uses heuristic rules to determine if a directory is a "decoy" —
// a directory that exists inside the project tree but does not contain source code
// worth parsing. Examples: python-portable (3G runtime), eval-datasets, node-portable.
//
// This complements .gitignore-based filtering. When a directory is not excluded by
// .gitignore but still shouldn't be parsed, these heuristics catch it.
func IsDecoyDir(dirPath string) bool {
	name := filepath.Base(dirPath)
	nameLower := strings.ToLower(name)

	// Check exact name matches
	if decoyDirExactNames[nameLower] {
		return true
	}

	// Check suffix/prefix patterns
	for _, suffix := range decoyDirSuffixes {
		if strings.Contains(nameLower, suffix) {
			return true
		}
	}

	// Platform-specific subdirectory under a portable/native root:
	// e.g. python-portable/mac, python-portable/linux, node-portable/windows
	parent := filepath.Base(filepath.Dir(dirPath))
	parentLower := strings.ToLower(parent)
	if isPlatformDir(nameLower) && (strings.Contains(parentLower, "portable") || strings.Contains(parentLower, "native")) {
		return true
	}

	return false
}

// isPlatformDir returns true if the directory name is a platform identifier
// (mac, linux, windows, win32, darwin, x64, arm64) — these are almost never
// source code directories.
func isPlatformDir(name string) bool {
	switch name {
	case "mac", "macos", "linux", "windows", "win32", "darwin",
		"x64", "x86", "arm64", "amd64", "ia32":
		return true
	}
	return false
}

// IsBinaryHeavyDir checks if a directory contains mostly binary/non-source files.
// Returns true if the ratio of binary files exceeds the given threshold (0.0-1.0).
// Binary files are identified by extension (.pyc, .so, .dll, .dylib, .exe, .o, .pyd, etc.).
//
// This is a more expensive check (reads directory listing) and should be used as a
// fallback when other heuristics don't match.
func IsBinaryHeavyDir(dirPath string, threshold float64) bool {
	// Binary file extensions that indicate non-source content
	binaryExts := map[string]bool{
		".pyc": true, ".pyd": true, ".pyo": true,
		".so": true, ".dll": true, ".dylib": true, ".a": true, ".lib": true,
		".o": true, ".obj": true,
		".exe": true, ".bin": true,
		".wasm": true,
		".pak": true, ".nupkg": true,
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil || len(entries) == 0 {
		return false
	}

	var binary, total int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		total++
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if binaryExts[ext] {
			binary++
		}
	}

	if total == 0 {
		return false
	}

	ratio := float64(binary) / float64(total)
	return ratio >= threshold
}

// ShouldIgnorePath checks if a file path should be ignored based on common ignore patterns.
// It ignores hidden files (containing "/.") and common build/dependency directories.
func ShouldIgnorePath(path string) bool {
	// Ignore hidden files
	if strings.Contains(path, "/.") {
		return true
	}

	for _, pattern := range DefaultIgnorePatterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}

	return false
}