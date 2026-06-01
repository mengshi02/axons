package embedding

import (
	"context"
	"math/rand"
)

// NoopEmbedder is a no-operation embedder for testing.
// It generates random vectors without making any API calls.
type NoopEmbedder struct {
	dimension int
	model     string
}

// NewNoopEmbedder creates a new noop embedder for testing.
func NewNoopEmbedder(dimension int) *NoopEmbedder {
	if dimension <= 0 {
		dimension = 384 // Default to all-MiniLM-L6-v2 dimension
	}
	return &NoopEmbedder{
		dimension: dimension,
		model:     "noop-random",
	}
}

// Embed generates random embeddings for testing.
func (e *NoopEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	embeddings := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, e.dimension)
		for j := range vec {
			vec[j] = rand.Float32()*2 - 1 // Random value in [-1, 1]
		}
		embeddings[i] = vec
	}

	return embeddings, nil
}

// EmbeddingDimension returns the dimension of the embeddings.
func (e *NoopEmbedder) EmbeddingDimension() int {
	return e.dimension
}

// ModelName returns the name of the embedding model.
func (e *NoopEmbedder) ModelName() string {
	return e.model
}