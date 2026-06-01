package cce

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/pkg/clients/embedding"
)

// ContextRetriever performs context-aware retrieval combining semantic search,
// keyword search, and graph expansion. It orchestrates multiple retrieval
// strategies and merges results using Reciprocal Rank Fusion (RRF).
type ContextRetriever struct {
	store    *Store
	repo     *repository.Repository
	embedder embedding.Embedder
	rootPath string
	mu       sync.RWMutex
}

// NewContextRetriever creates a new context-aware retriever.
func NewContextRetriever(store *Store, repo *repository.Repository, embedder embedding.Embedder, rootPath string) *ContextRetriever {
	return &ContextRetriever{
		store:    store,
		repo:     repo,
		embedder: embedder,
		rootPath: rootPath,
	}
}

// SetEmbedder updates the embedder.
func (r *ContextRetriever) SetEmbedder(embedder embedding.Embedder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embedder = embedder
}

// Retrieve performs context-aware retrieval based on the query and template.
func (r *ContextRetriever) Retrieve(ctx context.Context, query *RetrievalQuery) ([]*RetrievalResult, error) {
	templateConfig := GetTemplate(query.Template)

	// Apply template defaults if not specified in query
	maxResults := query.MaxResults
	if maxResults <= 0 {
		maxResults = templateConfig.MaxResults
	}
	minScore := query.MinScore
	if minScore <= 0 {
		minScore = templateConfig.MinScore
	}

	// Collect results from multiple sources
	var allResults []*RetrievalResult

	// 1. Semantic search (description embeddings)
	if r.embedder != nil {
		semResults, err := r.semanticSearch(ctx, query.Query, maxResults, minScore)
		if err != nil {
			logger.S().Warnw("[CCE] semantic search failed", "error", err)
		} else {
			allResults = append(allResults, semResults...)
		}

		// 2. Code semantic search (code embeddings)
		codeResults, err := r.codeSemanticSearch(ctx, query.Query, maxResults, minScore)
		if err != nil {
			logger.S().Warnw("[CCE] code semantic search failed", "error", err)
		} else {
			allResults = append(allResults, codeResults...)
		}
	}

	// 3. Keyword search (FTS5)
	kwResults, err := r.keywordSearch(query.Query, maxResults)
	if err != nil {
		logger.S().Warnw("[CCE] keyword search failed", "error", err)
	} else {
		allResults = append(allResults, kwResults...)
	}

	// 4. Graph expansion from anchors
	if len(query.Anchors) > 0 {
		graphResults, err := r.graphExpansion(query.Anchors, templateConfig)
		if err != nil {
			logger.S().Warnw("[CCE] graph expansion failed", "error", err)
		} else {
			allResults = append(allResults, graphResults...)
		}
	}

	// 5. Apply filters
	allResults = r.applyFilters(allResults, query)

	// 6. Deduplicate and merge scores using RRF
	merged := r.mergeWithRRF(allResults, 60)

	// 7. Sort by merged score and limit
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})

	if len(merged) > maxResults {
		merged = merged[:maxResults]
	}

	// Log pre-enrichment stats
	contentFilled := 0
	for _, res := range merged {
		if res.Content != "" {
			contentFilled++
		}
	}
	logger.S().Debugw("[CCE] Pre-enrichment stats",
		"total", len(merged),
		"content_filled", contentFilled)

	// 8. Enrich results with source code content
	r.enrichWithContent(merged)

	return merged, nil
}

// semanticSearch performs vector similarity search using description embeddings.
func (r *ContextRetriever) semanticSearch(ctx context.Context, queryText string, limit int, threshold float32) ([]*RetrievalResult, error) {
	r.mu.RLock()
	embedder := r.embedder
	r.mu.RUnlock()

	if embedder == nil {
		return nil, nil
	}

	vectors, err := embedder.Embed(ctx, []string{queryText})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}

	// Use existing repository semantic search
	semResults, err := r.repo.SemanticSearch(vectors[0], limit, threshold)
	if err != nil {
		return nil, err
	}

	results := make([]*RetrievalResult, len(semResults))
	for i, sr := range semResults {
		results[i] = &RetrievalResult{
			NodeID:  sr.NodeID,
			Name:    sr.Name,
			Kind:    sr.Kind,
			File:    sr.File,
			Line:    sr.Line,
			EndLine: sr.EndLine,
			Score:   sr.Score,
			Source:  "semantic_desc",
		}
	}
	return results, nil
}

