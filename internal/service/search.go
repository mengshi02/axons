// Package search provides search functionality for code symbols.
package service

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/clients/embedding"
	"github.com/mengshi02/axons/pkg/clients/reranker"
)

// RerankerPlugin is an alias for reranker.Reranker for backward compatibility.
type RerankerPlugin = reranker.Reranker

// Mode represents the search mode.
type Mode string

const (
	ModeHybrid   Mode = "hybrid"   // BM25 + semantic with RRF fusion
	ModeSemantic Mode = "semantic" // Pure vector similarity
	ModeKeyword  Mode = "keyword"  // Simple LIKE-based matching
)

// Request represents a search request.
type Request struct {
	Query    string  `json:"query"`
	Mode     Mode    `json:"mode"`
	Limit    int     `json:"limit"`
	MinScore float32 `json:"min_score"`
	Kind     string  `json:"kind"`     // Filter by symbol kind
	File     string  `json:"file"`     // Filter by file path pattern
	NoTests  bool    `json:"no_tests"` // Exclude test files
}

// Result represents a search result.
type Result struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	File          string  `json:"file"`
	Line          int     `json:"line"`
	EndLine       int     `json:"end_line,omitempty"`
	QualifiedName string  `json:"qualified_name,omitempty"`
	Score         float32 `json:"score"`         // Similarity score (0-1)
	RRFScore      float32 `json:"rrf_score"`     // RRF fusion score for hybrid
	BM25Score     float32 `json:"bm25_score"`    // BM25 score for keyword/hybrid
	BM25Rank      int     `json:"bm25_rank"`     // Rank in BM25 results
	SemanticRank  int     `json:"semantic_rank"` // Rank in semantic results
}

// Response represents a search response.
type Response struct {
	Results []*Result `json:"results"`
	Total   int       `json:"total"`
	Message string    `json:"message,omitempty"`
}

// Service provides search functionality.
type SearchService struct {
	repo     *repository.Repository
	embedder embedding.Embedder
	config   *HybridConfig
	reranker RerankerPlugin
}

// NewService creates a new search service.
func NewSearchService(repo *repository.Repository, embedder embedding.Embedder) *SearchService {
	return &SearchService{
		repo:     repo,
		embedder: embedder,
	}
}

// NewServiceWithConfig creates a new search service with configuration.
func NewSearchServiceWithConfig(repo *repository.Repository, embedder embedding.Embedder, config *HybridConfig) *SearchService {
	return &SearchService{
		repo:     repo,
		embedder: embedder,
		config:   config,
	}
}

// SetEmbedder sets the embedder.
func (s *SearchService) SetEmbedder(embedder embedding.Embedder) {
	s.embedder = embedder
}

// SetReranker sets the reranker.
func (s *SearchService) SetReranker(reranker RerankerPlugin) {
	s.reranker = reranker
}

// GetReranker returns the current reranker.
func (s *SearchService) GetReranker() RerankerPlugin {
	return s.reranker
}

// Search performs a search with the given request.
func (s *SearchService) Search(ctx context.Context, req *Request) (*Response, error) {
	// Set defaults
	if req.Mode == "" {
		req.Mode = ModeHybrid
	}
	if req.Limit <= 0 {
		req.Limit = 15
	}
	if req.MinScore <= 0 {
		req.MinScore = 0.2
	}

	switch req.Mode {
	case ModeKeyword:
		return s.keywordSearch(req)
	case ModeSemantic:
		return s.semanticSearch(ctx, req)
	case ModeHybrid:
		return s.hybridSearch(ctx, req)
	default:
		return s.hybridSearch(ctx, req)
	}
}

// keywordSearch performs simple LIKE-based keyword search.
func (s *SearchService) keywordSearch(req *Request) (*Response, error) {
	nodes, err := s.repo.SearchNodes(req.Query, req.Limit*2)
	if err != nil {
		return nil, err
	}

	// Convert nodes to results
	results := make([]*Result, 0, len(nodes))
	for i, n := range nodes {
		// Apply filters
		if req.Kind != "" && string(n.Kind) != req.Kind {
			continue
		}
		if req.File != "" && !matchFilePattern(n.File, req.File) {
			continue
		}
		if req.NoTests && isTestFile(n.File) {
			continue
		}

		results = append(results, &Result{
			ID:            n.ID,
			Name:          n.Name,
			Kind:          string(n.Kind),
			File:          n.File,
			Line:          n.Line,
			EndLine:       n.EndLine,
			QualifiedName: n.QualifiedName,
			Score:         1.0 - float32(i)*0.01,
			BM25Rank:      i + 1,
		})
	}

	// Limit results
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	return &Response{
		Results: results,
		Total:   len(results),
	}, nil
}

