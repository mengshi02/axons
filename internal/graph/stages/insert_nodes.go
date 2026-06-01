// Package stages provides pipeline stages for graph building.
package stages

import (
	"path/filepath"
	"time"

	"github.com/mengshi02/axons/internal/logger"
	"go.uber.org/zap"
)

// InsertNodes inserts parsed nodes into the database and builds indexes.
func InsertNodes(ctx *PipelineContext) error {
	if ctx.EarlyExit {
		return nil
	}

	start := time.Now()
	defer func() {
		ctx.RecordTiming("insert", time.Since(start))
	}()

	// Build lookup indexes and collect all file paths
	allFilePaths := make([]string, 0)
	seenFiles := make(map[string]bool)

	// For incremental builds, decide whether to load all existing nodes into memory
	// or use on-demand DB queries based on node count threshold.
	if !ctx.IsFullBuild {
		// Build set of changed file relative paths to skip (their nodes were already deleted)
		changedFiles := make(map[string]bool)
		for _, fc := range ctx.ParseChanges {
			changedFiles[ToRelativePath(fc.Path, ctx.Opts.RootDir)] = true
		}

		// Check total node count to decide lookup strategy
		nodeCount, err := ctx.Repo.CountNodes()
		if err != nil {
			logger.Warn("Failed to count nodes for lookup strategy", zap.Error(err))
			nodeCount = 0
		}

		// Threshold: if more than 50000 nodes, use DB lookup to avoid high memory usage
		const dbLookupThreshold int64 = 50000
		if nodeCount > dbLookupThreshold {
			// Large project: use DB queries on-demand
			ctx.UseDBLookup = true
			ctx.NodeLookup = &dbNodeLookup{repo: ctx.Repo}
			logger.Info("Using DB lookup for incremental build (large project)",
				zap.Int64("nodeCount", nodeCount),
				zap.Int64("threshold", dbLookupThreshold),
			)
		} else {
			// Small/medium project: load all nodes into memory for faster resolution
			existingNodes, err := ctx.Repo.ListAllNodes()
			if err != nil {
				logger.Warn("Failed to load existing nodes for incremental build", zap.Error(err))
			} else {
				loadedCount := 0
				for _, node := range existingNodes {
					// Skip nodes from changed files - they were deleted and will be re-created
					if changedFiles[node.File] {
						continue
					}
					ctx.NodesByName[node.Name] = append(ctx.NodesByName[node.Name], node)
					ctx.NodesByFile[node.File] = append(ctx.NodesByFile[node.File], node)
					if node.QualifiedName != "" {
						ctx.NodesByQualified[node.QualifiedName] = append(ctx.NodesByQualified[node.QualifiedName], node)
					}
					dir := filepath.Dir(node.File)
					ctx.NodesByDir[dir] = append(ctx.NodesByDir[dir], node)
					if !seenFiles[node.File] {
						seenFiles[node.File] = true
						allFilePaths = append(allFilePaths, node.File)
					}
					loadedCount++
				}
				logger.Info("Loaded existing nodes for incremental build",
					zap.Int("total", len(existingNodes)),
					zap.Int("loaded", loadedCount),
					zap.Int("skippedChangedFiles", len(changedFiles)),
				)
			}
			ctx.NodeLookup = &memoryNodeLookup{ctx: ctx}
		}
	}

	for _, pr := range ctx.ParseResults {
		if pr.Err != nil {
			continue
		}
		for _, node := range pr.Nodes {
			ctx.AllNodes = append(ctx.AllNodes, node)
			ctx.NodesByName[node.Name] = append(ctx.NodesByName[node.Name], node)
			ctx.NodesByFile[node.File] = append(ctx.NodesByFile[node.File], node)
			if node.QualifiedName != "" {
				ctx.NodesByQualified[node.QualifiedName] = append(ctx.NodesByQualified[node.QualifiedName], node)
			}
			// Index by directory for Go package imports
			dir := filepath.Dir(node.File)
			ctx.NodesByDir[dir] = append(ctx.NodesByDir[dir], node)
			// Collect unique file paths
			if !seenFiles[node.File] {
				seenFiles[node.File] = true
				allFilePaths = append(allFilePaths, node.File)
			}
		}
	}

	// Build suffix index for fast import resolution
	ctx.BuildSuffixIndex(allFilePaths)

	// Save all nodes to database in a single transaction
	// This is dramatically faster than individual CreateNode calls (each with its own fsync).
	// BatchInsertNodesWithIDs uses INSERT OR IGNORE + backfills IDs for edge creation.
	inserted, err := ctx.Repo.BatchInsertNodesWithIDs(ctx.AllNodes)
	if err != nil {
		logger.Warn("Batch node insert failed, falling back to individual inserts",
			zap.Error(err),
		)
		// Fallback: insert nodes one by one
		for _, node := range ctx.AllNodes {
			if err := ctx.Repo.CreateNode(node); err != nil {
				logger.Warn("Failed to create node",
					zap.String("name", node.Name),
					zap.String("file", node.File),
					zap.Error(err),
				)
			}
		}
	} else {
		logger.Debug("Batch inserted nodes",
			zap.Int("total", len(ctx.AllNodes)),
			zap.Int("inserted", inserted),
			zap.Int("skipped", len(ctx.AllNodes)-inserted),
		)
	}

	// Update file hashes for parsed files (with actual mtime/size)
	// Build a lookup map from ParseChanges for O(1) access
	changeMap := make(map[string]*FileChange, len(ctx.ParseChanges))
	for _, fc := range ctx.ParseChanges {
		changeMap[fc.Path] = fc
	}
	for _, pr := range ctx.ParseResults {
		if pr.Err != nil || pr.Output == nil {
			continue
		}
		relativePath := ToRelativePath(pr.FilePath, ctx.Opts.RootDir)
		if hash, ok := ctx.FileHashes[pr.FilePath]; ok {
			var mtime, size int64
			if fc, found := changeMap[pr.FilePath]; found {
				mtime = fc.Mtime
				size = fc.Size
			}
			ctx.Repo.UpsertFileHash(relativePath, mtime, size, hash)
		}
	}

	logger.Info("Inserted nodes",
		zap.Int("count", len(ctx.AllNodes)),
		zap.Int("filesWithNodes", len(ctx.NodesByFile)),
		zap.Int("uniqueNames", len(ctx.NodesByName)),
	)
	return nil
}