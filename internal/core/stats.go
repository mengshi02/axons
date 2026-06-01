package core

import (
	"github.com/mengshi02/axons/internal/db/repository"
)

// StatsService provides code graph statistics.
type StatsService struct {
	repo *repository.Repository
}

// NewStatsService creates a new StatsService.
func NewStatsService(repo *repository.Repository) *StatsService {
	return &StatsService{repo: repo}
}

// Stats returns graph statistics from the local DB.
func (s *StatsService) Stats() (*repository.Stats, error) {
	return s.repo.GetStats()
}

// ListFiles returns all indexed files.
func (s *StatsService) ListFiles() ([]string, error) {
	return s.repo.GetAllFiles()
}

// CountNodes returns the total node count.
func (s *StatsService) CountNodes() (int64, error) {
	return s.repo.CountNodes()
}
