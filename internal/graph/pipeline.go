// Package graph provides code graph building and querying capabilities.
package graph

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph/stages"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/pkg/types"
	"go.uber.org/zap"
)

// Pipeline orchestrates the graph build process using modular stages.
type Pipeline struct {
	ctx *stages.PipelineContext
}

// NewPipeline creates a new build pipeline.
func NewPipeline(repo *repository.Repository, opts *types.BuildOptions) *Pipeline {
	return NewPipelineWithGlobal(repo, nil, opts)
}

// NewPipelineWithGlobal creates a new build pipeline with global repository access.
func NewPipelineWithGlobal(repo *repository.Repository, globalRepo *repository.GlobalRepository, opts *types.BuildOptions) *Pipeline {
	if opts == nil {
		opts = &types.BuildOptions{}
	}

	ctx := stages.NewPipelineContext()
	ctx.Opts = opts
	ctx.Repo = repo
	ctx.GlobalRepo = globalRepo

	return &Pipeline{
		ctx: ctx,
	}
}

// Build executes the full build pipeline using declared stages with topological sort.
func (p *Pipeline) Build(ctx context.Context) (*types.BuildResult, error) {
	p.ctx.BuildStart = time.Now()

	// progress is a helper that calls the OnProgress callback if set.
	progress := func(phase string, percent int, msg string) {
		if p.ctx.OnProgress != nil {
			p.ctx.OnProgress(phase, percent, msg)
		}
	}

	// ── Crash recovery: check incrementalInProgress dirty flag ──
	// If a previous incremental build crashed mid-way, the flag will still be set.
	// Force a full rebuild to ensure DB consistency.
	if !p.ctx.Opts.FullBuild && p.ctx.Repo != nil {
		if flag, _ := p.ctx.Repo.GetMetadata("incremental_in_progress"); flag != "" {
			logger.Warn("Detected incomplete incremental build, forcing full rebuild",
				zap.String("flag", flag),
			)
			p.ctx.Opts.FullBuild = true
			_ = p.ctx.Repo.SetMetadata("incremental_in_progress", "")
		}
	}

	// Set incrementalInProgress flag before starting (for non-full builds)
	if !p.ctx.Opts.FullBuild && p.ctx.Repo != nil {
		_ = p.ctx.Repo.SetMetadata("incremental_in_progress", fmt.Sprintf("started_at=%d", p.ctx.BuildStart.Unix()))
	}

	// Get and sort stages by dependencies
	stageList := stages.AllStages()
	sorted, err := stages.TopologicalSort(stageList)
	if err != nil {
		return nil, fmt.Errorf("pipeline stage configuration error: %w", err)
	}

	// Execute stages in dependency order
	for _, stage := range sorted {
		progress(stage.Name, stage.ProgressStart, stage.ProgressMsg)

		if err := stage.Execute(ctx, p.ctx); err != nil {
			// Build failed — leave the dirty flag set so next startup triggers full rebuild
			return nil, fmt.Errorf("stage %s failed: %w", stage.Name, err)
		}

		progress(stage.Name, stage.ProgressEnd, fmt.Sprintf("Stage %s complete", stage.Name))

		// Push intermediate delta after stages that produce graph data
		if p.ctx.OnDelta != nil {
			switch stage.Name {
			case "insert":
				// After InsertNodes — push new nodes (no edges yet)
				p.ctx.OnDelta("insert", p.ctx.AllNodes, nil)
			case "edges":
				// After BuildEdges — push new edges (nodes were already pushed)
				p.ctx.OnDelta("edges", nil, p.ctx.AllEdges)
			}
		}

		// Check for early exit after detect stage
		if stage.Name == "detect" && p.ctx.EarlyExit {
			// Clean exit — clear the dirty flag
			if p.ctx.Repo != nil {
				_ = p.ctx.Repo.SetMetadata("incremental_in_progress", "")
			}
			progress("finalize", 100, "No changes, build skipped")
			return &types.BuildResult{
				Duration:     time.Since(p.ctx.BuildStart),
				IsFullBuild:  p.ctx.IsFullBuild,
				ChangedFiles: []string{},
				RemovedFiles: p.ctx.RemovedFiles,
			}, nil
		}
	}

	// Stage 8: Process detection (materialize execution flows)
	go func() {
		detector := NewProcessDetector(p.ctx.Repo)
		if err := detector.DetectAndSave(); err != nil {
			// Non-fatal background task
			_ = err
		}
	}()

	// Build result

	// ── Build succeeded — clear the incrementalInProgress dirty flag ──
	if p.ctx.Repo != nil {
		_ = p.ctx.Repo.SetMetadata("incremental_in_progress", "")
	}

	result := &types.BuildResult{
		Duration:                time.Since(p.ctx.BuildStart),
		IsFullBuild:             p.ctx.IsFullBuild,
		ChangedFiles:            make([]string, len(p.ctx.ParseChanges)),
		RemovedFiles:            p.ctx.RemovedFiles,
		ChangedFileOldNodeIDs:   p.ctx.ChangedFileOldNodeIDs,
		ChangedFileOldEdgeIDs:   p.ctx.ChangedFileOldEdgeIDs,
		FilesParsed:             len(p.ctx.ParseResults),
		NodesCreated:            len(p.ctx.AllNodes),
	}
	for i, change := range p.ctx.ParseChanges {
		result.ChangedFiles[i] = stages.ToRelativePath(change.Path, p.ctx.Opts.RootDir)
	}

	// Calculate language stack (sorted by file count)
	type langCount struct {
		name  string
		count int
	}
	var langCounts []langCount
	for lang, count := range p.ctx.LanguageStats {
		langCounts = append(langCounts, langCount{lang, count})
	}
	sort.Slice(langCounts, func(i, j int) bool {
		return langCounts[i].count > langCounts[j].count
	})
	for _, lc := range langCounts {
		result.LanguageStack = append(result.LanguageStack, lc.name)
	}

	// Save language stack to database if GlobalRepo and project ID are available
	if p.ctx.GlobalRepo != nil && p.ctx.Opts.ProjectID != "" && len(result.LanguageStack) > 0 {
		if err := p.ctx.GlobalRepo.UpdateProjectLanguageStack(p.ctx.Opts.ProjectID, result.LanguageStack); err != nil {
			// Non-fatal error, just log it
			logger.Warn("Failed to update language stack", zap.Error(err))
		}
	}

	return result, nil
}

// SetOnProgress sets the progress callback for the pipeline.
func (p *Pipeline) SetOnProgress(fn func(phase string, percent int, message string)) {
	p.ctx.OnProgress = fn
}

// SetOnDelta sets the delta callback for the pipeline.
// This is called after each stage that produces graph data (insert, edges)
// to push intermediate results to the frontend for progressive rendering.
func (p *Pipeline) SetOnDelta(fn func(stage string, nodes []*types.Node, edges []*types.Edge)) {
	p.ctx.OnDelta = fn
}

// GetContext returns the pipeline context.
func (p *Pipeline) GetContext() *stages.PipelineContext {
	return p.ctx
}