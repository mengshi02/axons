package core

import (
	"github.com/mengshi02/axons/internal/analysis"
	"github.com/mengshi02/axons/internal/db/repository"
)

// CoChangeService handles co-change analysis.
type CoChangeService struct {
	repo *repository.Repository
}

// NewCoChangeService creates a new CoChangeService.
func NewCoChangeService(repo *repository.Repository) *CoChangeService {
	return &CoChangeService{repo: repo}
}

// CoChangeOptions holds co-change analysis options.
type CoChangeOptions struct {
	RootDir    string
	Since      string
	MinSupport int
	MinJaccard float64
	Full       bool
	Limit      int
	NoTests    bool
	File       string
}

// CoChangePair represents two files that frequently change together.
type CoChangePair struct {
	FileA       string
	FileB       string
	CommitCount int
	Jaccard     float64
}

// Analyze runs co-change analysis, optionally scanning git history first.
func (s *CoChangeService) Analyze(opts *CoChangeOptions) ([]CoChangePair, error) {
	if opts.RootDir != "" {
		// Run git scan
		analyzer := analysis.NewCoChangeAnalyzer(s.repo, opts.RootDir)
		aopts := &analysis.CoChangeOptions{
			Since:      opts.Since,
			MinSupport: opts.MinSupport,
		}
		if err := analyzer.Analyze(aopts); err != nil {
			return nil, err
		}
	}
	return s.Query(opts)
}

// Query reads existing co-change data from DB.
func (s *CoChangeService) Query(opts *CoChangeOptions) ([]CoChangePair, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	minSupport := opts.MinSupport
	if minSupport <= 0 {
		minSupport = 3
	}

	pairs, err := s.repo.GetTopCoChanges(limit, minSupport)
	if err != nil {
		return nil, err
	}

	results := make([]CoChangePair, 0, len(pairs))
	for _, p := range pairs {
		if opts.MinJaccard > 0 && p.Jaccard < opts.MinJaccard {
			continue
		}
		if opts.File != "" && p.FileA != opts.File && p.FileB != opts.File {
			continue
		}
		results = append(results, CoChangePair{
			FileA:       p.FileA,
			FileB:       p.FileB,
			CommitCount: p.CommitCount,
			Jaccard:     p.Jaccard,
		})
	}
	return results, nil
}