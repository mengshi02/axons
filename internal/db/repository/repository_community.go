// Package repository provides database operations for graph data.
package repository

import "github.com/mengshi02/axons/pkg/types"

// SetNodeCommunity sets the community_id for a node (used by Louvain community detection).
func (r *Repository) SetNodeCommunity(nodeID int64, communityID int64) error {
	_, err := r.db.Exec(`UPDATE nodes SET community_id = ? WHERE id = ?`, communityID, nodeID)
	return err
}

// BatchSetNodeCommunity sets the community_id for multiple nodes in a single transaction.
// This is dramatically faster than calling SetNodeCommunity individually.
func (r *Repository) BatchSetNodeCommunity(nodeCommunityIDs map[int64]int64) error {
	if len(nodeCommunityIDs) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE nodes SET community_id = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for nodeID, communityID := range nodeCommunityIDs {
		_, err := stmt.Exec(communityID, nodeID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetNodeCommunityID returns the community_id for a node.
func (r *Repository) GetNodeCommunityID(nodeID int64) (int64, error) {
	var communityID int64
	err := r.db.QueryRow(`SELECT COALESCE(community_id, 0) FROM nodes WHERE id = ?`, nodeID).Scan(&communityID)
	if err != nil {
		return 0, err
	}
	return communityID, nil
}

// GetNodesByCommunity returns all nodes in a given community.
func (r *Repository) GetNodesByCommunity(communityID int64) ([]*types.Node, error) {
	rows, err := r.db.Query(`
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes WHERE community_id = ?
	`, communityID)
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
		node.CommunityID = &communityID
		nodes = append(nodes, node)
	}
	return nodes, nil
}