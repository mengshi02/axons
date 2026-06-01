// Package service provides various services for the axons daemon.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/mengshi02/axons/internal/db/repository"
	"github.com/mengshi02/axons/pkg/clients/embedding"
)

// Status represents the status of an embedding operation.
type Status string

const (
	StatusIdle     Status = "idle"
	StatusRunning  Status = "running"
	StatusComplete Status = "complete"
	StatusError    Status = "error"
	StatusCanceled Status = "canceled"
)

// Progress represents embedding progress.
type Progress struct {
	Status       Status `json:"status"`
	Current      int    `json:"current"`
	Total        int    `json:"total"`
	Message      string `json:"message"`
	NewCount     int    `json:"new_count"`
	UpdatedCount int    `json:"updated_count"`
	Error        string `json:"error,omitempty"`
}

// Service provides embedding generation and management.
type EmbeddingService struct {
	repo     *repository.Repository
	embedder embedding.Embedder

	mu          sync.RWMutex
	status      Status
	progress    *Progress
	cancelFunc  context.CancelFunc
	lastModel   string
	modelConfig map[string]string
}

// NewEmbeddingService creates a new embedding service.
func NewEmbeddingService(repo *repository.Repository, embedder embedding.Embedder) *EmbeddingService {
	return &EmbeddingService{
		repo:     repo,
		embedder: embedder,
		status:   StatusIdle,
		progress: &Progress{Status: StatusIdle},
	}
}

// SetEmbedder updates the embedder.
func (s *EmbeddingService) SetEmbedder(embedder embedding.Embedder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embedder = embedder
}

// SetModelConfig sets the model configuration.
func (s *EmbeddingService) SetModelConfig(config map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelConfig = config
}

// GetStatus returns the current embedding status.
func (s *EmbeddingService) GetStatus() *Progress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.progress
}

// Cancel cancels any running embedding operation.
func (s *EmbeddingService) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFunc != nil && s.status == StatusRunning {
		s.cancelFunc()
		s.status = StatusCanceled
		s.progress = &Progress{
			Status:  StatusCanceled,
			Message: "Embedding canceled by user",
		}
	}
}

// GenerateEmbeddingsWithRepo generates embeddings using a specific repository (for project-scoped DBs).
// If repo is nil, falls back to the service's default repo.
func (s *EmbeddingService) GenerateEmbeddingsWithRepo(ctx context.Context, repo *repository.Repository, force bool, kinds []string, progressChan chan<- Progress) (*Progress, error) {
	if repo == nil {
		repo = s.repo
	}
	return s.generateEmbeddingsInternal(ctx, repo, force, kinds, progressChan)
}

// GenerateEmbeddings generates embeddings for all nodes that don't have them yet.
// If force is true, it regenerates all embeddings.
func (s *EmbeddingService) GenerateEmbeddings(ctx context.Context, force bool, kinds []string, progressChan chan<- Progress) (*Progress, error) {
	return s.generateEmbeddingsInternal(ctx, s.repo, force, kinds, progressChan)
}

