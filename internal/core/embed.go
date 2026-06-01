package core

import (
	"context"
	"fmt"
	"os"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/service"
	"github.com/mengshi02/axons/pkg/clients/embedding"
)

// EmbedService handles embedding generation directly against the local DB.
type EmbedService struct {
	repo *repository.Repository
	svc  *service.EmbeddingService
}

// NewEmbedService creates a new EmbedService.
func NewEmbedService(repo *repository.Repository) *EmbedService {
	svc := service.NewEmbeddingService(repo, nil)
	return &EmbedService{repo: repo, svc: svc}
}

// EmbedOptions holds embedding options.
type EmbedOptions struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
	Strategy string // "incremental" or "full"
	Batch    int
}

// EmbedResult holds embedding result summary.
type EmbedResult struct {
	Total        int
	NewCount     int
	UpdatedCount int
}

// Embed generates embeddings using the provided options.
func (s *EmbedService) Embed(ctx context.Context, opts *EmbedOptions) (*EmbedResult, error) {
	embedder := buildEmbedder(opts)
	if embedder == nil {
		return nil, fmt.Errorf("no embedding provider configured (set --provider or OPENAI_API_KEY)")
	}
	s.svc.SetEmbedder(embedder)

	force := opts.Strategy == "full"
	progressChan := make(chan service.Progress, 10)
	go func() {
		for range progressChan {
		}
	}()

	prog, err := s.svc.GenerateEmbeddings(ctx, force, nil, progressChan)
	close(progressChan)
	if err != nil {
		return nil, err
	}
	return &EmbedResult{
		Total:        prog.Total,
		NewCount:     prog.NewCount,
		UpdatedCount: prog.UpdatedCount,
	}, nil
}

// buildEmbedder creates the appropriate embedder from options.
func buildEmbedder(opts *EmbedOptions) embedding.Embedder {
	provider := opts.Provider
	model := opts.Model
	baseURL := opts.BaseURL
	apiKey := opts.APIKey

	switch provider {
	case "openai":
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil
		}
		if model == "" {
			model = "text-embedding-3-small"
		}
		return embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
			APIKey:  apiKey,
			Model:   model,
			BaseURL: baseURL,
		})
	case "ollama", "":
		if baseURL == "" {
			baseURL = os.Getenv("OLLAMA_BASE_URL")
		}
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		if model == "" {
			model = "nomic-embed-text"
		}
		return embedding.NewOllamaEmbedder(baseURL, model)
	}
	return nil
}