package cce

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Store provides CCE-specific data access methods.
// It operates on the same project-scoped database used by repository.Repository,
// adding tables and queries needed for code-mode embeddings and context retrieval.
type Store struct {
	db *sql.DB
}

// NewStore creates a new CCE store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// DB returns the underlying database connection.
func (s *Store) DB() *sql.DB {
	return s.db
}

// --- Code Chunks ---

// StoreCodeChunk stores a code chunk for a node.
func (s *Store) StoreCodeChunk(chunk *CodeChunk) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO code_chunks (node_id, file, start_line, end_line, content, language, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, chunk.NodeID, chunk.File, chunk.StartLine, chunk.EndLine, chunk.Content, chunk.Language, chunk.ContentHash)
	if err != nil {
		return fmt.Errorf("failed to store code chunk for node %d: %w", chunk.NodeID, err)
	}
	return nil
}

// StoreCodeChunks stores multiple code chunks in a transaction.
func (s *Store) StoreCodeChunks(chunks []*CodeChunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO code_chunks (node_id, file, start_line, end_line, content, language, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, chunk := range chunks {
		if _, err := stmt.Exec(chunk.NodeID, chunk.File, chunk.StartLine, chunk.EndLine,
			chunk.Content, chunk.Language, chunk.ContentHash); err != nil {
			return fmt.Errorf("failed to store code chunk for node %d: %w", chunk.NodeID, err)
		}
	}

	return tx.Commit()
}

// GetCodeChunk retrieves the code chunk for a node.
func (s *Store) GetCodeChunk(nodeID int64) (*CodeChunk, error) {
	chunk := &CodeChunk{}
	err := s.db.QueryRow(`
		SELECT id, node_id, file, start_line, end_line, content, language, content_hash, created_at
		FROM code_chunks WHERE node_id = ?
	`, nodeID).Scan(&chunk.ID, &chunk.NodeID, &chunk.File, &chunk.StartLine,
		&chunk.EndLine, &chunk.Content, &chunk.Language, &chunk.ContentHash, &chunk.CreatedAt)
	if err != nil {
		return nil, err
	}
	return chunk, nil
}

// GetCodeChunksForNodes retrieves code chunks for multiple nodes.
func (s *Store) GetCodeChunksForNodes(nodeIDs []int64) (map[int64]*CodeChunk, error) {
	if len(nodeIDs) == 0 {
		return make(map[int64]*CodeChunk), nil
	}

	placeholders := make([]string, len(nodeIDs))
	args := make([]interface{}, len(nodeIDs))
	for i, id := range nodeIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, node_id, file, start_line, end_line, content, language, content_hash, created_at
		FROM code_chunks WHERE node_id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]*CodeChunk)
	for rows.Next() {
		chunk := &CodeChunk{}
		if err := rows.Scan(&chunk.ID, &chunk.NodeID, &chunk.File, &chunk.StartLine,
			&chunk.EndLine, &chunk.Content, &chunk.Language, &chunk.ContentHash, &chunk.CreatedAt); err != nil {
			return nil, err
		}
		result[chunk.NodeID] = chunk
	}
	return result, rows.Err()
}

// GetNodesWithoutCodeChunks returns node IDs that don't have code chunks yet.
func (s *Store) GetNodesWithoutCodeChunks(kinds []string) ([]int64, error) {
	query := `
		SELECT n.id FROM nodes n
		LEFT JOIN code_chunks cc ON n.id = cc.node_id
		WHERE cc.node_id IS NULL
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

	rows, err := s.db.Query(query, args...)
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

// --- Code Embeddings (vec_code_embeddings) ---

// EnsureVecCodeTable ensures vec_code_embeddings has the correct dimension.
func (s *Store) EnsureVecCodeTable(dim int) error {
	if dim <= 0 {
		return nil
	}

	var createSQL string
	err := s.db.QueryRow(
		`SELECT COALESCE(sql, '') FROM sqlite_master WHERE name='vec_code_embeddings'`,
	).Scan(&createSQL)

	tableExists := err == nil && createSQL != ""

	if tableExists {
		expected := fmt.Sprintf("float[%d]", dim)
		if strings.Contains(strings.ToLower(createSQL), strings.ToLower(expected)) {
			return nil
		}
		// Dimension mismatch: drop and recreate
		if _, err := s.db.Exec(`DROP TABLE IF EXISTS vec_code_embeddings`); err != nil {
			return fmt.Errorf("failed to drop vec_code_embeddings: %w", err)
		}
	}

	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS vec_code_embeddings USING vec0(
			embedding float[%d]
		)
	`, dim))
	if err != nil {
		return fmt.Errorf("failed to create vec_code_embeddings (dim=%d): %w", dim, err)
	}
	return nil
}