func (s *EmbeddingService) generateEmbeddingsInternal(ctx context.Context, repo *repository.Repository, force bool, kinds []string, progressChan chan<- Progress) (*Progress, error) {
	s.mu.Lock()
	if s.status == StatusRunning {
		s.mu.Unlock()
		return nil, fmt.Errorf("embedding already in progress")
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel
	s.status = StatusRunning
	s.progress = &Progress{
		Status:  StatusRunning,
		Message: "Starting embedding generation...",
	}
	s.mu.Unlock()

	// vec_embeddings dimension will be ensured lazily after the first real
	// embedding batch returns, so we know the actual vector size.
	vecTableEnsured := false

	defer func() {
		s.mu.Lock()
		s.cancelFunc = nil
		if s.status != StatusCanceled {
			s.status = StatusIdle
		}
		s.mu.Unlock()
	}()

	// Get nodes to embed
	var nodeIDs []int64
	var err error

	if force {
		// Get all nodes of specified kinds
		nodeIDs, err = s.getNodeIDsByKindsWithRepo(repo, kinds)
	} else {
		// Get only nodes without embeddings
		nodeIDs, err = repo.GetNodesWithoutEmbeddings(kinds)
	}

	if err != nil {
		fmt.Printf("ERROR: failed to get node IDs: %v\n", err)
		s.mu.Lock()
		s.progress = &Progress{Status: StatusError, Error: err.Error(), Message: fmt.Sprintf("Failed to get nodes: %v", err)}
		s.mu.Unlock()
		return s.progress, err
	}

	if len(nodeIDs) == 0 {
		s.mu.Lock()
		s.progress = &Progress{
			Status:  StatusComplete,
			Message: "No nodes to embed",
		}
		s.mu.Unlock()
		return s.progress, nil
	}

	total := len(nodeIDs)
	newCount := 0
	updatedCount := 0
	batchSize := 50 // Process in batches

	for i := 0; i < total; i += batchSize {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.progress = &Progress{
				Status:  StatusCanceled,
				Message: "Embedding canceled",
			}
			s.mu.Unlock()
			return s.progress, nil
		default:
		}

		end := i + batchSize
		if end > total {
			end = total
		}

		batch := nodeIDs[i:end]

		// Get node content for embedding
		texts, err := s.getNodeTextsWithRepo(repo, batch)
		if err != nil {
			fmt.Printf("ERROR: getNodeTextsWithRepo failed: %v\n", err)
			s.mu.Lock()
			s.progress = &Progress{Status: StatusError, Error: err.Error(), Message: fmt.Sprintf("Failed to get node texts: %v", err)}
			s.mu.Unlock()
			return s.progress, err
		}

		// Generate embeddings
		vectors, err := s.embedder.Embed(ctx, texts)
		if err != nil {
			fmt.Printf("ERROR: embedder.Embed failed (batch %d-%d): %v\n", i, end, err)
			s.mu.Lock()
			s.progress = &Progress{Status: StatusError, Error: err.Error(), Message: fmt.Sprintf("Embed API error: %v", err)}
			s.mu.Unlock()
			return s.progress, err
		}

		// Ensure vec_embeddings has the correct dimension based on actual vector size.
		// Done lazily on first successful batch so custom/unknown models work correctly.
		if !vecTableEnsured && len(vectors) > 0 && len(vectors[0]) > 0 {
			dim := len(vectors[0])
			if ensureErr := repo.EnsureVecTable(dim); ensureErr != nil {
				fmt.Printf("warning: EnsureVecTable(dim=%d) failed: %v\n", dim, ensureErr)
			} else {
				fmt.Printf("info: EnsureVecTable(dim=%d) OK (actual vector size)\n", dim)
			}
			vecTableEnsured = true
		}

		// Store embeddings
		model := s.getModelName()
		for j, nodeID := range batch {
			if j < len(vectors) {
				// Check if new BEFORE storing (Bug fix: must check before store)
				isNew := s.isNewEmbeddingWithRepo(repo, nodeID)
				contentHash := s.computeHash(texts[j])
				err := s.storeEmbeddingWithHashRepo(repo, nodeID, vectors[j], model, texts[j], contentHash)
				if err != nil {
					// Log error but continue
					fmt.Printf("Failed to store embedding for node %d: %v\n", nodeID, err)
				} else {
					if isNew {
						newCount++
					} else {
						updatedCount++
					}
				}
			}
		}

		// Update progress
		s.mu.Lock()
		s.progress = &Progress{
			Status:       StatusRunning,
			Current:      end,
			Total:        total,
			Message:      fmt.Sprintf("Processing embeddings %d/%d", end, total),
			NewCount:     newCount,
			UpdatedCount: updatedCount,
		}
		s.mu.Unlock()

		if progressChan != nil {
			progressChan <- *s.progress
		}
	}

	s.mu.Lock()
	s.progress = &Progress{
		Status:       StatusComplete,
		Total:        total,
		Current:      total,
		Message:      "Embedding complete",
		NewCount:     newCount,
		UpdatedCount: updatedCount,
	}
	s.mu.Unlock()

	return s.progress, nil
}

