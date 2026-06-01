// Package utils provides common utility functions used across the application.
package utils

import (
	"path/filepath"
	"strings"
)

// ToRelativePath converts an absolute file path to a path relative to the root directory.
func ToRelativePath(filePath, rootDir string) string {
	if rootDir == "" {
		return filePath
	}

	root := filepath.Clean(rootDir)
	absPath := filepath.Clean(filePath)

	if !filepath.IsAbs(absPath) {
		return absPath
	}

	if strings.HasPrefix(absPath, root+string(filepath.Separator)) {
		return strings.TrimPrefix(absPath, root+string(filepath.Separator))
	}

	if absPath == root {
		return ""
	}

	return filePath
}

// FileExtension extracts the file extension from a file path (e.g. ".go", ".py").
func FileExtension(filePath string) string {
	for i := len(filePath) - 1; i >= 0; i-- {
		if filePath[i] == '.' {
			return filePath[i:]
		}
		if filePath[i] == '/' || filePath[i] == '\\' {
			break
		}
	}
	return ""
}