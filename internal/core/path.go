package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/graph"
	"github.com/mengshi02/axons/pkg/types"
)

// PathService finds call paths between two symbols.
type PathService struct {
	repo *repository.Repository
}

// NewPathService creates a new PathService.
func NewPathService(repo *repository.Repository) *PathService {
	return &PathService{repo: repo}
}

// PathOptions holds options for path finding.
type PathOptions struct {
	From     string
	To       string
	MaxDepth int
	File     string
}

// PathResult holds the result.
type PathResult struct {
	From       string
	To         string
	Paths      [][]*types.Node
	TotalPaths int
	Truncated  bool
}

// Find finds all call paths from one symbol to another.
func (s *PathService) Find(ctx context.Context, opts *PathOptions) (*PathResult, error) {
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 10
	}

	// Resolve from node
	fromNodes, err := s.repo.FindNodesByName(opts.From, 10)
	if err != nil || len(fromNodes) == 0 {
		return nil, fmt.Errorf("symbol %q not found", opts.From)
	}
	toNodes, err := s.repo.FindNodesByName(opts.To, 10)
	if err != nil || len(toNodes) == 0 {
		return nil, fmt.Errorf("symbol %q not found", opts.To)
	}

	// Filter by file if specified
	fromNode := filterByFile(fromNodes, opts.File)
	toNode := filterByFile(toNodes, opts.File)

	qs := graph.NewQueryService(s.repo)
	chain, err := qs.FindCallChain(ctx, fromNode.ID, toNode.ID, maxDepth)
	if err != nil {
		return nil, err
	}

	return &PathResult{
		From:       opts.From,
		To:         opts.To,
		Paths:      chain.Paths,
		TotalPaths: len(chain.Paths),
	}, nil
}

func filterByFile(nodes []*types.Node, fileFilter string) *types.Node {
	if fileFilter == "" {
		return nodes[0]
	}
	for _, n := range nodes {
		if strings.Contains(n.File, fileFilter) {
			return n
		}
	}
	return nodes[0]
}