// Package repository provides data access layer.
package repository

import (
	"github.com/mengshi02/axons/pkg/types"
)

// ListAllNodes returns all nodes in the database.
func (r *Repository) ListAllNodes() ([]*types.Node, error) {
	rows, err := r.db.Query(`
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*types.Node
	for rows.Next() {
		node := &types.Node{}
		if err := rows.Scan(&node.ID, &node.Name, &node.Kind, &node.File, &node.Line, &node.EndLine, &node.ParentID, &node.Exported, &node.QualifiedName, &node.Scope, &node.Visibility, &node.Role, &node.FileHash); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// ListAllEdges returns all edges in the database.
func (r *Repository) ListAllEdges() ([]*types.Edge, error) {
	rows, err := r.db.Query(`
		SELECT id, source_id, target_id, kind, confidence, dynamic
		FROM edges ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []*types.Edge
	for rows.Next() {
		edge := &types.Edge{}
		if err := rows.Scan(&edge.ID, &edge.SourceID, &edge.TargetID, &edge.Kind, &edge.Confidence, &edge.Dynamic); err != nil {
			return nil, err
		}
		edges = append(edges, edge)
	}
	return edges, nil
}

// FindASTNodesByParent finds AST nodes by parent node ID (stored in parent_node_id).
func (r *Repository) FindASTNodesByParent(parentNodeID int64) ([]*types.AstNode, error) {
	rows, err := r.db.Query(`
		SELECT id, file, line, kind, name, text, receiver, parent_node_id
		FROM ast_nodes WHERE parent_node_id = ?
		ORDER BY line
	`, parentNodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*types.AstNode
	for rows.Next() {
		node := &types.AstNode{}
		if err := rows.Scan(&node.ID, &node.File, &node.Line, &node.Kind, &node.Name, &node.Text, &node.Receiver, &node.ParentNodeID); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// GetTopCoChanges returns the top co-change pairs by commit count.
func (r *Repository) GetTopCoChanges(limit, minCount int) ([]*types.CoChange, error) {
	rows, err := r.db.Query(`
		SELECT file_a, file_b, commit_count, jaccard, last_commit_epoch
		FROM co_changes
		WHERE commit_count >= ?
		ORDER BY commit_count DESC
		LIMIT ?
	`, minCount, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var coChanges []*types.CoChange
	for rows.Next() {
		cc := &types.CoChange{}
		if err := rows.Scan(&cc.FileA, &cc.FileB, &cc.CommitCount, &cc.Jaccard, &cc.LastCommitTime); err != nil {
			return nil, err
		}
		coChanges = append(coChanges, cc)
	}
	return coChanges, nil
}