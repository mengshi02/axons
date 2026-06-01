// Package repository provides data access layer.
package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
)

// EmbeddingRecord represents a stored embedding.
type EmbeddingRecord struct {
	NodeID    int64
	Vector    []float32
	Model     string
	Text      string // Source text (optional)
	CreatedAt string
}

// SemanticSearchResult represents a semantic search result.
type SemanticSearchResult struct {
	NodeID        int64   `json:"node_id"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	File          string  `json:"file"`
	Line          int     `json:"line"`
	EndLine       int     `json:"end_line"`
	Score         float32 `json:"score"`
	Text          string  `json:"text,omitempty"`
	QualifiedName string  `json:"qualified_name,omitempty"`
}

// EnsureVecTable ensures vec_embeddings has the correct dimension.
// If the table has a different dimension, it drops and recreates it.
// This must be called before storing embeddings.
func (r *Repository) EnsureVecTable(dim int) error {
	if dim <= 0 {
		return nil
	}

	// Query sqlite_master for the virtual table definition.
	// vec0 virtual tables appear as type='table' in sqlite_master.
	var createSQL string
	err := r.db.QueryRow(
		`SELECT COALESCE(sql, '') FROM sqlite_master WHERE name='vec_embeddings'`,
	).Scan(&createSQL)

	fmt.Printf("info: EnsureVecTable dim=%d, existing createSQL=%q, queryErr=%v\n", dim, createSQL, err)

	tableExists := err == nil && createSQL != ""

	if tableExists {
		// Check if dimension matches, e.g. "float[1536]" vs "float[768]"
		expected := fmt.Sprintf("float[%d]", dim)
		if containsIgnoreCase(createSQL, expected) {
			// Dimension already correct
			fmt.Printf("info: vec_embeddings already has correct dim=%d\n", dim)
			return nil
		}
		// Dimension mismatch: drop and recreate
		fmt.Printf("info: vec_embeddings dimension mismatch, dropping table (sql=%q)\n", createSQL)
		if _, dropErr := r.db.Exec(`DROP TABLE IF EXISTS vec_embeddings`); dropErr != nil {
			return fmt.Errorf("failed to drop vec_embeddings: %w", dropErr)
		}
	}

	// Create (or recreate) vec_embeddings with correct dimension
	_, err = r.db.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_embeddings USING vec0(
			embedding float[%d]
		)
	`, dim))
	if err != nil {
		return fmt.Errorf("failed to create vec_embeddings (dim=%d): %w", dim, err)
	}
	fmt.Printf("info: vec_embeddings created/recreated with dim=%d\n", dim)
	return nil
}

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > 0 && containsStr(toLower(s), toLower(substr)))
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// StoreEmbedding stores an embedding for a node using sqlite-vec.
func (r *Repository) StoreEmbedding(nodeID int64, vector []float32, model, text string) error {
	// Use transaction to store in both embeddings table and vec_embeddings virtual table
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Store metadata in embeddings table
	vectorBytes := float32SliceToBytes(vector)
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO embeddings (node_id, embedding, model, created_at)
		VALUES (?, ?, ?, datetime('now'))
	`, nodeID, vectorBytes, model)
	if err != nil {
		return fmt.Errorf("failed to store embedding metadata: %w", err)
	}

	// Store vector in vec_embeddings virtual table
	// Use DELETE + INSERT instead of INSERT OR REPLACE (vec0 may not support OR REPLACE)
	_, _ = tx.Exec(`DELETE FROM vec_embeddings WHERE rowid = ?`, nodeID)
	vectorJSON, err := vectorToJSON(vector)
	if err != nil {
		return fmt.Errorf("failed to serialize vector: %w", err)
	}
	_, err = tx.Exec(`
		INSERT INTO vec_embeddings (rowid, embedding)
		VALUES (?, ?)
	`, nodeID, vectorJSON)
	if err != nil {
		// Log but don't fail: embeddings table is the source of truth
		fmt.Printf("warning: failed to store vector in vec_embeddings for node %d: %v\n", nodeID, err)
		// Still commit the embeddings table entry
		_ = tx.Rollback()
		// Re-insert only into embeddings table
		_, err2 := r.db.Exec(`
			INSERT OR REPLACE INTO embeddings (node_id, embedding, model, created_at)
			VALUES (?, ?, ?, datetime('now'))
		`, nodeID, vectorBytes, model)
		return err2
	}

	return tx.Commit()
}

// StoreEmbeddings stores multiple embeddings in a transaction.
func (r *Repository) StoreEmbeddings(embeddings []*EmbeddingRecord) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Prepare statements
	metaStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO embeddings (node_id, embedding, model, created_at)
		VALUES (?, ?, ?, datetime('now'))
	`)
	if err != nil {
		return err
	}
	defer metaStmt.Close()

	delStmt, err := tx.Prepare(`DELETE FROM vec_embeddings WHERE rowid = ?`)
	if err != nil {
		return err
	}
	defer delStmt.Close()

	vecStmt, err := tx.Prepare(`
		INSERT INTO vec_embeddings (rowid, embedding)
		VALUES (?, ?)
	`)
	if err != nil {
		return err
	}
	defer vecStmt.Close()

	for _, e := range embeddings {
		// Store metadata
		vectorBytes := float32SliceToBytes(e.Vector)
		_, err := metaStmt.Exec(e.NodeID, vectorBytes, e.Model)
		if err != nil {
			return fmt.Errorf("failed to store embedding metadata for node %d: %w", e.NodeID, err)
		}

		// Store vector (DELETE + INSERT to avoid OR REPLACE issues with vec0)
		_, _ = delStmt.Exec(e.NodeID)
		vectorJSON, err := vectorToJSON(e.Vector)
		if err != nil {
			return fmt.Errorf("failed to serialize vector for node %d: %w", e.NodeID, err)
		}
		if _, err = vecStmt.Exec(e.NodeID, vectorJSON); err != nil {
			fmt.Printf("warning: failed to store vector in vec_embeddings for node %d: %v\n", e.NodeID, err)
		}
	}

	return tx.Commit()
}

