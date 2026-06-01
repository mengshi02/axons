// Package stages provides pipeline stages for graph building.
package stages

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/internal/utils"
	"go.uber.org/zap"
)

// CollectFiles collects all source files to process.
func CollectFiles(ctx *PipelineContext) error {
	start := time.Now()
	defer func() {
		ctx.RecordTiming("collect", time.Since(start))
	}()

	rootDir := ctx.Opts.RootDir
	if rootDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		rootDir = cwd
		ctx.Opts.RootDir = rootDir
	}

	// Set supported extensions for code files
	for _, lang := range ctx.Registry.ListLanguages() {
		for _, ext := range lang.Extensions {
			ctx.SupportedExts[ext] = true
		}
	}

	// ── Layer 1: Load .gitignore rules ──
	gitIgnoreMatcher := utils.NewGitIgnoreMatcher(rootDir)

	// ── Layer 0: Load .axonsignore rules (highest priority, supports ! negation) ──
	axonsIgnoreMatcher := utils.NewAxonsIgnoreMatcher(rootDir)

	// ── Layer 2: Hardcoded skip directories (fast path) ──
	skipDirs := map[string]bool{
		// Version control
		".git": true, ".svn": true, ".hg": true,
		// Dependencies
		"node_modules": true, "vendor": true, "venv": true, ".venv": true,
		"third_party": true, "3rdparty": true,
		"jspm_packages": true, "bower_components": true,
		"site-packages": true, "eggs": true, ".eggs": true, ".tox": true, "wheels": true, "lib64": true,
		// Build outputs
		"dist": true, "build": true, "target": true, "out": true, "bin": true,
		"generated": true, "auto-generated": true, ".generated": true,
		// IaC generated
		".terraform": true, ".serverless": true,
		// Cache directories
		"__pycache__": true, ".pytest_cache": true, ".mypy_cache": true,
		".cache": true, ".nyc_output": true, ".next": true, ".nuxt": true,
		".parcel-cache": true, ".turbo": true, ".svelte-kit": true,
		// IDE/Editor
		".idea": true, ".vscode": true, ".vs": true, ".eclipse": true, ".settings": true,
		// Test coverage / fixtures
		"coverage": true, ".coverage": true,
		"fixtures": true, "snapshots": true, "__snapshots__": true,
		// CI configuration
		".circleci": true, ".gitlab": true,
		// Framework specific
		".axons": true,
	}

	// File extensions to skip (binary/generated files)
	skipExts := map[string]bool{
		// Images
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true, ".svg": true, ".webp": true,
		// Fonts
		".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
		// Binary
		".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true, ".lib": true,
		".o": true, ".obj": true, ".class": true, ".jar": true, ".war": true,
		".wasm": true, ".node": true,
		// Archives
		".zip": true, ".tar": true, ".gz": true, ".rar": true, ".7z": true,
		// Documents
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
		// Media
		".mp3": true, ".mp4": true, ".wav": true, ".avi": true, ".mov": true, ".mkv": true,
		// Certificates / keys (security)
		".pem": true, ".key": true, ".crt": true, ".cer": true, ".p12": true, ".pfx": true,
		// Data files
		".csv": true, ".tsv": true, ".parquet": true, ".avro": true, ".h5": true, ".hdf5": true,
		".npy": true, ".npz": true, ".pkl": true, ".pickle": true,
		// Database files
		".db": true, ".sqlite": true, ".sqlite3": true, ".mdb": true,
		// Lock files
		".lock": true,
		// Type declarations / source maps
		".d.ts": true, ".map": true,
		// Other binary / disk images
		".bin": true, ".dat": true, ".data": true, ".raw": true, ".iso": true, ".img": true, ".dmg": true,
		// Other binary
		".pyc": true, ".pyo": true, ".swp": true, ".swo": true,
		".min.js": true, ".min.css": true, // minified files
	}

	// File names to skip
	skipFiles := map[string]bool{
		".DS_Store":       true, // macOS
		"Thumbs.db":       true, // Windows
		"desktop.ini":     true, // Windows
		".gitkeep":        true, // Git placeholder
		"*.min.js":        true, // minified JS
		"*.min.css":       true, // minified CSS
		// Lock files
		"package-lock.json":    true,
		"yarn.lock":            true,
		"pnpm-lock.yaml":       true,
		"composer.lock":        true,
		"Gemfile.lock":         true,
		"poetry.lock":          true,
		"Cargo.lock":           true,
		"go.sum":               true,
		// Config / editor files
		".npmrc":        true, ".yarnrc":        true,
		".editorconfig": true, ".prettierrc":    true, ".prettierignore": true,
		".eslintignore": true, ".dockerignore":  true,
		// License / docs (non-code)
		"LICENSE": true, "LICENSE.md": true, "LICENSE.txt": true,
		"CHANGELOG.md": true, "CONTRIBUTING.md": true, "CODE_OF_CONDUCT.md": true, "SECURITY.md": true,
		// Environment files
		".env": true, ".env.local": true, ".env.development": true, ".env.production": true, ".env.test": true, ".env.example": true,
	}

	// ── Layer 3: User-specified exclude patterns (glob) ──
	var excludeGlobs []string
	if ctx.Opts != nil {
		excludeGlobs = ctx.Opts.ExcludePatterns
	}

	var files []string
	var skippedDecoyDirs []string

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := d.Name()

		if d.IsDir() {
			// ── .axonsignore ! negation can override hardcoded blacklist ──
			// If .axonsignore explicitly "unignores" a directory (e.g. "!vendor/"),
			// it takes priority over the hardcoded skipDirs blacklist.
			if axonsIgnoreMatcher.Unignore(path, true) {
				// Explicitly unignored by .axonsignore — skip blacklist check
				// Still check .gitignore (lower priority than .axonsignore)
				if gitIgnoreMatcher.Match(path, true) {
					return filepath.SkipDir
				}
				ctx.DiscoveredDirs[path] = true
				return nil
			}

			// Check .axonsignore for ignore rules
			if axonsIgnoreMatcher.Match(path, true) {
				return filepath.SkipDir
			}

			// Fast path: skip hardcoded blacklist directories
			if skipDirs[name] {
				return filepath.SkipDir
			}

			// Layer 1: Check .gitignore
			if gitIgnoreMatcher.Match(path, true) {
				return filepath.SkipDir
			}

			// Layer 2: Check decoy directory heuristics
			if utils.IsDecoyDir(path) {
				skippedDecoyDirs = append(skippedDecoyDirs, path)
				return filepath.SkipDir
			}

			// Layer 3: Check user exclude patterns (glob)
			if matchExcludeGlobs(rootDir, path, excludeGlobs) {
				return filepath.SkipDir
			}

			ctx.DiscoveredDirs[path] = true
			return nil
		}

		// ── .axonsignore ! negation can override hardcoded blacklist for files ──
		// If .axonsignore explicitly "unignores" a file (e.g. "!go.sum"),
		// it takes priority over the hardcoded skipFiles/skipExts blacklist.
		axonsUnignore := axonsIgnoreMatcher.Unignore(path, false)
		if !axonsUnignore {
			// Check .axonsignore for ignore rules
			if axonsIgnoreMatcher.Match(path, false) {
				return nil
			}

			// Skip blacklisted files
			if skipFiles[name] {
				return nil
			}
		}

		// Skip symlinks: broken symlinks (stat fails) or symlinks to directories
		if d.Type()&os.ModeSymlink != 0 {
			fi, err := os.Stat(path)
			if err != nil || fi.IsDir() {
				return nil
			}
		}

		// Layer 1: Check .gitignore for files (always applies, even for unignored files)
		if gitIgnoreMatcher.Match(path, false) {
			return nil
		}

		// Layer 3: Check user exclude patterns for files (always applies)
		if matchExcludeGlobs(rootDir, path, excludeGlobs) {
			return nil
		}

		// Get file extension
		ext := strings.ToLower(filepath.Ext(path))

		// Skip blacklisted extensions (unless explicitly unignored by .axonsignore)
		if !axonsUnignore {
			if skipExts[ext] {
				return nil
			}

			// Skip minified files (check for .min.js, .min.css patterns)
			if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") {
				return nil
			}
		}

		// Skip files exceeding size limit (default 1MB)
		// Priority: BuildOptions.MaxFileSize > AXONS_MAX_FILE_SIZE env > 1MB default
		maxFileSize := int64(1 * 1024 * 1024) // 1MB default
		if envSize := os.Getenv("AXONS_MAX_FILE_SIZE"); envSize != "" {
			if parsed, err := strconv.ParseInt(envSize, 10, 64); err == nil && parsed > 0 {
				maxFileSize = parsed
			}
		}
		if ctx.Opts != nil && ctx.Opts.MaxFileSize > 0 {
			maxFileSize = ctx.Opts.MaxFileSize
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.Size() > maxFileSize {
			return nil
		}

		// Skip files that are not source code (not a registered language extension)
		// This dramatically reduces the number of files passed to DetectChanges/ParseFiles
		if !ctx.SupportedExts[ext] {
			return nil
		}

		// Collect code files
		files = append(files, path)

		// Track language stats
		if lang := ctx.Registry.DetectLanguage(path); lang != nil {
			ctx.LanguageStats[lang.Name]++
		}

		return nil
	})

	if err != nil {
		logger.Error("Failed to collect files", zap.Error(err))
		return err
	}

	ctx.AllFiles = files

	logFields := []zap.Field{
		zap.Int("total", len(files)),
		zap.Int("codeFiles", sumLanguageStats(ctx.LanguageStats)),
	}
	if len(skippedDecoyDirs) > 0 {
		logFields = append(logFields, zap.Int("decoyDirsSkipped", len(skippedDecoyDirs)))
	}
	logger.Info("Collected files", logFields...)

	return nil
}

