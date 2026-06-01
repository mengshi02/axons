// Package stages provides pipeline stages for graph building.
package stages

import (
	"os"
	"time"

	"github.com/mengshi02/axons/internal/logger"
	"go.uber.org/zap"
)

// DetectChanges detects which files have changed since last build.
func DetectChanges(ctx *PipelineContext) error {
	start := time.Now()
	defer func() {
		ctx.RecordTiming("detect", time.Since(start))
	}()

	// Full build case — skip hash computation entirely.
	// All files will be re-parsed anyway, so reading content just to compute
	// a hash is pure waste. Content will be read on-demand during ParseFiles.
	if ctx.Opts.FullBuild {
		ctx.IsFullBuild = true

		ctx.ParseChanges = make([]*FileChange, 0, len(ctx.AllFiles))
		for _, path := range ctx.AllFiles {
			// Defensive: skip if path is actually a directory (e.g. symlink to dir)
			fi, err := os.Stat(path)
			if err != nil {
				logger.Warn("Failed to stat file for full build", zap.String("path", path), zap.Error(err))
				continue
			}
			if fi.IsDir() {
				continue
			}
			ctx.ParseChanges = append(ctx.ParseChanges, &FileChange{
				Path:  path,
				Mtime: fi.ModTime().Unix(),
				Size:  fi.Size(),
			})
		}
		logger.Info("Full build requested", zap.Int("files", len(ctx.ParseChanges)))
		return nil
	}

	// ── Incremental build ──
	// Two-tier change detection:
	//   Tier 1: mtime + size fast check (no file read)
	//   Tier 2: content hash (only for files where Tier 1 indicates a change)

	// Get all previously indexed file metadata
	indexedMeta, err := ctx.Repo.GetAllFileMeta()
	if err != nil {
		// If error, fall back to full build (without reading file content)
		ctx.IsFullBuild = true
		ctx.ParseChanges = make([]*FileChange, 0, len(ctx.AllFiles))
		for _, path := range ctx.AllFiles {
			fi, err := os.Stat(path)
			if err != nil {
				logger.Warn("Failed to stat file for fallback full build", zap.String("path", path), zap.Error(err))
				continue
			}
			if fi.IsDir() {
				continue
			}
			ctx.ParseChanges = append(ctx.ParseChanges, &FileChange{
				Path:  path,
				Mtime: fi.ModTime().Unix(),
				Size:  fi.Size(),
			})
		}
		logger.Warn("Failed to get indexed file metadata, falling back to full build", zap.Error(err))
		return nil
	}

	// Build map of current files using relative paths
	currentFiles := make(map[string]bool)
	for _, f := range ctx.AllFiles {
		relativePath := ToRelativePath(f, ctx.Opts.RootDir)
		currentFiles[relativePath] = true
	}

	// Find removed files
	for path := range indexedMeta {
		if !currentFiles[path] {
			ctx.RemovedFiles = append(ctx.RemovedFiles, path)
		}
	}

	// Tier 1 + Tier 2 change detection
	tier1Hits := 0
	tier2Hits := 0

	for _, path := range ctx.AllFiles {
		relativePath := ToRelativePath(path, ctx.Opts.RootDir)

		// Get current file info (mtime + size) — no content read
		fi, err := os.Stat(path)
		if err != nil {
			logger.Warn("Failed to stat file", zap.String("path", path), zap.Error(err))
			continue
		}

		storedMeta, exists := indexedMeta[relativePath]
		if !exists {
			// New file — needs parsing, but defer content read to ParseFiles
			ctx.ParseChanges = append(ctx.ParseChanges, &FileChange{
				Path:  path,
				Mtime: fi.ModTime().Unix(),
				Size:  fi.Size(),
			})
			tier1Hits++
			continue
		}

		// Tier 1: mtime + size fast check
		currentMtime := fi.ModTime().Unix()
		currentSize := fi.Size()
		if currentMtime == storedMeta.Mtime && currentSize == storedMeta.Size {
			// File unchanged at Tier 1 — skip
			continue
		}

		// Tier 2: content hash comparison (only for Tier 1 hits)
		content, err := os.ReadFile(path)
		if err != nil {
			logger.Warn("Failed to read file for hash check", zap.String("path", path), zap.Error(err))
			continue
		}
		currentHash := ComputeHash(content)

		if currentHash != storedMeta.Hash {
			ctx.ParseChanges = append(ctx.ParseChanges, &FileChange{
				Path:    path,
				Hash:    currentHash,
				Content: content,
				Mtime:   currentMtime,
				Size:    currentSize,
			})
			tier2Hits++
		} else {
			// Tier 1 hit but Tier 2 miss (e.g. touch without content change)
			// Update stored metadata to avoid repeated Tier 2 checks
			_ = ctx.Repo.UpsertFileHash(relativePath, currentMtime, currentSize, currentHash)
		}
	}

	// Delete removed files from database
	for _, f := range ctx.RemovedFiles {
		if err := ctx.Repo.DeleteEdgesByFile(f); err != nil {
			logger.Warn("Failed to delete edges for removed file", zap.String("file", f), zap.Error(err))
		}
		if err := ctx.Repo.DeleteNodesByFile(f); err != nil {
			logger.Warn("Failed to delete nodes for removed file", zap.String("file", f), zap.Error(err))
		}
	}

	// Delete old data for changed files before re-parsing
	// This ensures stale nodes/edges are removed before InsertNodes/BuildEdges recreate them
	// First, collect old node/edge IDs so the frontend can remove them from the graph
	for _, fc := range ctx.ParseChanges {
		relativePath := ToRelativePath(fc.Path, ctx.Opts.RootDir)

		// Collect old node IDs before deletion
		oldNodeIDs, err := ctx.Repo.FindNodeIDsByFile(relativePath)
		if err != nil {
			logger.Warn("Failed to query old node IDs for changed file", zap.String("file", relativePath), zap.Error(err))
		} else {
			ctx.ChangedFileOldNodeIDs = append(ctx.ChangedFileOldNodeIDs, oldNodeIDs...)
		}

		// Collect old edge IDs before deletion
		oldEdgeIDs, err := ctx.Repo.FindEdgeIDsByFile(relativePath)
		if err != nil {
			logger.Warn("Failed to query old edge IDs for changed file", zap.String("file", relativePath), zap.Error(err))
		} else {
			ctx.ChangedFileOldEdgeIDs = append(ctx.ChangedFileOldEdgeIDs, oldEdgeIDs...)
		}

		// ── Three-layer write set expansion (my-inspired) ──
		// Layer 1: BFS importers (depth ≤ 4) — files that transitively import from this file
		importerFiles, err := ctx.Repo.FindImporterFiles(relativePath, 4)
		if err != nil {
			logger.Debug("Failed to find importer files via BFS", zap.String("file", relativePath), zap.Error(err))
		} else {
			ctx.AffectedFilesFromDB = append(ctx.AffectedFilesFromDB, importerFiles...)
		}

		// Layer 2: Direct 1-hop neighbors — files with any edge to this file
		oneHopFiles, err := ctx.Repo.FindOneHopNeighborFiles(relativePath)
		if err != nil {
			logger.Debug("Failed to find 1-hop neighbor files", zap.String("file", relativePath), zap.Error(err))
		} else {
			ctx.AffectedFilesFromDB = append(ctx.AffectedFilesFromDB, oneHopFiles...)
		}

		// Layer 3: Shadow-seed — files that might import from a module path this file resolves to
		// (New files can "steal" import resolution from existing files, e.g. foo.ts stealing foo.js)
		shadowSeedFiles, err := ctx.Repo.FindFilesImportingFromPath(relativePath)
		if err != nil {
			logger.Debug("Failed to find shadow-seed files", zap.String("file", relativePath), zap.Error(err))
		} else {
			ctx.AffectedFilesFromDB = append(ctx.AffectedFilesFromDB, shadowSeedFiles...)
		}

		if err := ctx.Repo.DeleteEdgesByFile(relativePath); err != nil {
			logger.Warn("Failed to delete edges for changed file", zap.String("file", relativePath), zap.Error(err))
		}
		if err := ctx.Repo.DeleteNodesByFile(relativePath); err != nil {
			logger.Warn("Failed to delete nodes for changed file", zap.String("file", relativePath), zap.Error(err))
		}
	}

	// Early exit if no changes
	if len(ctx.ParseChanges) == 0 && len(ctx.RemovedFiles) == 0 {
		ctx.EarlyExit = true
		logger.Info("No changes detected, skipping build")
		return nil
	}

	ctx.IsFullBuild = false
	logger.Info("Detected changes",
		zap.Int("changed", len(ctx.ParseChanges)),
		zap.Int("removed", len(ctx.RemovedFiles)),
		zap.Int("tier1_new", tier1Hits),
		zap.Int("tier2_modified", tier2Hits),
	)

	return nil
}