// GetEmbedding retrieves an embedding for a node.
func (r *Repository) GetEmbedding(nodeID int64) (*EmbeddingRecord, error) {
	var record EmbeddingRecord
	var vectorBytes []byte

	err := r.db.QueryRow(`
		SELECT node_id, embedding, model, created_at
		FROM embeddings WHERE node_id = ?
	`, nodeID).Scan(&record.NodeID, &vectorBytes, &record.Model, &record.CreatedAt)
	if err != nil {
		return nil, err
	}

	record.Vector = bytesToFloat32Slice(vectorBytes)
	return &record, nil
}

// GetEmbeddingCount returns the number of stored embeddings.
func (r *Repository) GetEmbeddingCount() (int64, error) {
	var count int64
	err := r.db.QueryRow("SELECT COUNT(*) FROM embeddings").Scan(&count)
	return count, err
}

// DeleteEmbeddingsByModel deletes all embeddings for a specific model.
func (r *Repository) DeleteEmbeddingsByModel(model string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get node IDs for this model
	rows, err := tx.Query("SELECT node_id FROM embeddings WHERE model = ?", model)
	if err != nil {
		return err
	}
	var nodeIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		nodeIDs = append(nodeIDs, id)
	}
	rows.Close()

	// Delete from vec_embeddings
	for _, id := range nodeIDs {
		if _, err := tx.Exec("DELETE FROM vec_embeddings WHERE rowid = ?", id); err != nil {
			return err
		}
	}

	// Delete from embeddings
	if _, err := tx.Exec("DELETE FROM embeddings WHERE model = ?", model); err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteAllEmbeddings deletes all embeddings.