// StoreCodeEmbedding stores a code-mode embedding for a node chunk.
// chunkIndex identifies which chunk of the node this embedding represents (0-based).
func (s *Store) StoreCodeEmbedding(nodeID int64, chunkIndex int, vector []float32, model, text string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Store metadata in code_embeddings table
	vectorBytes := float32SliceToCCEBytes(vector)
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO code_embeddings (node_id, chunk_index, embedding, model, text, created_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
	`, nodeID, chunkIndex, vectorBytes, model, text)
	if err != nil {
		return fmt.Errorf("failed to store code embedding metadata: %w", err)
	}

	// Store vector in vec_code_embeddings virtual table
	// Use composite rowid: nodeID * 1000 + chunkIndex to ensure uniqueness
	vecRowID := nodeID*1000 + int64(chunkIndex)
	_, _ = tx.Exec(`DELETE FROM vec_code_embeddings WHERE rowid = ?`, vecRowID)
	vectorJSON, err := vectorToCCEJSON(vector)
	if err != nil {
		return fmt.Errorf("failed to serialize code vector: %w", err)
	}
	_, err = tx.Exec(`
		INSERT INTO vec_code_embeddings (rowid, embedding)
		VALUES (?, ?)
	`, vecRowID, vectorJSON)
	if err != nil {
		// Log warning - vec table insert failed but we can still save metadata
		fmt.Printf("[CCE] warning: failed to store vector in vec_code_embeddings for node %d chunk %d: %v\n", nodeID, chunkIndex, err)
		// Still commit the metadata entry
		_ = tx.Rollback()
		_, err2 := s.db.Exec(`
			INSERT OR REPLACE INTO code_embeddings (node_id, chunk_index, embedding, model, text, created_at)
			VALUES (?, ?, ?, ?, ?, datetime('now'))
		`, nodeID, chunkIndex, vectorBytes, model, text)
		return err2
	}

	return tx.Commit()
}

// StoreCodeEmbeddings stores multiple code embeddings in a transaction.
func (s *Store) StoreCodeEmbeddings(embeddings []*CodeEmbedding) error {
	if len(embeddings) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	metaStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO code_embeddings (node_id, chunk_index, embedding, model, text, created_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
	`)
	if err != nil {
		return err
	}
	defer metaStmt.Close()

	delStmt, err := tx.Prepare(`DELETE FROM vec_code_embeddings WHERE rowid = ?`)
	if err != nil {
		return err
	}
	defer delStmt.Close()

	vecStmt, err := tx.Prepare(`
		INSERT INTO vec_code_embeddings (rowid, embedding)
		VALUES (?, ?)
	`)
	if err != nil {
		return err
	}
	defer vecStmt.Close()

	for _, e := range embeddings {
		vectorBytes := float32SliceToCCEBytes(e.Vector)
		_, err := metaStmt.Exec(e.ChunkID, e.ChunkIndex, vectorBytes, e.Model, e.Text)
		if err != nil {
			return fmt.Errorf("failed to store code embedding metadata for node %d chunk %d: %w", e.ChunkID, e.ChunkIndex, err)
		}

		vecRowID := e.ChunkID*1000 + int64(e.ChunkIndex)
		_, _ = delStmt.Exec(vecRowID)
		vectorJSON, err := vectorToCCEJSON(e.Vector)
		if err != nil {
			return fmt.Errorf("failed to serialize code vector for node %d chunk %d: %w", e.ChunkID, e.ChunkIndex, err)
		}
		if _, err = vecStmt.Exec(vecRowID, vectorJSON); err != nil {
			fmt.Printf("warning: failed to store vector in vec_code_embeddings for node %d chunk %d: %v\n", e.ChunkID, e.ChunkIndex, err)
		}
	}

	return tx.Commit()
}

