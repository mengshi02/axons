package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// SequenceService generates sequence diagrams from the call graph.
type SequenceService struct {
	repo *repository.Repository
}

// NewSequenceService creates a new SequenceService.
func NewSequenceService(repo *repository.Repository) *SequenceService {
	return &SequenceService{repo: repo}
}

// SequenceOptions holds options for sequence generation.
type SequenceOptions struct {
	Name        string
	Depth       int
	FileFilters []string
	KindFilter  types.SymbolKind
	NoTests     bool
}

// SequenceMessage is a single message in the diagram.
type SequenceMessage struct {
	From     string
	To       string
	Function string
	Line     int
}

// SequenceResult holds the generated sequence data.
type SequenceResult struct {
	Entry        *types.Node
	Participants []string
	Messages     []SequenceMessage
	Truncated    bool
}

// Generate builds a sequence diagram for the named symbol.
func (s *SequenceService) Generate(ctx context.Context, opts *SequenceOptions) (*SequenceResult, error) {
	maxDepth := opts.Depth
	if maxDepth <= 0 {
		maxDepth = 10
	}

	nodes, err := s.repo.FindNodesByName(opts.Name, 20)
	if err != nil || len(nodes) == 0 {
		return nil, fmt.Errorf("symbol %q not found", opts.Name)
	}

	entry := nodes[0]
	for _, n := range nodes {
		if string(opts.KindFilter) != "" && n.Kind == opts.KindFilter {
			entry = n
			break
		}
	}

	result := &SequenceResult{Entry: entry}
	seen := make(map[int64]bool)
	fileSet := make(map[string]bool)

	var dfs func(nodeID int64, depth int)
	dfs = func(nodeID int64, depth int) {
		if depth >= maxDepth || seen[nodeID] {
			if depth >= maxDepth {
				result.Truncated = true
			}
			return
		}
		seen[nodeID] = true

		caller, err := s.repo.FindNodeByID(nodeID)
		if err != nil {
			return
		}
		callees, _ := s.repo.FindCallees(nodeID)
		for _, callee := range callees {
			if opts.NoTests && strings.Contains(callee.File, "_test.") {
				continue
			}
			if len(opts.FileFilters) > 0 && !matchesAny(callee.File, opts.FileFilters) {
				continue
			}
			result.Messages = append(result.Messages, SequenceMessage{
				From:     caller.File,
				To:       callee.File,
				Function: callee.Name,
				Line:     callee.Line,
			})
			fileSet[caller.File] = true
			fileSet[callee.File] = true
			dfs(callee.ID, depth+1)
		}
	}
	dfs(entry.ID, 0)

	for f := range fileSet {
		result.Participants = append(result.Participants, f)
	}
	return result, nil
}

func matchesAny(s string, filters []string) bool {
	for _, f := range filters {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

// BaseFile returns just the filename without extension.
func BaseFile(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return base[:len(base)-len(ext)]
}