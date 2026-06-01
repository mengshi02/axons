package cce

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/internal/logger"
	"github.com/mengshi02/axons/internal/version"
	"github.com/mengshi02/axons/pkg/clients/embedding"
)

// Engine is the main entry point for the Cognitive Context Engine.
// It orchestrates bimodal embedding, context-aware retrieval,
// and context assembly to provide high-quality code context.
type Engine struct {
	store     *Store
	repo      *repository.Repository
	embedder  embedding.Embedder
	retriever *ContextRetriever
	assembler *ContextAssembler
	bimodal   *BimodalEmbedder

	mu     sync.RWMutex
	status string // "idle", "running", "error"
}

// NewEngine creates a new CCE Engine.
func NewEngine(repo *repository.Repository, embedder embedding.Embedder, rootPath string) *Engine {
	store := NewStore(repo.DB())
	retriever := NewContextRetriever(store, repo, embedder, rootPath)
	assembler := NewContextAssembler()
	bimodal := NewBimodalEmbedder(store, embedder, rootPath)

	return &Engine{
		store:     store,
		repo:      repo,
		embedder:  embedder,
		retriever: retriever,
		assembler: assembler,
		bimodal:   bimodal,
		status:    "idle",
	}
}

// SetEmbedder updates the embedder across all sub-components.
func (e *Engine) SetEmbedder(embedder embedding.Embedder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.embedder = embedder
	e.retriever.SetEmbedder(embedder)
	e.bimodal.SetEmbedder(embedder)
}

// SetMaxContextTokens updates the max context tokens config for the bimodal embedder.
func (e *Engine) SetMaxContextTokens(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bimodal.SetMaxContextTokens(n)
}

// GetStore returns the CCE store.
func (e *Engine) GetStore() *Store {
	return e.store
}

// GetStatus returns the current engine status.
func (e *Engine) GetStatus() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.status
}

// --- Core Operations ---

// GetContext is the primary API: takes a query, retrieves, and assembles context.
// This is the main method called by chat handlers and MCP tools.
func (e *Engine) GetContext(ctx context.Context, query *RetrievalQuery) (*AssembledContext, error) {
	startTime := time.Now()

	logger.S().Infow("[CCE] GetContext starting",
		"query", query.Query,
		"template", query.Template,
		"anchors", len(query.Anchors))

	// Step 1: Retrieve relevant results
	results, err := e.retriever.Retrieve(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("CCE retrieval failed: %w", err)
	}

	retrievalTime := time.Since(startTime)

	logger.S().Infow("[CCE] Retrieval complete",
		"results", len(results),
		"retrieval_time_ms", retrievalTime.Milliseconds())

	// Step 2: Assemble context from results
	assembled := e.assembler.Assemble(results, query)
	assembled.Metadata.RetrievalTime = fmt.Sprintf("%d", retrievalTime.Milliseconds())

	logger.S().Infow("[CCE] Context assembled",
		"sections", len(assembled.Sections),
		"total_tokens", assembled.TotalTokens,
		"sources", len(assembled.Sources))

	return assembled, nil
}

// GetContextForChat is a convenience method for chat integration.
// It returns formatted context text and the CCE banner for display.
func (e *Engine) GetContextForChat(ctx context.Context, query string, projectID string) (string, string, error) {
	cceQuery := &RetrievalQuery{
		Query:      query,
		Template:   TemplateGeneral,
		MaxTokens:  4000,
		MaxResults: 15,
		MinScore:   0.15,
	}

	// If we can detect anchors from the query, add them
	// This is a simple heuristic - the agent can also provide anchors explicitly
	anchors := e.detectAnchors(query)
	if len(anchors) > 0 {
		cceQuery.Anchors = anchors
	}

	assembled, err := e.GetContext(ctx, cceQuery)
	if err != nil {
		return "", "", err
	}

	contextText := assembled.FormatContextForLLM()
	banner := FormatCCEBanner(cceQuery.Template, len(assembled.Sources))

	return contextText, banner, nil
}

// GenerateEmbeddings generates CCE embeddings for the given project.
func (e *Engine) GenerateEmbeddings(ctx context.Context, force bool, mode EmbeddingMode, kinds []string, progressCh chan<- EmbeddingProgress) (*EmbeddingProgress, error) {
	e.mu.Lock()
	if e.status == "running" {
		e.mu.Unlock()
		return nil, fmt.Errorf("CCE embedding already in progress")
	}
	e.status = "running"
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.status = "idle"
		e.mu.Unlock()
	}()

	// Get node IDs that need code embeddings
	var nodeIDs []int64
	var err error

	if force {
		// Get all nodes
		nodeIDs, err = e.getAllNodeIDs(kinds)
	} else {
		// Get only nodes without code embeddings
		nodeIDs, err = e.store.GetNodesWithoutCodeChunks(kinds)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get node IDs: %w", err)
	}

	if len(nodeIDs) == 0 {
		return &EmbeddingProgress{
			Mode:   mode,
			Status: "complete",
		}, nil
	}

	logger.S().Infow("[CCE] Starting embedding generation",
		"mode", mode,
		"nodes", len(nodeIDs),
		"force", force)

	return e.bimodal.GenerateDualEmbeddings(ctx, nodeIDs, force, mode, progressCh)
}

// GetStats returns CCE statistics.
func (e *Engine) GetStats() (map[string]interface{}, error) {
	descCount, codeCount, err := e.store.GetDualEmbeddingStats()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"description_embeddings": descCount,
		"code_embeddings":        codeCount,
		"cce_version":            version.Version,
		"status":                 e.GetStatus(),
	}, nil
}

// --- Helper Methods ---

// detectAnchors attempts to detect symbol references in the query.
func (e *Engine) detectAnchors(query string) []Anchor {
	// Simple heuristic: look for capitalized words or dotted names
	// A more sophisticated approach would use the existing search to find exact matches
	var anchors []Anchor

	// Try FTS5 search to find potential anchor symbols
	results, err := e.repo.FTS5Search(query, 3)
	if err != nil {
		return nil
	}

	for _, r := range results {
		anchors = append(anchors, Anchor{
			NodeID:     r.NodeID,
			SymbolName: r.Name,
			Kind:       r.Kind,
			File:       r.File,
		})
	}

	return anchors
}

// getAllNodeIDs returns all node IDs, optionally filtered by kind.
func (e *Engine) getAllNodeIDs(kinds []string) ([]int64, error) {
	query := "SELECT id FROM nodes"
	args := []interface{}{}

	if len(kinds) > 0 {
		query += " WHERE kind IN ("
		for i, kind := range kinds {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, kind)
		}
		query += ")"
	}

	rows, err := e.repo.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}