// semanticSearch performs vector similarity search.
func (s *SearchService) semanticSearch(ctx context.Context, req *Request) (*Response, error) {
	// Check if embedder is available
	if s.embedder == nil {
		return &Response{
			Results: []*Result{},
			Total:   0,
			Message: "Semantic search requires embedding model. Run 'axons embed' first.",
		}, nil
	}

	// Get query embedding
	vectors, err := s.embedder.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return &Response{
			Results: []*Result{},
			Total:   0,
		}, nil
	}

	// Perform semantic search
	semResults, err := s.repo.SemanticSearch(vectors[0], req.Limit*2, req.MinScore)
	if err != nil {
		return nil, err
	}

	// Convert results
	results := make([]*Result, 0, len(semResults))
	for i, r := range semResults {
		// Apply filters
		if req.Kind != "" && r.Kind != req.Kind {
			continue
		}
		if req.File != "" && !matchFilePattern(r.File, req.File) {
			continue
		}
		if req.NoTests && isTestFile(r.File) {
			continue
		}

		results = append(results, &Result{
			ID:            r.NodeID,
			Name:          r.Name,
			Kind:          r.Kind,
			File:          r.File,
			Line:          r.Line,
			EndLine:       r.EndLine,
			QualifiedName: r.QualifiedName,
			Score:         r.Score,
			SemanticRank:  i + 1,
		})
	}

	// Limit results
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	return &Response{
		Results: results,
		Total:   len(results),
	}, nil
}