// CodeSemanticSearch performs semantic search using code-mode embeddings.
// When a node has multiple chunk embeddings (from chunking), this method
// merges results: for the same node, it keeps the best (lowest distance) score.
func (s *Store) CodeSemanticSearch(queryVector []float32, limit int, threshold float32) ([]*RetrievalResult, error) {
	vectorJSON, err := vectorToCCEJSON(queryVector)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize query vector: %w", err)
	}

	// Query more results than needed to account for deduplication
	queryLimit := limit * 3
	if queryLimit < 50 {
		queryLimit = 50
	}

	query := `
		SELECT
			v.rowid as vec_rowid,
			v.distance,
			n.id as node_id,
			n.name,
			n.kind,
			n.file,
			n.line,
			n.end_line
		FROM vec_code_embeddings v
		JOIN nodes n ON (v.rowid / 1000) = n.id
		WHERE v.embedding MATCH ? AND k = ?
		ORDER BY v.distance
	`

	rows, err := s.db.Query(query, vectorJSON, queryLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to query vec_code_embeddings: %w", err)
	}
	defer rows.Close()

	// Merge results: keep best score per node
	type nodeResult struct {
		nodeID  int64
		name    string
		kind    string
		file    string
		line    int
		endLine int
		score   float32
	}
	seen := make(map[int64]*nodeResult)

	for rows.Next() {
		var vecRowID int64
		var distance float64
		var nodeID int64
		var name, kind, file string
		var line, endLine int

		if err := rows.Scan(&vecRowID, &distance, &nodeID, &name, &kind, &file, &line, &endLine); err != nil {
			return nil, err
		}

		score := float32(1.0 / (1.0 + distance))
		if score < threshold {
			continue
		}

		if existing, ok := seen[nodeID]; ok {
			if score > existing.score {
				existing.score = score
			}
		} else {
			seen[nodeID] = &nodeResult{
				nodeID:  nodeID,
				name:    name,
				kind:    kind,
				file:    file,
				line:    line,
				endLine: endLine,
				score:   score,
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Convert to sorted results
	var results []*RetrievalResult
	for _, nr := range seen {
		results = append(results, &RetrievalResult{
			NodeID:  nr.nodeID,
			Name:    nr.name,
			Kind:    nr.kind,
			File:    nr.file,
			Line:    nr.line,
			EndLine: nr.endLine,
			Score:   nr.score,
			Source:  "semantic_code",
		})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetCodeEmbeddingCount returns the number of stored code embeddings.
func (s *Store) GetCodeEmbeddingCount() (int64, error) {
	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM code_embeddings").Scan(&count)
	return count, err
}

// DeleteCodeEmbeddingsByModel deletes all code embeddings for a specific model.
func (s *Store) DeleteCodeEmbeddingsByModel(model string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Collect (node_id, chunk_index) pairs for vec table cleanup
	rows, err := tx.Query("SELECT node_id, chunk_index FROM code_embeddings WHERE model = ?", model)
	if err != nil {
		return err
	}
	type chunkRef struct {
		nodeID     int64
		chunkIndex int
	}
	var refs []chunkRef
	for rows.Next() {
		var ref chunkRef
		if err := rows.Scan(&ref.nodeID, &ref.chunkIndex); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, ref)
	}
	rows.Close()

	for _, ref := range refs {
		vecRowID := ref.nodeID*1000 + int64(ref.chunkIndex)
		if _, err := tx.Exec("DELETE FROM vec_code_embeddings WHERE rowid = ?", vecRowID); err != nil {
			return err
		}
	}

	if _, err := tx.Exec("DELETE FROM code_embeddings WHERE model = ?", model); err != nil {
		return err
	}

	return tx.Commit()
}

// --- Dual Embedding Queries ---

// GetDualEmbeddingStats returns counts for description and code embeddings.
func (s *Store) GetDualEmbeddingStats() (descCount, codeCount int64, err error) {
	err = s.db.QueryRow("SELECT COUNT(*) FROM embeddings").Scan(&descCount)
	if err != nil {
		return 0, 0, err
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM code_embeddings").Scan(&codeCount)
	return descCount, codeCount, err
}

// --- Graph-aware queries ---

// GetNeighborsByEdgeTypes retrieves neighbor nodes via specified edge types.
func (s *Store) GetNeighborsByEdgeTypes(nodeID int64, edgeTypes []string, depth int) ([]*RetrievalResult, error) {
	if depth <= 0 || len(edgeTypes) == 0 {
		return nil, nil
	}

	// Build edge type filter
	placeholders := make([]string, len(edgeTypes))
	args := make([]interface{}, len(edgeTypes))
	for i, et := range edgeTypes {
		placeholders[i] = "?"
		args[i] = et
	}

	// BFS-style traversal up to specified depth
	visited := map[int64]bool{nodeID: true}
	var allResults []*RetrievalResult
	currentLevel := []int64{nodeID}

	for d := 0; d < depth; d++ {
		if len(currentLevel) == 0 {
			break
		}

		// Build IN clause for current level
		levelPlaceholders := make([]string, len(currentLevel))
		levelArgs := make([]interface{}, len(currentLevel))
		for i, id := range currentLevel {
			levelPlaceholders[i] = "?"
			levelArgs[i] = id
		}

		query := fmt.Sprintf(`
			SELECT DISTINCT n.id, n.name, n.kind, n.file, n.line, n.end_line
			FROM edges e
			JOIN nodes n ON (
				(CASE WHEN e.source_id IN (%s) THEN e.target_id ELSE e.source_id END) = n.id
			)
			WHERE (e.source_id IN (%s) OR e.target_id IN (%s))
			AND e.kind IN (%s)
		`, strings.Join(levelPlaceholders, ","),
			strings.Join(levelPlaceholders, ","),
			strings.Join(levelPlaceholders, ","),
			strings.Join(placeholders, ","))

		allArgs := make([]interface{}, 0, len(levelArgs)*3+len(args))
		allArgs = append(allArgs, levelArgs...)
		allArgs = append(allArgs, levelArgs...)
		allArgs = append(allArgs, levelArgs...)
		allArgs = append(allArgs, args...)

		rows, err := s.db.Query(query, allArgs...)
		if err != nil {
			return nil, fmt.Errorf("graph neighbor query failed at depth %d: %w", d, err)
		}

		var nextLevel []int64
		for rows.Next() {
			var id int64
			var name, kind, file string
			var line, endLine int
			if err := rows.Scan(&id, &name, &kind, &file, &line, &endLine); err != nil {
				rows.Close()
				return nil, err
			}
			if !visited[id] {
				visited[id] = true
				nextLevel = append(nextLevel, id)
				allResults = append(allResults, &RetrievalResult{
					NodeID:  id,
					Name:    name,
					Kind:    kind,
					File:    file,
					Line:    line,
					EndLine: endLine,
					Score:   float32(1.0-0.2*float32(d+1)), // Score decreases with depth
					Source:  "graph",
					Depth:   d + 1,
				})
			}
		}
		rows.Close()

		currentLevel = nextLevel
	}

	return allResults, nil
}

// --- Utility functions ---

// vectorToCCEJSON converts a float32 slice to JSON array string for sqlite-vec.
func vectorToCCEJSON(vec []float32) (string, error) {
	bytes, err := json.Marshal(vec)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// float32SliceToCCEBytes converts a float32 slice to bytes.
func float32SliceToCCEBytes(vec []float32) []byte {
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