// Package stages provides pipeline stages for graph building.
package stages

import (
	"path/filepath"
	"strings"

	"github.com/mengshi02/axons/internal/utils"
)

// ToRelativePath converts an absolute file path to a path relative to the root directory.
// Delegates to utils.ToRelativePath.
func ToRelativePath(filePath, rootDir string) string {
	return utils.ToRelativePath(filePath, rootDir)
}

// ComputeHash computes a SHA256 hash of content.
// Delegates to utils.ComputeSHA256.
func ComputeHash(content []byte) string {
	return utils.ComputeSHA256(content)
}

// importExtensions are extensions to try during import resolution.
var importExtensions = []string{
	"", // exact match
	// TypeScript/JavaScript
	".ts", ".tsx", ".js", ".jsx", "/index.ts", "/index.tsx", "/index.js", "/index.jsx",
	// Python
	".py", "/__init__.py",
	// Go
	".go",
	// Rust
	".rs", "/mod.rs",
	// Java/Kotlin
	".java", ".kt", ".kts",
	// C/C++
	".c", ".h", ".cpp", ".hpp", ".cc", ".cxx", ".hxx", ".hh",
	// C#
	".cs",
	// PHP
	".php", ".phtml",
	// Swift
	".swift",
	// Ruby
	".rb",
}

// TryResolveWithExtensions tries to resolve a base path with various file extensions.
func TryResolveWithExtensions(idx *SuffixIndex, basePath string) string {
	normalized := filepath.ToSlash(basePath)
	for _, ext := range importExtensions {
		candidate := normalized + ext
		if path, ok := idx.ExactMap[candidate]; ok {
			return path
		}
		if path, ok := idx.LowerMap[strings.ToLower(candidate)]; ok {
			return path
		}
	}
	return ""
}

// SuffixResolve tries to find a file by matching path suffixes.
func SuffixResolve(idx *SuffixIndex, pathParts []string) string {
	for i := 0; i < len(pathParts); i++ {
		suffix := strings.Join(pathParts[i:], "/")
		for _, ext := range importExtensions {
			suffixWithExt := suffix + ext
			if path, ok := idx.ExactMap[suffixWithExt]; ok {
				return path
			}
			if path, ok := idx.LowerMap[strings.ToLower(suffixWithExt)]; ok {
				return path
			}
		}
	}
	return ""
}