// codeSemanticSearch performs vector similarity search using code embeddings.
func (r *ContextRetriever) codeSemanticSearch(ctx context.Context, queryText string, limit int, threshold float32) ([]*RetrievalResult, error) {
	r.mu.RLock()
	embedder := r.embedder
	r.mu.RUnlock()

	if embedder == nil {
		return nil, nil
	}

	vectors, err := embedder.Embed(ctx, []string{queryText})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}

	return r.store.CodeSemanticSearch(vectors[0], limit, threshold)
}

// keywordSearch performs FTS5 keyword search.
func (r *ContextRetriever) keywordSearch(queryText string, limit int) ([]*RetrievalResult, error) {
	// Sanitize query for FTS5: remove special characters that cause syntax errors
	cleaned := sanitizeFTS5Query(queryText)
	if cleaned == "" {
		return nil, nil
	}
	ftsResults, err := r.repo.FTS5Search(cleaned, limit)
	if err != nil {
		return nil, err
	}

	results := make([]*RetrievalResult, len(ftsResults))
	for i, fr := range ftsResults {
		// Normalize BM25 score to 0-1 range
		score := float32(1.0 / (1.0 + fr.BM25Score))
		if score < 0 {
			score = -score
		}
		if score > 1.0 {
			score = 1.0
		}
		results[i] = &RetrievalResult{
			NodeID:  fr.NodeID,
			Name:    fr.Name,
			Kind:    fr.Kind,
			File:    fr.File,
			Line:    fr.Line,
			EndLine: fr.EndLine,
			Score:   score,
			Source:  "keyword",
		}
	}
	return results, nil
}

// graphExpansion retrieves related symbols via graph traversal from anchors.
func (r *ContextRetriever) graphExpansion(anchors []Anchor, config *TemplateConfig) ([]*RetrievalResult, error) {
	var allResults []*RetrievalResult
	seen := make(map[int64]bool)

	for _, anchor := range anchors {
		if anchor.NodeID <= 0 {
			continue
		}

		neighbors, err := r.store.GetNeighborsByEdgeTypes(anchor.NodeID, config.EdgeTypes, config.GraphDepth)
		if err != nil {
			logger.S().Warnw("[CCE] graph expansion from anchor failed", "anchor_node_id", anchor.NodeID, "error", err)
			continue
		}

		for _, nr := range neighbors {
			if !seen[nr.NodeID] {
				seen[nr.NodeID] = true
				nr.Source = "graph"
				allResults = append(allResults, nr)
			}
		}
	}

	return allResults, nil
}

// applyFilters applies query filters to results.
func (r *ContextRetriever) applyFilters(results []*RetrievalResult, query *RetrievalQuery) []*RetrievalResult {
	var filtered []*RetrievalResult
	for _, result := range results {
		// File filter
		if query.FileFilter != "" && !matchFilePattern(result.File, query.FileFilter) {
			continue
		}
		// Exclude test files
		if query.NoTests && isTestFile(result.File) {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered
}

// mergeWithRRF merges results from multiple sources using Reciprocal Rank Fusion.
func (r *ContextRetriever) mergeWithRRF(results []*RetrievalResult, k int) []*RetrievalResult {
	// Group by node ID, track per-source ranks
	type nodeScores struct {
		result   *RetrievalResult
		rrfScore float32
		sources  map[string]int // source -> rank
	}

	nodeMap := make(map[int64]*nodeScores)

	// Sort results by source then by score to determine per-source ranks
	sourceGroups := make(map[string][]*RetrievalResult)
	for _, r := range results {
		sourceGroups[r.Source] = append(sourceGroups[r.Source], r)
	}

	for source, group := range sourceGroups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].Score > group[j].Score
		})

		for rank, r := range group {
			if _, ok := nodeMap[r.NodeID]; !ok {
				nodeMap[r.NodeID] = &nodeScores{
					result:  r,
					sources: make(map[string]int),
				}
			}
			nodeMap[r.NodeID].rrfScore += 1.0 / float32(k+rank+1)
			nodeMap[r.NodeID].sources[source] = rank + 1
		}
	}

	// Build merged results
	merged := make([]*RetrievalResult, 0, len(nodeMap))
	for _, ns := range nodeMap {
		ns.result.Score = ns.rrfScore
		// Track which sources contributed
		var sourceList []string
		for s := range ns.sources {
			sourceList = append(sourceList, s)
		}
		ns.result.Source = strings.Join(sourceList, "+")
		merged = append(merged, ns.result)
	}

	return merged
}