// hybridSearch combines BM25 keyword search with semantic search using RRF fusion.
func (s *SearchService) hybridSearch(ctx context.Context, req *Request) (*Response, error) {
	// RRF k parameter
	k := 60.0

	// Run both searches in parallel
	type keywordResult struct {
		nodes []*repository.SemanticSearchResult
		err   error
	}
	type semanticResult struct {
		results []*repository.SemanticSearchResult
		err     error
	}

	keywordCh := make(chan keywordResult, 1)
	semanticCh := make(chan semanticResult, 1)

	// Keyword search (using SearchNodes converted to SemanticSearchResult)
	go func() {
		nodes, err := s.repo.SearchNodes(req.Query, req.Limit*3)
		if err != nil {
			keywordCh <- keywordResult{err: err}
			return
		}
		// Convert to SemanticSearchResult format
		results := make([]*repository.SemanticSearchResult, len(nodes))
		for i, n := range nodes {
			results[i] = &repository.SemanticSearchResult{
				NodeID:        n.ID,
				Name:          n.Name,
				Kind:          string(n.Kind),
				File:          n.File,
				Line:          n.Line,
				EndLine:       n.EndLine,
				QualifiedName: n.QualifiedName,
				Score:         1.0 - float32(i)*0.01, // Decreasing score by rank
			}
		}
		keywordCh <- keywordResult{nodes: results}
	}()

	// Semantic search
	go func() {
		if s.embedder == nil {
			semanticCh <- semanticResult{results: nil}
			return
		}
		vectors, err := s.embedder.Embed(ctx, []string{req.Query})
		if err != nil {
			semanticCh <- semanticResult{err: err}
			return
		}
		if len(vectors) == 0 {
			semanticCh <- semanticResult{results: nil}
			return
		}
		results, err := s.repo.SemanticSearch(vectors[0], req.Limit*3, req.MinScore)
		semanticCh <- semanticResult{results: results, err: err}
	}()

	// Collect results
	kwRes := <-keywordCh
	semRes := <-semanticCh

	// Handle errors
	if kwRes.err != nil && semRes.err != nil {
		return nil, kwRes.err
	}

	// If no semantic results, fall back to keyword only
	if len(semRes.results) == 0 {
		resp, err := s.keywordSearch(req)
		if err != nil {
			return nil, err
		}
		resp.Message = "Semantic search unavailable. Showing keyword results only."
		return resp, nil
	}

	// RRF fusion
	fusionMap := make(map[int64]*Result)

	// Add keyword results
	for i, r := range kwRes.nodes {
		// Apply filters
		if req.Kind != "" && r.Kind != req.Kind {
			continue
		}
		if req.File != "" && !matchFilePattern(r.File, req.File) {
			continue
		}
		if req.NoTests && isTestFile(r.File) {
			continue
		}

		rrfScore := float32(1.0 / (k + float64(i+1)))
		fusionMap[r.NodeID] = &Result{
			ID:            r.NodeID,
			Name:          r.Name,
			Kind:          r.Kind,
			File:          r.File,
			Line:          r.Line,
			EndLine:       r.EndLine,
			QualifiedName: r.QualifiedName,
			RRFScore:      rrfScore,
			BM25Score:     r.Score,
			BM25Rank:      i + 1,
			Score:         r.Score,
		}
	}

	// Add semantic results
	for i, r := range semRes.results {
		// Apply filters
		if req.Kind != "" && r.Kind != req.Kind {
			continue
		}
		if req.File != "" && !matchFilePattern(r.File, req.File) {
			continue
		}
		if req.NoTests && isTestFile(r.File) {
			continue
		}

		rrfScore := float32(1.0 / (k + float64(i+1)))
		if existing, ok := fusionMap[r.NodeID]; ok {
			// Merge scores
			existing.RRFScore += rrfScore
			existing.SemanticRank = i + 1
			existing.Score = r.Score // Use semantic score as primary
		} else {
			fusionMap[r.NodeID] = &Result{
				ID:            r.NodeID,
				Name:          r.Name,
				Kind:          r.Kind,
				File:          r.File,
				Line:          r.Line,
				EndLine:       r.EndLine,
				QualifiedName: r.QualifiedName,
				RRFScore:      rrfScore,
				Score:         r.Score,
				SemanticRank:  i + 1,
			}
		}
	}

	// Sort by RRF score
	results := make([]*Result, 0, len(fusionMap))
	for _, r := range fusionMap {
		results = append(results, r)
	}

	// Sort by RRF score descending
	sortByRRF(results)

	// Limit results
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	return &Response{
		Results: results,
		Total:   len(results),
	}, nil
}

// HybridConfig holds configuration for hybrid search.
type HybridConfig struct {
	// RRF parameters
	RRFK float64 `json:"rrf_k"` // RRF constant (default: 60)

	// Search limits
	KeywordLimit int `json:"keyword_limit"` // Limit for keyword search
	VectorLimit  int `json:"vector_limit"`  // Limit for vector search
	FinalLimit   int `json:"final_limit"`   // Final result limit

	// Thresholds
	MinScore float32 `json:"min_score"` // Minimum similarity score

	// Rerank settings
	RerankEnabled  bool   `json:"rerank_enabled"`
	RerankTopK     int    `json:"rerank_top_k"` // Candidates for reranking
	RerankProvider string `json:"rerank_provider"`
}

// DefaultHybridConfig returns the default hybrid search configuration.
func DefaultHybridConfig() *HybridConfig {
	return &HybridConfig{
		RRFK:          60.0,
		KeywordLimit:  50,
		VectorLimit:   50,
		FinalLimit:    10,
		MinScore:      0.2,
		RerankEnabled: false,
		RerankTopK:    20,
	}
}

