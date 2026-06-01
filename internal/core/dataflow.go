package core

import (
	"fmt"
	"strings"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// DataflowService retrieves dataflow edges for a function from the DB.
type DataflowService struct {
	repo *repository.Repository
}

// NewDataflowService creates a new DataflowService.
func NewDataflowService(repo *repository.Repository) *DataflowService {
	return &DataflowService{repo: repo}
}

// DataflowOptions holds query options.
type DataflowOptions struct {
	Name string
	File string
}

// DataflowEdgeInfo represents a single dataflow relationship.
type DataflowEdgeInfo struct {
	From     string
	To       string
	EdgeType string
}

// DataflowResult holds the dataflow analysis output.
type DataflowResult struct {
	Node          *types.Node
	DataflowEdges []DataflowEdgeInfo
}

// Analyze returns dataflow edges for the named symbol.
func (s *DataflowService) Analyze(opts *DataflowOptions) (*DataflowResult, error) {
	nodes, err := s.repo.FindNodesByName(opts.Name, 10)
	if err != nil || len(nodes) == 0 {
		return nil, fmt.Errorf("symbol %q not found", opts.Name)
	}

	var node *types.Node
	for _, n := range nodes {
		if opts.File == "" || strings.Contains(n.File, opts.File) {
			node = n
			break
		}
	}
	if node == nil {
		node = nodes[0]
	}

	// Query flow edges (flows_to, returns, mutates) for this node
	flowKinds := []types.EdgeKind{
		types.EdgeKindFlowsTo,
		types.EdgeKindReturns,
		types.EdgeKindMutates,
	}

	result := &DataflowResult{Node: node}
	for _, kind := range flowKinds {
		edges, err := s.repo.FindEdgesByKind(kind, 200, 0)
		if err != nil {
			continue
		}
		for _, e := range edges {
			if e.SourceID != node.ID {
				continue
			}
			target, _ := s.repo.FindNodeByID(e.TargetID)
			to := fmt.Sprintf("node:%d", e.TargetID)
			if target != nil {
				to = target.Name
			}
			result.DataflowEdges = append(result.DataflowEdges, DataflowEdgeInfo{
				From:     node.Name,
				To:       to,
				EdgeType: string(e.Kind),
			})
		}
	}
	return result, nil
}