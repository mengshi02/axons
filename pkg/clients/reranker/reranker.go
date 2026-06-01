// Package reranker provides reranker client interfaces and implementations.
package reranker

import "context"

// Reranker defines the interface for reranking providers.
type Reranker interface {
	// Rerank reranks documents against a query.
	// Returns a slice of RerankResult with index and score.
	Rerank(ctx context.Context, query string, documents []string) ([]RerankResult, error)

	// RerankWithIDs reranks documents with their IDs.
	// This is useful when you need to track document IDs.
	RerankWithIDs(ctx context.Context, query string, docs []RerankDocument) ([]RerankResult, error)
}

// RerankDocument represents a document for reranking.
type RerankDocument struct {
	ID       string                 `json:"id"`
	Text     string                 `json:"text"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// RerankResult represents a reranking result.
type RerankResult struct {
	Index    int     `json:"index"`
	ID       string  `json:"id,omitempty"`
	Score    float32 `json:"score"`
	Document string  `json:"document,omitempty"`
}