// HybridSearchV2 performs hybrid search combining FTS5 BM25 and vector search.
// This is an enhanced version that uses FTS5 for keyword search instead of LIKE.
func (s *SearchService) HybridSearchV2(ctx context.Context, req *Request) (*Response, error) {
	config := s.getConfig()

	// Set defaults
	if req.Limit <= 0 {
		req.Limit = config.FinalLimit
	}
	if req.MinScore <= 0 {
		req.MinScore = config.MinScore
	}

	// Run FTS5 and vector search in parallel
	type ftsResult struct {
		results []*repository.FTS5SearchResult
		err     error
	}
	type vectorResult struct {
		results []*repository.SemanticSearchResult
		err     error
	}

	ftsCh := make(chan ftsResult, 1)
	vectorCh := make(chan vectorResult, 1)

	// FTS5 keyword search
	go func() {
		results, err := s.repo.FTS5SearchWithFilter(req.Query, req.Kind, req.File, config.KeywordLimit)
		ftsCh <- ftsResult{results: results, err: err}
	}()

	// Vector semantic search
	go func() {
		if s.embedder == nil {
			vectorCh <- vectorResult{results: nil}
			return
		}

		vectors, err := s.embedder.Embed(ctx, []string{req.Query})
		if err != nil {
			vectorCh <- vectorResult{err: err}
			return
		}
		if len(vectors) == 0 {
			vectorCh <- vectorResult{results: nil}
			return
		}

		results, err := s.repo.SemanticSearch(vectors[0], config.VectorLimit, req.MinScore)
		vectorCh <- vectorResult{results: results, err: err}
	}()

	// Collect results
	ftsRes := <-ftsCh
	vecRes := <-vectorCh

	// Handle errors
	if ftsRes.err != nil && vecRes.err != nil {
		return nil, ftsRes.err
	}

	// Fallback to FTS5 only if no vector results
	if len(vecRes.results) == 0 {
		return s.ftsSearchToResponse(ftsRes.results, req)
	}

	// RRF Fusion
	fusionMap := make(map[int64]*Result)
	k := config.RRFK

	// Process FTS5 results
	for i, r := range ftsRes.results {
		if req.NoTests && isTestFile(r.File) {
			continue
		}

		rrfScore := float32(1.0 / (k + float64(i+1)))
		fusionMap[r.NodeID] = &Result{
			ID:            r.NodeID,
			Name:          r.Name,
			Kind:          r.Kind,
			File:          r.File,
			Line:          r.Line,
			EndLine:       r.EndLine,
			QualifiedName: r.QualifiedName,
			RRFScore:      rrfScore,
			BM25Score:     float32(r.BM25Score),
			BM25Rank:      i + 1,
			Score:         rrfScore, // Use RRF score as primary
		}
	}

	// Process vector results
	for i, r := range vecRes.results {
		if req.Kind != "" && r.Kind != req.Kind {
			continue
		}
		if req.File != "" && !matchFilePattern(r.File, req.File) {
			continue
		}
		if req.NoTests && isTestFile(r.File) {
			continue
		}

		rrfScore := float32(1.0 / (k + float64(i+1)))
		if existing, ok := fusionMap[r.NodeID]; ok {
			// Merge scores
			existing.RRFScore += rrfScore
			existing.SemanticRank = i + 1
			existing.Score = existing.RRFScore // Update to combined RRF score
		} else {
			fusionMap[r.NodeID] = &Result{
				ID:            r.NodeID,
				Name:          r.Name,
				Kind:          r.Kind,
				File:          r.File,
				Line:          r.Line,
				EndLine:       r.EndLine,
				QualifiedName: r.QualifiedName,
				RRFScore:      rrfScore,
				Score:         rrfScore,
				SemanticRank:  i + 1,
			}
		}
	}

	// Convert to slice and sort
	results := make([]*Result, 0, len(fusionMap))
	for _, r := range fusionMap {
		results = append(results, r)
	}

	// Sort by RRF score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	// Limit results
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	return &Response{
		Results: results,
		Total:   len(results),
	}, nil
}

// FTS5KeywordSearch performs FTS5-based keyword search.
func (s *SearchService) FTS5KeywordSearch(req *Request) (*Response, error) {
	config := s.getConfig()

	if req.Limit <= 0 {
		req.Limit = config.FinalLimit
	}

	results, err := s.repo.FTS5SearchWithFilter(req.Query, req.Kind, req.File, req.Limit)
	if err != nil {
		return nil, err
	}

	return s.ftsSearchToResponse(results, req)
}

