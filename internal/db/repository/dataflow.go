// Package repository provides data access layer.
package repository

import (
	"database/sql"

	"github.com/mengshi02/axons/pkg/types"
)

// SaveDataflowEdge saves a dataflow edge to the database.
func (r *Repository) SaveDataflowEdge(edge *types.DataflowEdge) error {
	_, err := r.db.Exec(`
		INSERT INTO dataflow (source_id, target_id, kind, param_index, expression, line, confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, edge.SourceID, edge.TargetID, edge.Kind, edge.ParamIndex, edge.Expression, edge.Line, edge.Confidence)
	return err
}

// SaveDataflowEdges saves multiple dataflow edges in a transaction.
func (r *Repository) SaveDataflowEdges(edges []*types.DataflowEdge) error {
	if len(edges) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO dataflow (source_id, target_id, kind, param_index, expression, line, confidence)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, edge := range edges {
		_, err := stmt.Exec(edge.SourceID, edge.TargetID, edge.Kind, edge.ParamIndex, edge.Expression, edge.Line, edge.Confidence)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// FindDataflowBySource finds dataflow edges by source node ID.
func (r *Repository) FindDataflowBySource(sourceID int64) ([]*types.DataflowEdge, error) {
	rows, err := r.db.Query(`
		SELECT id, source_id, target_id, kind, param_index, expression, line, confidence
		FROM dataflow WHERE source_id = ?
	`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []*types.DataflowEdge
	for rows.Next() {
		edge := &types.DataflowEdge{}
		var paramIndex sql.NullInt64
		err := rows.Scan(&edge.ID, &edge.SourceID, &edge.TargetID, &edge.Kind, &paramIndex, &edge.Expression, &edge.Line, &edge.Confidence)
		if err != nil {
			return nil, err
		}
		if paramIndex.Valid {
			idx := int(paramIndex.Int64)
			edge.ParamIndex = &idx
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

// FindDataflowByTarget finds dataflow edges by target node ID.
func (r *Repository) FindDataflowByTarget(targetID int64) ([]*types.DataflowEdge, error) {
	rows, err := r.db.Query(`
		SELECT id, source_id, target_id, kind, param_index, expression, line, confidence
		FROM dataflow WHERE target_id = ?
	`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []*types.DataflowEdge
	for rows.Next() {
		edge := &types.DataflowEdge{}
		var paramIndex sql.NullInt64
		err := rows.Scan(&edge.ID, &edge.SourceID, &edge.TargetID, &edge.Kind, &paramIndex, &edge.Expression, &edge.Line, &edge.Confidence)
		if err != nil {
			return nil, err
		}
		if paramIndex.Valid {
			idx := int(paramIndex.Int64)
			edge.ParamIndex = &idx
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

// FindDataflowByKind finds dataflow edges by kind.
func (r *Repository) FindDataflowByKind(kind types.DataflowKind, limit, offset int) ([]*types.DataflowEdge, error) {
	query := `
		SELECT id, source_id, target_id, kind, param_index, expression, line, confidence
		FROM dataflow WHERE kind = ?
		ORDER BY source_id, target_id
	`
	args := []interface{}{kind}
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []*types.DataflowEdge
	for rows.Next() {
		edge := &types.DataflowEdge{}
		var paramIndex sql.NullInt64
		err := rows.Scan(&edge.ID, &edge.SourceID, &edge.TargetID, &edge.Kind, &paramIndex, &edge.Expression, &edge.Line, &edge.Confidence)
		if err != nil {
			return nil, err
		}
		if paramIndex.Valid {
			idx := int(paramIndex.Int64)
			edge.ParamIndex = &idx
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

// DeleteDataflowByFile deletes all dataflow edges for nodes in a file.
func (r *Repository) DeleteDataflowByFile(file string, projectID ...int64) error {
	if len(projectID) > 0 && projectID[0] > 0 {
		_, err := r.db.Exec(`
			DELETE FROM dataflow WHERE source_id IN (SELECT id FROM nodes WHERE file = ? AND project_id = ?)
			OR target_id IN (SELECT id FROM nodes WHERE file = ? AND project_id = ?)
		`, file, projectID[0], file, projectID[0])
		return err
	}
	_, err := r.db.Exec(`
		DELETE FROM dataflow WHERE source_id IN (SELECT id FROM nodes WHERE file = ?)
		OR target_id IN (SELECT id FROM nodes WHERE file = ?)
	`, file, file)
	return err
}

// SaveAstNode saves an AST node to the database.
func (r *Repository) SaveAstNode(node *types.AstNode) (int64, error) {
	result, err := r.db.Exec(`
		INSERT INTO ast_nodes (file, line, kind, name, text, receiver, parent_node_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, node.File, node.Line, node.Kind, node.Name, node.Text, node.Receiver, node.ParentNodeID)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// SaveAstNodes saves multiple AST nodes in a transaction.
func (r *Repository) SaveAstNodes(nodes []*types.AstNode) error {
	if len(nodes) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO ast_nodes (file, line, kind, name, text, receiver, parent_node_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, node := range nodes {
		_, err := stmt.Exec(node.File, node.Line, node.Kind, node.Name, node.Text, node.Receiver, node.ParentNodeID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// FindAstNodesByFile finds AST nodes by file path.
func (r *Repository) FindAstNodesByFile(file string, limit, offset int) ([]*types.AstNode, error) {
	query := `
		SELECT id, file, line, kind, name, text, receiver, parent_node_id
		FROM ast_nodes WHERE file = ?
		ORDER BY line
	`
	args := []interface{}{file}
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*types.AstNode
	for rows.Next() {
		node := &types.AstNode{}
		var text, receiver sql.NullString
		var parentID sql.NullInt64
		err := rows.Scan(&node.ID, &node.File, &node.Line, &node.Kind, &node.Name, &text, &receiver, &parentID)
		if err != nil {
			return nil, err
		}
		if text.Valid {
			node.Text = text.String
		}
		if receiver.Valid {
			node.Receiver = receiver.String
		}
		if parentID.Valid {
			node.ParentNodeID = &parentID.Int64
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// FindAstNodesByKind finds AST nodes by kind.
func (r *Repository) FindAstNodesByKind(kind string, limit, offset int) ([]*types.AstNode, error) {
	query := `
		SELECT id, file, line, kind, name, text, receiver, parent_node_id
		FROM ast_nodes WHERE kind = ?
		ORDER BY file, line
	`
	args := []interface{}{kind}
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*types.AstNode
	for rows.Next() {
		node := &types.AstNode{}
		var text, receiver sql.NullString
		var parentID sql.NullInt64
		err := rows.Scan(&node.ID, &node.File, &node.Line, &node.Kind, &node.Name, &text, &receiver, &parentID)
		if err != nil {
			return nil, err
		}
		if text.Valid {
			node.Text = text.String
		}
		if receiver.Valid {
			node.Receiver = receiver.String
		}
		if parentID.Valid {
			node.ParentNodeID = &parentID.Int64
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// DeleteAstNodesByFile deletes all AST nodes for a file.
func (r *Repository) DeleteAstNodesByFile(file string) error {
	_, err := r.db.Exec("DELETE FROM ast_nodes WHERE file = ?", file)
	return err
}

// SaveCoChange saves a co-change relationship.
func (r *Repository) SaveCoChange(cc *types.CoChange) error {
	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO co_changes (file_a, file_b, commit_count, jaccard, last_commit_epoch)
		VALUES (?, ?, ?, ?, ?)
	`, cc.FileA, cc.FileB, cc.CommitCount, cc.Jaccard, cc.LastCommitTime)
	return err
}

// SaveCoChanges saves multiple co-change relationships in a transaction.
func (r *Repository) SaveCoChanges(changes []*types.CoChange) error {
	if len(changes) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO co_changes (file_a, file_b, commit_count, jaccard, last_commit_epoch)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, cc := range changes {
		_, err := stmt.Exec(cc.FileA, cc.FileB, cc.CommitCount, cc.Jaccard, cc.LastCommitTime)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// FindCoChangesByFile finds co-change relationships for a file.
func (r *Repository) FindCoChangesByFile(file string, limit int) ([]*types.CoChange, error) {
	query := `
		SELECT id, file_a, file_b, commit_count, jaccard, last_commit_epoch
		FROM co_changes WHERE file_a = ? OR file_b = ?
		ORDER BY jaccard DESC
	`
	args := []interface{}{file, file}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []*types.CoChange
	for rows.Next() {
		cc := &types.CoChange{}
		var lastEpoch sql.NullInt64
		err := rows.Scan(&cc.ID, &cc.FileA, &cc.FileB, &cc.CommitCount, &cc.Jaccard, &lastEpoch)
		if err != nil {
			return nil, err
		}
		if lastEpoch.Valid {
			cc.LastCommitTime = lastEpoch.Int64
		}
		changes = append(changes, cc)
	}
	return changes, nil
}

// SaveFileCommitCount saves a file commit count.
func (r *Repository) SaveFileCommitCount(fcc *types.FileCommitCount) error {
	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO file_commit_counts (file, commit_count)
		VALUES (?, ?)
	`, fcc.File, fcc.CommitCount)
	return err
}

// GetFileCommitCount gets the commit count for a file.
func (r *Repository) GetFileCommitCount(file string) (*types.FileCommitCount, error) {
	row := r.db.QueryRow("SELECT file, commit_count FROM file_commit_counts WHERE file = ?", file)
	fcc := &types.FileCommitCount{}
	err := row.Scan(&fcc.File, &fcc.CommitCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return fcc, nil
}

// SaveCoChangeMeta saves co-change metadata.
func (r *Repository) SaveCoChangeMeta(key, value string) error {
	_, err := r.db.Exec("INSERT OR REPLACE INTO co_change_meta (key, value) VALUES (?, ?)", key, value)
	return err
}

// GetCoChangeMeta gets co-change metadata.
func (r *Repository) GetCoChangeMeta(key string) (string, error) {
	var value string
	err := r.db.QueryRow("SELECT value FROM co_change_meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SaveNodeMetrics saves metrics for a node.
func (r *Repository) SaveNodeMetrics(metrics *types.NodeMetrics) error {
	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO node_metrics (node_id, line_count, symbol_count, import_count, export_count, fan_in, fan_out, cohesion, file_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, metrics.NodeID, metrics.LineCount, metrics.SymbolCount, metrics.ImportCount, metrics.ExportCount, metrics.FanIn, metrics.FanOut, metrics.Cohesion, metrics.FileCount)
	return err
}

// GetNodeMetrics gets metrics for a node.
func (r *Repository) GetNodeMetrics(nodeID int64) (*types.NodeMetrics, error) {
	row := r.db.QueryRow(`
		SELECT node_id, line_count, symbol_count, import_count, export_count, fan_in, fan_out, cohesion, file_count
		FROM node_metrics WHERE node_id = ?
	`, nodeID)
	metrics := &types.NodeMetrics{}
	err := row.Scan(&metrics.NodeID, &metrics.LineCount, &metrics.SymbolCount, &metrics.ImportCount, &metrics.ExportCount, &metrics.FanIn, &metrics.FanOut, &metrics.Cohesion, &metrics.FileCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return metrics, nil
}

// ClearCoChanges clears all co-change data.
func (r *Repository) ClearCoChanges() error {
	if _, err := r.db.Exec("DELETE FROM co_changes"); err != nil {
		return err
	}
	if _, err := r.db.Exec("DELETE FROM file_commit_counts"); err != nil {
		return err
	}
	if _, err := r.db.Exec("DELETE FROM co_change_meta"); err != nil {
		return err
	}
	return nil
}