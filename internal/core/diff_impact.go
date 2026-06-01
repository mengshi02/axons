package core

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/pkg/types"
)

// DiffImpactService analyzes the impact of git diff on the code graph.
type DiffImpactService struct {
	repo *repository.Repository
}

// NewDiffImpactService creates a new DiffImpactService.
func NewDiffImpactService(repo *repository.Repository) *DiffImpactService {
	return &DiffImpactService{repo: repo}
}

// DiffImpactOptions holds options for diff-impact analysis.
type DiffImpactOptions struct {
	RootDir  string
	Branch   string
	MaxDepth int
}

// DiffImpactResult holds the analysis result.
type DiffImpactResult struct {
	ChangedFiles   []string
	ImpactedNodes  []*types.Node
	TotalAffected  int
}

// Analyze computes the blast radius of pending git changes.
func (s *DiffImpactService) Analyze(ctx context.Context, opts *DiffImpactOptions) (*DiffImpactResult, error) {
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}

	// Get changed files via git diff
	changedFiles, err := getGitChangedFiles(opts.RootDir, opts.Branch)
	if err != nil {
		return nil, fmt.Errorf("get changed files: %w", err)
	}

	result := &DiffImpactResult{ChangedFiles: changedFiles}
	impactedSet := make(map[int64]*types.Node)

	qs := graph.NewQueryService(s.repo)

	for _, file := range changedFiles {
		nodes, err := s.repo.FindNodesByFile(file)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			impact, err := qs.ImpactAnalysis(ctx, node.ID, maxDepth)
			if err != nil {
				continue
			}
			for _, n := range impact.ImpactedNodes {
				impactedSet[n.ID] = n
			}
		}
	}

	for _, n := range impactedSet {
		result.ImpactedNodes = append(result.ImpactedNodes, n)
	}
	result.TotalAffected = len(result.ImpactedNodes)
	return result, nil
}

func getGitChangedFiles(rootDir, branch string) ([]string, error) {
	var args []string
	if branch != "" {
		args = []string{"diff", "--name-only", branch + "...HEAD"}
	} else {
		args = []string{"diff", "--name-only", "HEAD"}
	}
	cmd := exec.Command("git", args...)
	if rootDir != "" {
		cmd.Dir = rootDir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}