// GenerateEmbeddingsForNodes generates embeddings for specific nodes.
func (s *EmbeddingService) GenerateEmbeddingsForNodes(ctx context.Context, nodeIDs []int64, progressChan chan<- Progress) (*Progress, error) {
	s.mu.Lock()
	if s.status == StatusRunning {
		s.mu.Unlock()
		return nil, fmt.Errorf("embedding already in progress")
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel
	s.status = StatusRunning
	s.progress = &Progress{
		Status:  StatusRunning,
		Message: "Starting embedding generation...",
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.cancelFunc = nil
		if s.status != StatusCanceled {
			s.status = StatusIdle
		}
		s.mu.Unlock()
	}()

	total := len(nodeIDs)
	newCount := 0
	updatedCount := 0
	batchSize := 50

	for i := 0; i < total; i += batchSize {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.progress = &Progress{Status: StatusCanceled, Message: "Embedding canceled"}
			s.mu.Unlock()
			return s.progress, nil
		default:
		}

		end := i + batchSize
		if end > total {
			end = total
		}

		batch := nodeIDs[i:end]

		texts, err := s.getNodeTexts(batch)
		if err != nil {
			s.mu.Lock()
			s.progress = &Progress{Status: StatusError, Error: err.Error()}
			s.mu.Unlock()
			return s.progress, err
		}

		vectors, err := s.embedder.Embed(ctx, texts)
		if err != nil {
			s.mu.Lock()
			s.progress = &Progress{Status: StatusError, Error: err.Error()}
			s.mu.Unlock()
			return s.progress, err
		}

		model := s.getModelName()
		for j, nodeID := range batch {
			if j < len(vectors) {
				contentHash := s.computeHash(texts[j])
				err := s.storeEmbeddingWithHash(nodeID, vectors[j], model, texts[j], contentHash)
				if err != nil {
					fmt.Printf("Failed to store embedding for node %d: %v\n", nodeID, err)
				} else {
					if s.isNewEmbedding(nodeID) {
						newCount++
					} else {
						updatedCount++
					}
				}
			}
		}

		s.mu.Lock()
		s.progress = &Progress{
			Status:       StatusRunning,
			Current:      end,
			Total:        total,
			Message:      fmt.Sprintf("Processing embeddings %d/%d", end, total),
			NewCount:     newCount,
			UpdatedCount: updatedCount,
		}
		s.mu.Unlock()

		if progressChan != nil {
			progressChan <- *s.progress
		}
	}

	s.mu.Lock()
	s.progress = &Progress{
		Status:       StatusComplete,
		Total:        total,
		Current:      total,
		Message:      "Embedding complete",
		NewCount:     newCount,
		UpdatedCount: updatedCount,
	}
	s.mu.Unlock()

	return s.progress, nil
}

// ClearEmbeddings removes all embeddings from the database.
func (s *EmbeddingService) ClearEmbeddings() error {
	return s.repo.DeleteAllEmbeddings()
}

// IsConfigured returns true if embedding is properly configured.
func (s *EmbeddingService) IsConfigured() bool {
	return s.embedder != nil
}

// NeedsReembedding checks if embeddings need to be regenerated due to model change.
func (s *EmbeddingService) NeedsReembedding() (bool, string, error) {
	// Get current model name
	currentModel := s.getModelName()

	// Get model from existing embeddings
	storedModel, err := s.repo.GetEmbeddingModel()
	if err != nil {
		return false, "", err
	}

	// If no embeddings exist, no re-embedding needed
	if storedModel == "" {
		return false, currentModel, nil
	}

	// If models differ, re-embedding needed
	if storedModel != currentModel {
		return true, currentModel, nil
	}

	return false, currentModel, nil
}

