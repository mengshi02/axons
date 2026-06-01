package core

import (
	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/types"
)

// QueryService handles symbol query operations.
type QueryService struct {
	repo *repository.Repository
}

// NewQueryService creates a new QueryService.
func NewQueryService(repo *repository.Repository) *QueryService {
	return &QueryService{repo: repo}
}

// QueryOptions holds query parameters.
type QueryOptions struct {
	Name    string
	Kind    string
	File    string
	Callers bool
	Callees bool
	NoTests bool
	Limit   int
}

// QueryResult holds query results.
type QueryResult struct {
	Nodes   []*types.Node
	Callers []*types.Node
	Callees []*types.Node
}

// Query searches for symbols by name, optionally with caller/callee info.
func (s *QueryService) Query(opts *QueryOptions) (*QueryResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	nodes, err := s.repo.FindNodesByName(opts.Name, limit)
	if err != nil {
		return nil, err
	}

	result := &QueryResult{Nodes: nodes}

	if len(nodes) == 0 {
		return result, nil
	}

	// Get callers/callees for the first match
	first := nodes[0]

	if opts.Callers {
		callers, err := s.repo.FindCallers(first.ID)
		if err == nil {
			result.Callers = callers
		}
	}

	if opts.Callees {
		callees, err := s.repo.FindCallees(first.ID)
		if err == nil {
			result.Callees = callees
		}
	}

	return result, nil
}

// FindNodeByID returns a single node by ID.
func (s *QueryService) FindNodeByID(id int64) (*types.Node, error) {
	return s.repo.FindNodeByID(id)
}