func (r *Repository) DeleteAllEmbeddings() error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM vec_embeddings"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM embeddings"); err != nil {
		return err
	}

	return tx.Commit()
}

// SemanticSearch performs semantic search using sqlite-vec.
func (r *Repository) SemanticSearch(queryVector []float32, limit int, threshold float32) ([]*SemanticSearchResult, error) {
	// Convert query vector to JSON format for sqlite-vec
	vectorJSON, err := vectorToJSON(queryVector)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize query vector: %w", err)
	}

	// Use sqlite-vec for KNN search
	// Note: sqlite-vec requires 'k = ?' in the MATCH clause for KNN queries
	query := `
		SELECT 
			v.rowid as node_id,
			v.distance,
			n.name,
			n.kind,
			n.file,
			n.line,
			n.end_line,
			n.qualified_name
		FROM vec_embeddings v
		JOIN nodes n ON v.rowid = n.id
		WHERE v.embedding MATCH ? AND k = ?
		ORDER BY v.distance
	`

	rows, err := r.db.Query(query, vectorJSON, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query vec_embeddings: %w", err)
	}
	defer rows.Close()

	var results []*SemanticSearchResult
	for rows.Next() {
		var nodeID int64
		var distance float64
		var name, kind, file, qualName string
		var line, endLine int

		if err := rows.Scan(&nodeID, &distance, &name, &kind, &file, &line, &endLine, &qualName); err != nil {
			return nil, err
		}

		// Convert L2 distance to cosine similarity score
		// For normalized vectors: cosine_similarity = 1 - (distance^2 / 2)
		// We use a simpler approximation: score = 1 / (1 + distance)
		score := float32(1.0 / (1.0 + distance))

		// Apply threshold
		if score >= threshold {
			results = append(results, &SemanticSearchResult{
				NodeID:        nodeID,
				Name:          name,
				Kind:          kind,
				File:          file,
				Line:          line,
				EndLine:       endLine,
				Score:         score,
				QualifiedName: qualName,
			})
		}
	}

	return results, rows.Err()
}

// GetNodesWithEmbeddings returns all node IDs that have embeddings.
func (r *Repository) GetNodesWithEmbeddings() ([]int64, error) {
	rows, err := r.db.Query("SELECT node_id FROM embeddings")
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

// GetNodesWithoutEmbeddings returns node IDs that don't have embeddings.
func (r *Repository) GetNodesWithoutEmbeddings(kinds []string) ([]int64, error) {
	query := `
		SELECT n.id FROM nodes n
		LEFT JOIN embeddings e ON n.id = e.node_id
		WHERE e.node_id IS NULL
	`
	args := []interface{}{}

	if len(kinds) > 0 {
		query += " AND n.kind IN ("
		for i, kind := range kinds {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, kind)
		}
		query += ")"
	}

	rows, err := r.db.Query(query, args...)
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

// GetEmbeddingModel returns the model used for embeddings.
func (r *Repository) GetEmbeddingModel() (string, error) {
	var model string
	err := r.db.QueryRow("SELECT model FROM embeddings LIMIT 1").Scan(&model)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return model, err
}

// vectorToJSON converts a float32 slice to JSON array string for sqlite-vec.
func vectorToJSON(vec []float32) (string, error) {
	bytes, err := json.Marshal(vec)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// cosineSimilarity computes cosine similarity between two vectors.
// Kept for fallback and testing purposes.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// float32SliceToBytes converts a float32 slice to bytes.
func float32SliceToBytes(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(v)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}

// bytesToFloat32Slice converts bytes to a float32 slice.
func bytesToFloat32Slice(buf []byte) []float32 {
	if len(buf)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(buf)/4)
	for i := range vec {
		bits := uint32(buf[i*4]) | uint32(buf[i*4+1])<<8 | uint32(buf[i*4+2])<<16 | uint32(buf[i*4+3])<<24
		vec[i] = math.Float32frombits(bits)
	}
	return vec
}