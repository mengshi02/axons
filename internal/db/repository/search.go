// Package repository provides data access layer.
package repository

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// FTS5SearchResult represents a full-text search result.
type FTS5SearchResult struct {
	NodeID        int64   `json:"node_id"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	File          string  `json:"file"`
	Line          int     `json:"line"`
	EndLine       int     `json:"end_line"`
	QualifiedName string  `json:"qualified_name"`
	Docstring     string  `json:"docstring,omitempty"`
	BM25Score     float64 `json:"bm25_score"`
}

// FTS5Search performs full-text search using FTS5 with BM25 ranking.
func (r *Repository) FTS5Search(query string, limit int) ([]*FTS5SearchResult, error) {
	// Sanitize the user-supplied query so that it is safe to feed into the
	// FTS5 MATCH operator. Without this, characters like '=', ':', '*', '"',
	// '(', ')' may produce "fts5: syntax error near ..." failures whenever
	// the LLM (or any caller) passes a free-form string such as `name = "x"`.
	safeQuery := sanitizeFTS5Query(query)
	if safeQuery == "" {
		return nil, nil
	}
	// Use FTS5 MATCH with BM25 scoring
	// BM25 returns negative scores, so we negate for ranking (higher is better)
	rows, err := r.db.Query(`
		SELECT 
			n.id,
			n.name,
			n.kind,
			n.file,
			n.line,
			n.end_line,
			n.qualified_name,
			n.docstring,
			-bm25(nodes_fts) as bm25_score
		FROM nodes_fts fts
		JOIN nodes n ON fts.rowid = n.id
		WHERE nodes_fts MATCH ?
		ORDER BY bm25_score DESC
		LIMIT ?
	`, safeQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("fts5 search failed: %w", err)
	}
	defer rows.Close()

	var results []*FTS5SearchResult
	for rows.Next() {
		result := &FTS5SearchResult{}
		var docstring sql.NullString
		var qualName sql.NullString
		var endLine sql.NullInt64

		if err := rows.Scan(&result.NodeID, &result.Name, &result.Kind, &result.File, &result.Line,
			&endLine, &qualName, &docstring, &result.BM25Score); err != nil {
			return nil, err
		}

		if endLine.Valid {
			result.EndLine = int(endLine.Int64)
		}
		if qualName.Valid {
			result.QualifiedName = qualName.String
		}
		if docstring.Valid {
			result.Docstring = docstring.String
		}

		results = append(results, result)
	}

	return results, rows.Err()
}

// FTS5SearchWithFilter performs FTS5 search with additional filters.
func (r *Repository) FTS5SearchWithFilter(query string, kind string, filePattern string, limit int) ([]*FTS5SearchResult, error) {
	// Sanitize the FTS5 MATCH expression — see FTS5Search for rationale.
	safeQuery := sanitizeFTS5Query(query)
	if safeQuery == "" {
		return nil, nil
	}
	// Build query with filters
	sqlQuery := `
		SELECT 
			n.id,
			n.name,
			n.kind,
			n.file,
			n.line,
			n.end_line,
			n.qualified_name,
			n.docstring,
			-bm25(nodes_fts) as bm25_score
		FROM nodes_fts fts
		JOIN nodes n ON fts.rowid = n.id
		WHERE nodes_fts MATCH ?
	`
	args := []interface{}{safeQuery}

	if kind != "" {
		sqlQuery += " AND n.kind = ?"
		args = append(args, kind)
	}

	if filePattern != "" {
		sqlQuery += " AND n.file LIKE ?"
		args = append(args, "%"+filePattern+"%")
	}

	sqlQuery += " ORDER BY bm25_score DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("fts5 search with filter failed: %w", err)
	}
	defer rows.Close()

	var results []*FTS5SearchResult
	for rows.Next() {
		result := &FTS5SearchResult{}
		var docstring sql.NullString
		var qualName sql.NullString
		var endLine sql.NullInt64

		if err := rows.Scan(&result.NodeID, &result.Name, &result.Kind, &result.File, &result.Line,
			&endLine, &qualName, &docstring, &result.BM25Score); err != nil {
			return nil, err
		}

		if endLine.Valid {
			result.EndLine = int(endLine.Int64)
		}
		if qualName.Valid {
			result.QualifiedName = qualName.String
		}
		if docstring.Valid {
			result.Docstring = docstring.String
		}

		results = append(results, result)
	}

	return results, rows.Err()
}

// UpdateNodeDocstring updates the docstring field of a node.
func (r *Repository) UpdateNodeDocstring(nodeID int64, docstring string) error {
	_, err := r.db.Exec("UPDATE nodes SET docstring = ? WHERE id = ?", docstring, nodeID)
	return err
}

// RebuildFTS5Index rebuilds the FTS5 index from scratch.
func (r *Repository) RebuildFTS5Index() error {
	// Clear existing index
	if _, err := r.db.Exec("DELETE FROM nodes_fts"); err != nil {
		return fmt.Errorf("failed to clear FTS5 index: %w", err)
	}

	// Rebuild from nodes table
	if _, err := r.db.Exec(`
		INSERT INTO nodes_fts(rowid, name, qualified_name, docstring)
		SELECT id, name, COALESCE(qualified_name, ''), COALESCE(docstring, '')
		FROM nodes
	`); err != nil {
		return fmt.Errorf("failed to rebuild FTS5 index: %w", err)
	}

	return nil
}

// GetSourceCode retrieves source code for a node by reading the file.
func (r *Repository) GetSourceCode(nodeID int64) (string, error) {
	node, err := r.FindNodeByID(nodeID)
	if err != nil {
		return "", err
	}

	// Read file content
	content, err := os.ReadFile(node.File)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", node.File, err)
	}

	lines := strings.Split(string(content), "\n")

	// Extract lines for the node
	startLine := node.Line
	endLine := node.EndLine
	if endLine == 0 || endLine < startLine {
		endLine = startLine
	}

	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	// Return source code (1-indexed to 0-indexed)
	result := strings.Join(lines[startLine-1:endLine], "\n")
	return result, nil
}

// GetSourceCodeForNodes retrieves source code for multiple nodes.
func (r *Repository) GetSourceCodeForNodes(nodeIDs []int64) (map[int64]string, error) {
	results := make(map[int64]string)
	for _, id := range nodeIDs {
		code, err := r.GetSourceCode(id)
		if err != nil {
			results[id] = ""
			continue
		}
		results[id] = code
	}
	return results, nil
}

// HybridSearchResult represents a hybrid search result combining FTS5 and vector search.
type HybridSearchResult struct {
	NodeID        int64   `json:"node_id"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	File          string  `json:"file"`
	Line          int     `json:"line"`
	EndLine       int     `json:"end_line"`
	QualifiedName string  `json:"qualified_name"`
	Docstring     string  `json:"docstring,omitempty"`
	SourceCode    string  `json:"source_code,omitempty"`

	// Scores from different search methods
	BM25Score   float64 `json:"bm25_score,omitempty"`   // FTS5 BM25 score
	VectorScore float32 `json:"vector_score,omitempty"` // Vector similarity score
	RRFScore    float64 `json:"rrf_score"`              // RRF fusion score

	// Ranking information
	BM25Rank   int `json:"bm25_rank,omitempty"`   // Rank in BM25 results
	VectorRank int `json:"vector_rank,omitempty"` // Rank in vector results
	FinalRank  int `json:"final_rank"`            // Final rank after fusion
}