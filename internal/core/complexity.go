package core

import (
	"github.com/mengshi02/axons/internal/db/repository"
)

// ComplexityService retrieves complexity metrics from the DB.
type ComplexityService struct {
	repo *repository.Repository
}

// NewComplexityService creates a new ComplexityService.
func NewComplexityService(repo *repository.Repository) *ComplexityService {
	return &ComplexityService{repo: repo}
}

// ComplexityOptions holds filter options.
type ComplexityOptions struct {
	Threshold int
	Limit     int
	File      string
}

// FunctionComplexity holds complexity info for a function.
type FunctionComplexity struct {
	NodeID     int64
	Name       string
	File       string
	Line       int
	Cyclomatic int
	Cognitive  int
	Nesting    int
	LOC        int
}

// TopComplex returns the most complex functions above the threshold.
func (s *ComplexityService) TopComplex(opts *ComplexityOptions) ([]FunctionComplexity, error) {
	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = 10
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	query := `
		SELECT n.id, n.name, n.file, n.line, fc.cyclomatic, fc.cognitive, fc.nesting, fc.lines_of_code
		FROM function_complexity fc
		JOIN nodes n ON fc.node_id = n.id
		WHERE fc.cyclomatic >= ?
	`
	args := []interface{}{threshold}
	if opts.File != "" {
		query += " AND n.file LIKE ?"
		args = append(args, "%"+opts.File+"%")
	}
	query += " ORDER BY fc.cyclomatic DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.repo.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FunctionComplexity
	for rows.Next() {
		var fc FunctionComplexity
		if err := rows.Scan(&fc.NodeID, &fc.Name, &fc.File, &fc.Line, &fc.Cyclomatic, &fc.Cognitive, &fc.Nesting, &fc.LOC); err != nil {
			return nil, err
		}
		results = append(results, fc)
	}
	return results, nil
}