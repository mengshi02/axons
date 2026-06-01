package core

import (
	"fmt"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// CFGService retrieves control flow graph data from the DB.
type CFGService struct {
	repo *repository.Repository
}

// NewCFGService creates a new CFGService.
func NewCFGService(repo *repository.Repository) *CFGService {
	return &CFGService{repo: repo}
}

// CFGOptions holds query options.
type CFGOptions struct {
	Name    string
	File    []string
	Kind    string
	NoTests bool
	Limit   int
}

// CFGBlock is a basic block in the CFG.
type CFGBlock struct {
	Index     int
	Type      string
	StartLine int
	EndLine   int
	Label     string
}

// CFGEdge is an edge in the CFG.
type CFGEdge struct {
	Source int
	Target int
	Kind   string
}

// CFGSummary summarizes a CFG.
type CFGSummary struct {
	BlockCount int
	EdgeCount  int
}

// CFGResult holds CFG data for one symbol.
type CFGResult struct {
	Name    string
	Kind    string
	File    string
	Line    int
	Blocks  []CFGBlock
	Edges   []CFGEdge
	Summary CFGSummary
}

// Query returns CFG data for symbols matching opts.Name.
func (s *CFGService) Query(opts *CFGOptions) ([]CFGResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	nodes, err := s.repo.FindNodesByName(opts.Name, limit)
	if err != nil {
		return nil, fmt.Errorf("find nodes: %w", err)
	}

	var results []CFGResult
	for _, n := range nodes {
		if opts.Kind != "" && string(n.Kind) != opts.Kind {
			continue
		}
		if opts.NoTests && strings.Contains(n.File, "_test.") {
			continue
		}
		if len(opts.File) > 0 && !matchesAny(n.File, opts.File) {
			continue
		}

		// Load AST nodes to build a basic CFG
		astNodes, _ := s.repo.FindASTNodesByParent(n.ID)
		blocks, edges := buildCFGBlocks(astNodes)

		results = append(results, CFGResult{
			Name:   n.Name,
			Kind:   string(n.Kind),
			File:   n.File,
			Line:   n.Line,
			Blocks: blocks,
			Edges:  edges,
			Summary: CFGSummary{
				BlockCount: len(blocks),
				EdgeCount:  len(edges),
			},
		})
	}
	return results, nil
}

func buildCFGBlocks(astNodes []*types.AstNode) ([]CFGBlock, []CFGEdge) {
	blocks := make([]CFGBlock, 0, len(astNodes))
	edges := make([]CFGEdge, 0, len(astNodes))

	for i, n := range astNodes {
		blocks = append(blocks, CFGBlock{
			Index:     i,
			Type:      n.Kind,
			StartLine: n.Line,
			EndLine:   n.Line,
			Label:     n.Name,
		})
		if i > 0 {
			edges = append(edges, CFGEdge{Source: i - 1, Target: i, Kind: "sequential"})
		}
	}
	return blocks, edges
}