// ftsSearchToResponse converts FTS5 results to Response.
func (s *SearchService) ftsSearchToResponse(results []*repository.FTS5SearchResult, req *Request) (*Response, error) {
	var filteredResults []*Result

	for i, r := range results {
		if req.NoTests && isTestFile(r.File) {
			continue
		}

		filteredResults = append(filteredResults, &Result{
			ID:            r.NodeID,
			Name:          r.Name,
			Kind:          r.Kind,
			File:          r.File,
			Line:          r.Line,
			EndLine:       r.EndLine,
			QualifiedName: r.QualifiedName,
			BM25Score:     float32(r.BM25Score),
			BM25Rank:      i + 1,
			Score:         float32(r.BM25Score),
		})
	}

	if len(filteredResults) > req.Limit {
		filteredResults = filteredResults[:req.Limit]
	}

	return &Response{
		Results: filteredResults,
		Total:   len(filteredResults),
	}, nil
}

// GetSourceCode retrieves source code for search results.
func (s *SearchService) GetSourceCode(results []*Result) (map[int64]string, error) {
	nodeIDs := make([]int64, len(results))
	for i, r := range results {
		nodeIDs[i] = r.ID
	}
	return s.repo.GetSourceCodeForNodes(nodeIDs)
}

// getConfig returns the configuration for hybrid search.
func (s *SearchService) getConfig() *HybridConfig {
	if s.config != nil {
		return s.config
	}
	return DefaultHybridConfig()
}

// SetConfig sets the configuration for hybrid search.
func (s *SearchService) SetConfig(config *HybridConfig) {
	s.config = config
}

// ParallelSearch performs multiple search strategies in parallel and merges results.
func (s *SearchService) ParallelSearch(ctx context.Context, queries []string, mode Mode) (*Response, error) {
	var wg sync.WaitGroup
	resultsCh := make(chan *Response, len(queries))

	for _, query := range queries {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			resp, err := s.Search(ctx, &Request{
				Query: q,
				Mode:  mode,
				Limit: 10,
			})
			if err != nil {
				resultsCh <- &Response{Results: []*Result{}}
				return
			}
			resultsCh <- resp
		}(query)
	}

	wg.Wait()
	close(resultsCh)

	// Merge results using RRF
	fusionMap := make(map[int64]*Result)
	k := 60.0
	rank := 1

	for resp := range resultsCh {
		for _, r := range resp.Results {
			rrfScore := float32(1.0 / (k + float64(rank)))
			if existing, ok := fusionMap[r.ID]; ok {
				existing.RRFScore += rrfScore
				existing.Score = existing.RRFScore
			} else {
				r.RRFScore = rrfScore
				r.Score = rrfScore
				fusionMap[r.ID] = r
			}
		}
		rank++
	}

	// Sort and limit
	results := make([]*Result, 0, len(fusionMap))
	for _, r := range fusionMap {
		results = append(results, r)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	if len(results) > 20 {
		results = results[:20]
	}

	return &Response{
		Results: results,
		Total:   len(results),
	}, nil
}

// sortByRRF sorts results by RRF score in descending order.
func sortByRRF(results []*Result) {
	// Simple insertion sort for small lists
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].RRFScore > results[j-1].RRFScore; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}

// matchFilePattern checks if a file path matches a pattern.
// Supports glob patterns with * and ?.
func matchFilePattern(filePath, pattern string) bool {
	// Simple pattern matching
	if strings.Contains(pattern, "*") || strings.Contains(pattern, "?") {
		// Convert glob to regex
		regex := globToRegex(pattern)
		matched, _ := regexp.MatchString(regex, filePath)
		return matched
	}
	// Simple substring match
	return strings.Contains(filePath, pattern)
}

// globToRegex converts a glob pattern to a regex pattern.
func globToRegex(glob string) string {
	var result strings.Builder
	result.WriteString("^")
	for _, ch := range glob {
		switch ch {
		case '*':
			result.WriteString(".*")
		case '?':
			result.WriteString(".")
		case '.', '/', '\\', '(', ')', '[', ']', '{', '}', '^', '$', '+', '|':
			result.WriteRune('\\')
			result.WriteRune(ch)
		default:
			result.WriteRune(ch)
		}
	}
	result.WriteString("$")
	return result.String()
}

// isTestFile checks if a file is a test file.
func isTestFile(filePath string) bool {
	lower := strings.ToLower(filePath)
	return strings.Contains(lower, "_test.") ||
		strings.Contains(lower, ".test.") ||
		strings.Contains(lower, ".spec.") ||
		strings.Contains(lower, "_spec.") ||
		strings.Contains(lower, "__test__") ||
		strings.Contains(lower, "__tests__")
}
