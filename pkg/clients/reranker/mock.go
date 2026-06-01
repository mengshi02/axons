package reranker

import (
	"context"
	"math/rand"
	"strings"
)

// MockReranker implements Reranker for testing and offline scenarios.
// It uses simple text similarity heuristics for ranking without external API calls.
type MockReranker struct {
	// Configuration
	useRandomScores bool
	seed            int64
	rng             *rand.Rand
}

// MockConfig contains configuration for Mock reranker.
type MockConfig struct {
	UseRandomScores bool  `json:"use_random_scores"`
	Seed            int64 `json:"seed"`
}

// DefaultMockConfig is the default Mock configuration.
var DefaultMockConfig = MockConfig{
	UseRandomScores: false,
	Seed:            42,
}

// NewMockReranker creates a new Mock reranker.
func NewMockReranker(config MockConfig) *MockReranker {
	if config.Seed == 0 {
		config.Seed = DefaultMockConfig.Seed
	}

	return &MockReranker{
		useRandomScores: config.UseRandomScores,
		seed:            config.Seed,
		rng:             rand.New(rand.NewSource(config.Seed)),
	}
}

// Rerank reranks documents using simple text similarity heuristics.
func (r *MockReranker) Rerank(ctx context.Context, query string, documents []string) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	results := make([]RerankResult, len(documents))
	scores := make([]float32, len(documents))

	for i, doc := range documents {
		results[i] = RerankResult{
			Index:    i,
			Document: doc,
		}

		if r.useRandomScores {
			// Use random scores for testing
			scores[i] = r.rng.Float32()
		} else {
			// Use simple text similarity
			scores[i] = r.calculateSimilarity(queryWords, strings.ToLower(doc))
		}
	}

	// Sort by score descending (simple bubble sort for small lists)
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if scores[j] > scores[i] {
				scores[i], scores[j] = scores[j], scores[i]
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Assign final scores
	for i := range results {
		results[i].Score = scores[i]
	}

	return results, nil
}

// RerankWithIDs reranks documents with their IDs.
func (r *MockReranker) RerankWithIDs(ctx context.Context, query string, docs []RerankDocument) ([]RerankResult, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	// Extract documents
	documents := make([]string, len(docs))
	for i, d := range docs {
		documents[i] = d.Text
	}

	// Rerank
	results, err := r.Rerank(ctx, query, documents)
	if err != nil {
		return nil, err
	}

	// Add IDs to results
	for i := range results {
		idx := results[i].Index
		if idx >= 0 && idx < len(docs) {
			results[i].ID = docs[idx].ID
		}
	}

	return results, nil
}

// calculateSimilarity calculates a simple text similarity score.
func (r *MockReranker) calculateSimilarity(queryWords []string, docLower string) float32 {
	if len(queryWords) == 0 {
		return 0
	}

	matches := 0
	for _, word := range queryWords {
		if strings.Contains(docLower, word) {
			matches++
		}
	}

	// Simple TF-based score
	score := float32(matches) / float32(len(queryWords))

	// Boost for exact phrase match
	if strings.Contains(docLower, strings.Join(queryWords, " ")) {
		score += 0.3
		if score > 1 {
			score = 1
		}
	}

	return score
}

// SetUseRandomScores enables or disables random scoring mode.
func (r *MockReranker) SetUseRandomScores(use bool) {
	r.useRandomScores = use
}

// ResetSeed resets the random number generator seed.
func (r *MockReranker) ResetSeed(seed int64) {
	r.seed = seed
	r.rng = rand.New(rand.NewSource(seed))
}