package core

import (
	"context"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/service"
	"github.com/mengshi02/axons/pkg/clients/embedding"
)

// SearchService wraps service.SearchService for use by CLI and daemon.
type SearchService struct {
	svc *service.SearchService
}

// NewSearchService creates a SearchService backed by the given repo and embedder.
// embedder may be nil for keyword-only search.
func NewSearchService(repo *repository.Repository, embedder embedding.Embedder) *SearchService {
	return &SearchService{
		svc: service.NewSearchService(repo, embedder),
	}
}

// SetEmbedder replaces the embedder at runtime.
func (s *SearchService) SetEmbedder(embedder embedding.Embedder) {
	s.svc.SetEmbedder(embedder)
}

// Search delegates to service.SearchService.
func (s *SearchService) Search(ctx context.Context, req *service.Request) (*service.Response, error) {
	return s.svc.Search(ctx, req)
}

// HybridSearchV2 delegates to service.SearchService.
func (s *SearchService) HybridSearchV2(ctx context.Context, req *service.Request) (*service.Response, error) {
	return s.svc.HybridSearchV2(ctx, req)
}

// FTS5KeywordSearch delegates to service.SearchService.
func (s *SearchService) FTS5KeywordSearch(req *service.Request) (*service.Response, error) {
	return s.svc.FTS5KeywordSearch(req)
}