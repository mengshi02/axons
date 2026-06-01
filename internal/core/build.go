// Package core provides the core business logic layer shared by CLI and daemon.
package core

import (
	"context"
	"fmt"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/pkg/types"
)

// BuildService handles code graph build operations.
type BuildService struct {
	repo *repository.Repository
}

// NewBuildService creates a new BuildService.
func NewBuildService(repo *repository.Repository) *BuildService {
	return &BuildService{repo: repo}
}

// BuildOptions mirrors types.BuildOptions for convenience.
type BuildOptions struct {
	RootDir         string
	FullBuild       bool
	ExcludePatterns []string
	IncludeDataflow bool
	IncludeAST      bool
	ProjectID       string
	ProjectName     string
}

// Build executes the graph build pipeline directly against the local DB.
func (s *BuildService) Build(ctx context.Context, opts *BuildOptions) (*types.BuildResult, error) {
	if opts.RootDir == "" {
		return nil, fmt.Errorf("root_dir is required")
	}

	buildOpts := &types.BuildOptions{
		RootDir:         opts.RootDir,
		FullBuild:       opts.FullBuild,
		ExcludePatterns: opts.ExcludePatterns,
		IncludeDataflow: opts.IncludeDataflow,
		IncludeAST:      opts.IncludeAST,
		ProjectID:       opts.ProjectID,
		ProjectName:     opts.ProjectName,
	}

	pipeline := graph.NewPipeline(s.repo, buildOpts)
	return pipeline.Build(ctx)
}