// enrichWithContent fills in the Content field from code_chunks or source files.
func (r *ContextRetriever) enrichWithContent(results []*RetrievalResult) {
	if len(results) == 0 {
		return
	}

	// Batch load code chunks
	nodeIDs := make([]int64, len(results))
	for i, res := range results {
		nodeIDs[i] = res.NodeID
	}

	chunks, err := r.store.GetCodeChunksForNodes(nodeIDs)
	if err != nil {
		logger.S().Warnw("[CCE] enrichWithContent: GetCodeChunksForNodes failed, falling back to file read",
			"error", err, "node_count", len(nodeIDs))
		// Fallback: load source code directly from files
		for _, result := range results {
			src := r.readSourceFile(result.File, result.Line, result.EndLine)
			if src != "" {
				result.Content = src
			}
		}
		return
	}

	enrichedCount := 0
	fallbackCount := 0
	for _, result := range results {
		if chunk, ok := chunks[result.NodeID]; ok && chunk.Content != "" {
			result.Content = chunk.Content
			enrichedCount++
		} else {
			// Fallback: read source file directly
			src := r.readSourceFile(result.File, result.Line, result.EndLine)
			if src != "" {
				result.Content = src
				enrichedCount++
				fallbackCount++
			}
		}
	}
	logger.S().Debugw("[CCE] enrichWithContent complete",
		"total_results", len(results),
		"chunks_found", len(chunks),
		"enriched", enrichedCount,
		"fallback", fallbackCount)
}

// readSourceFile reads source code from the file system using rootPath.
func (r *ContextRetriever) readSourceFile(file string, startLine, endLine int) string {
	if file == "" || startLine <= 0 {
		return ""
	}

	absPath := file
	if !filepath.IsAbs(file) && r.rootPath != "" {
		absPath = filepath.Join(r.rootPath, file)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")
	if endLine <= 0 || endLine < startLine {
		endLine = startLine
	}
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	return strings.Join(lines[startLine-1:endLine], "\n")
}

// matchFilePattern checks if a file path matches a pattern.
func matchFilePattern(file, pattern string) bool {
	return strings.Contains(strings.ToLower(file), strings.ToLower(pattern))
}

// isTestFile checks if a file is a test file.
func isTestFile(file string) bool {
	lower := strings.ToLower(file)
	return strings.Contains(lower, "_test.go") ||
		strings.Contains(lower, "_test.py") ||
		strings.Contains(lower, "_test.js") ||
		strings.Contains(lower, ".test.ts") ||
		strings.Contains(lower, ".spec.ts") ||
		strings.Contains(lower, "/test/") ||
		strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/__tests__/")
}

// fts5SpecialChars matches characters that have special meaning in FTS5 queries.
var fts5SpecialChars = regexp.MustCompile(`[^\w\s]`)

// sanitizeFTS5Query removes or escapes FTS5 special characters from a query string.
// FTS5 does not support brackets, colons, quotes, etc. in plain queries.
func sanitizeFTS5Query(query string) string {
	// Remove all special characters, keep only word chars and spaces
	cleaned := fts5SpecialChars.ReplaceAllString(query, " ")
	// Collapse multiple spaces
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return cleaned
}