// matchExcludeGlobs checks if a path matches any of the user-specified exclude
// glob patterns. Patterns are evaluated relative to rootDir.
// Supports simple glob patterns: *, **, ?.
func matchExcludeGlobs(rootDir, path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	relPath, err := filepath.Rel(rootDir, path)
	if err != nil {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	for _, pattern := range patterns {
		// Support ** glob by converting to filepath.Match compatible pattern
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return true
		}
		// Also match against the relative path for patterns containing /
		if strings.Contains(pattern, "/") {
			if matched, _ := filepath.Match(pattern, relPath); matched {
				return true
			}
			// Handle ** patterns: try matching each path prefix
			if strings.Contains(pattern, "**") {
				if matchDoubleStar(pattern, relPath) {
					return true
				}
			}
		}
	}
	return false
}

// matchDoubleStar handles ** glob patterns by simple prefix/suffix matching.
// For full ** support, consider adding github.com/bmatcuk/doublestar in the future.
func matchDoubleStar(pattern, path string) bool {
	parts := strings.SplitN(pattern, "**", 2)
	if len(parts) != 2 {
		return false
	}
	prefix := strings.TrimSuffix(parts[0], "/")
	suffix := strings.TrimPrefix(parts[1], "/")

	if prefix != "" && !strings.HasPrefix(path, prefix) {
		return false
	}
	if suffix != "" {
		// Check if any suffix of the path matches
		for i := 0; i < len(path); i++ {
			if matched, _ := filepath.Match(suffix, path[i:]); matched {
				return true
			}
		}
		return false
	}
	return true
}

// sumLanguageStats returns the total count of code files.
func sumLanguageStats(stats map[string]int) int {
	total := 0
	for _, count := range stats {
		total += count
	}
	return total
}