// GetEmbeddingCount returns the number of embeddings in the database.
func (s *EmbeddingService) GetEmbeddingCount() (int64, error) {
	return s.repo.GetEmbeddingCount()
}

// Helper methods

func (s *EmbeddingService) getNodeIDsByKinds(kinds []string) ([]int64, error) {
	return s.getNodeIDsByKindsWithRepo(s.repo, kinds)
}

func (s *EmbeddingService) getNodeIDsByKindsWithRepo(repo *repository.Repository, kinds []string) ([]int64, error) {
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

	rows, err := repo.DB().Query(query, args...)
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
	return ids, nil
}

func (s *EmbeddingService) getNodeTexts(nodeIDs []int64) ([]string, error) {
	return s.getNodeTextsWithRepo(s.repo, nodeIDs)
}

func (s *EmbeddingService) getNodeTextsWithRepo(repo *repository.Repository, nodeIDs []int64) ([]string, error) {
	// Build query with placeholders
	placeholders := make([]string, len(nodeIDs))
	args := make([]interface{}, len(nodeIDs))
	for i, id := range nodeIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, name, kind, file, qualified_name, 
		       COALESCE(docstring, '') as docstring
		FROM nodes 
		WHERE id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := repo.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Create a map for quick lookup
	textMap := make(map[int64]string)
	for rows.Next() {
		var id int64
		var name, kind, file, qualName, docstring string
		if err := rows.Scan(&id, &name, &kind, &file, &qualName, &docstring); err != nil {
			return nil, err
		}

		// Build text for embedding
		text := s.buildEmbeddingText(name, kind, file, qualName, docstring)
		textMap[id] = text
	}

	// Return in the same order as input
	texts := make([]string, len(nodeIDs))
	for i, id := range nodeIDs {
		texts[i] = textMap[id]
	}

	return texts, nil
}

func (s *EmbeddingService) buildEmbeddingText(name, kind, file, qualName, docstring string) string {
	var parts []string

	// Add qualified name as primary identifier
	if qualName != "" {
		parts = append(parts, qualName)
	} else {
		parts = append(parts, name)
	}

	// Add kind information
	if kind != "" {
		parts = append(parts, fmt.Sprintf("(%s)", kind))
	}

	// Add file path
	if file != "" {
		parts = append(parts, fmt.Sprintf("in %s", file))
	}

	// Add docstring if available
	if docstring != "" {
		parts = append(parts, docstring)
	}

	return strings.Join(parts, " ")
}

func (s *EmbeddingService) getModelName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.modelConfig != nil {
		if provider, ok := s.modelConfig["embedding_provider"]; ok {
			model, _ := s.modelConfig["embedding_model"]
			if model == "" {
				model = "default"
			}
			return fmt.Sprintf("%s/%s", provider, model)
		}
	}

	if s.embedder != nil {
		return s.embedder.ModelName()
	}

	return "unknown"
}

func (s *EmbeddingService) computeHash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func (s *EmbeddingService) storeEmbeddingWithHash(nodeID int64, vector []float32, model, text, contentHash string) error {
	return s.storeEmbeddingWithHashRepo(s.repo, nodeID, vector, model, text, contentHash)
}

func (s *EmbeddingService) storeEmbeddingWithHashRepo(repo *repository.Repository, nodeID int64, vector []float32, model, text, contentHash string) error {
	// Store embedding with content hash
	// This requires updating the repository to support content_hash
	return repo.StoreEmbedding(nodeID, vector, model, text)
}

func (s *EmbeddingService) isNewEmbedding(nodeID int64) bool {
	return s.isNewEmbeddingWithRepo(s.repo, nodeID)
}

func (s *EmbeddingService) isNewEmbeddingWithRepo(repo *repository.Repository, nodeID int64) bool {
	_, err := repo.GetEmbedding(nodeID)
	return err != nil // If error (not found), it's new
}
