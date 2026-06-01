// Package embedding provides embedding client interfaces and implementations.
package embedding

import "context"

// Embedder defines the interface for embedding providers.
type Embedder interface {
	// Embed generates embeddings for the given texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// EmbeddingDimension returns the dimension of the embeddings.
	EmbeddingDimension() int

	// ModelName returns the name of the embedding model.
	ModelName() string
}