// Package repository provides data access layer.
package repository

import (
	"database/sql"

	"github.com/mengshi02/axons/pkg/types"
)

// Repository provides data access methods.
type Repository struct {
	db *sql.DB
}

// ComplexityMetrics represents complexity metrics for a function.
type ComplexityMetrics struct {
	NodeID             int64
	Cyclomatic         int
	Cognitive          int
	Nesting            int
	LinesOfCode        int
	HalsteadVolume     float64
	HalsteadDifficulty float64
	HalsteadEffort     float64
	HalsteadTime       float64
	HalsteadBugs       float64
}

// New creates a new Repository.
func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// DB returns the underlying database connection.
func (r *Repository) DB() *sql.DB {
	return r.db
}

// InsertNode inserts a node and returns its ID.
func (r *Repository) InsertNode(node *types.Node) (int64, error) {
	result, err := r.db.Exec(`
		INSERT INTO nodes (name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, node.Name, node.Kind, node.File, node.Line, node.EndLine, node.ParentID, node.Exported, node.QualifiedName, node.Scope, node.Visibility, node.Role, node.FileHash)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// InsertEdge inserts an edge.
func (r *Repository) InsertEdge(edge *types.Edge) (int64, error) {
	result, err := r.db.Exec(`
		INSERT INTO edges (source_id, target_id, kind, confidence, dynamic)
		VALUES (?, ?, ?, ?, ?)
	`, edge.SourceID, edge.TargetID, edge.Kind, edge.Confidence, edge.Dynamic)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// FindNodeByID finds a node by ID.
func (r *Repository) FindNodeByID(id int64) (*types.Node, error) {
	node := &types.Node{}
	err := r.db.QueryRow(`
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes WHERE id = ?
	`, id).Scan(&node.ID, &node.Name, &node.Kind, &node.File, &node.Line, &node.EndLine, &node.ParentID, &node.Exported, &node.QualifiedName, &node.Scope, &node.Visibility, &node.Role, &node.FileHash)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// FindNodesByName finds nodes by name.
func (r *Repository) FindNodesByName(name string, limit int) ([]*types.Node, error) {
	query := `
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes WHERE name LIKE ?
	`
	if limit > 0 {
		query += " LIMIT ?"
	}
	
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = r.db.Query(query, "%"+name+"%", limit)
	} else {
		rows, err = r.db.Query(query, "%"+name+"%")
	}
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

// FindNodesByFile finds nodes by file path.
func (r *Repository) FindNodesByFile(file string) ([]*types.Node, error) {
	rows, err := r.db.Query(`
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes WHERE file = ?
	`, file)
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

// FindCallers finds nodes that call the given node.
func (r *Repository) FindCallers(nodeID int64) ([]*types.Node, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.parent_id, n.exported, n.qualified_name, n.scope, n.visibility, n.role, n.file_hash
		FROM edges e
		JOIN nodes n ON e.source_id = n.id
		WHERE e.target_id = ? AND e.kind = 'calls'
	`, nodeID)
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

// FindCallees finds nodes that are called by the given node.
func (r *Repository) FindCallees(nodeID int64) ([]*types.Node, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.parent_id, n.exported, n.qualified_name, n.scope, n.visibility, n.role, n.file_hash
		FROM edges e
		JOIN nodes n ON e.target_id = n.id
		WHERE e.source_id = ? AND e.kind = 'calls'
	`, nodeID)
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

// DeleteNodesByFile deletes all nodes for a file.
func (r *Repository) DeleteNodesByFile(file string) error {
	_, err := r.db.Exec("DELETE FROM nodes WHERE file = ?", file)
	return err
}

// FindNodeIDsByFile returns all node IDs for a file.
func (r *Repository) FindNodeIDsByFile(file string) ([]int64, error) {
	rows, err := r.db.Query("SELECT id FROM nodes WHERE file = ?", file)
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

// FindEdgeIDsByFile returns all edge IDs connected to nodes in a file.
func (r *Repository) FindEdgeIDsByFile(file string) ([]int64, error) {
	rows, err := r.db.Query(`
		SELECT e.id FROM edges e
		WHERE e.source_id IN (SELECT id FROM nodes WHERE file = ?)
		   OR e.target_id IN (SELECT id FROM nodes WHERE file = ?)
	`, file, file)
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

// DeleteEdgesByFile deletes all edges for nodes in a file.
func (r *Repository) DeleteEdgesByFile(file string) error {
	_, err := r.db.Exec(`
		DELETE FROM edges WHERE source_id IN (SELECT id FROM nodes WHERE file = ?)
		OR target_id IN (SELECT id FROM nodes WHERE file = ?)
	`, file, file)
	return err
}

// DeleteEdgesBetweenFiles deletes edges where one end is in sourceFile and the other is in targetFile.
func (r *Repository) DeleteEdgesBetweenFiles(sourceFile, targetFile string) error {
	_, err := r.db.Exec(`
		DELETE FROM edges WHERE 
		(source_id IN (SELECT id FROM nodes WHERE file = ?) AND target_id IN (SELECT id FROM nodes WHERE file = ?))
		OR
		(source_id IN (SELECT id FROM nodes WHERE file = ?) AND target_id IN (SELECT id FROM nodes WHERE file = ?))
	`, sourceFile, targetFile, targetFile, sourceFile)
	return err
}

// FindEdgeIDsBetweenFiles returns edge IDs where one end is in sourceFile and the other in targetFile.
func (r *Repository) FindEdgeIDsBetweenFiles(sourceFile, targetFile string) ([]int64, error) {
	rows, err := r.db.Query(`
		SELECT e.id FROM edges e
		WHERE (e.source_id IN (SELECT id FROM nodes WHERE file = ?) AND e.target_id IN (SELECT id FROM nodes WHERE file = ?))
		OR (e.source_id IN (SELECT id FROM nodes WHERE file = ?) AND e.target_id IN (SELECT id FROM nodes WHERE file = ?))
	`, sourceFile, targetFile, targetFile, sourceFile)
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

// FindFilesWithEdgesTo finds all files that have nodes connected via edges
// to nodes in the specified file. This is used during incremental builds to
// identify files that need their edges rebuilt when a file changes.
func (r *Repository) FindFilesWithEdgesTo(file string) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT n2.file FROM edges e
		JOIN nodes n1 ON (e.source_id = n1.id OR e.target_id = n1.id)
		JOIN nodes n2 ON (e.source_id = n2.id OR e.target_id = n2.id)
		WHERE n1.file = ? AND n2.file != ?
	`, file, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			continue
		}
		files = append(files, f)
	}
	return files, nil
}

// FindImporterFiles finds all files that import (directly or transitively) from
// the specified file, using BFS up to maxDepth hops along IMPORTS edges.
// This is used during incremental builds to expand the write set beyond
// directly changed files, ensuring cross-file edge consistency.
func (r *Repository) FindImporterFiles(file string, maxDepth int) ([]string, error) {
	if maxDepth <= 0 {
		maxDepth = 4
	}

	visited := map[string]bool{file: true}
	frontier := []string{file}
	var result []string

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var nextFrontier []string
		for _, f := range frontier {
			// Find files that have an IMPORTS edge pointing to nodes in file f
			rows, err := r.db.Query(`
				SELECT DISTINCT n2.file FROM edges e
				JOIN nodes n1 ON e.target_id = n1.id
				JOIN nodes n2 ON e.source_id = n2.id
				WHERE n1.file = ? AND n2.file != ? AND e.kind = 'IMPORTS'
			`, f, f)
			if err != nil {
				continue
			}
			for rows.Next() {
				var importer string
				if err := rows.Scan(&importer); err != nil {
					continue
				}
				if !visited[importer] {
					visited[importer] = true
					nextFrontier = append(nextFrontier, importer)
					result = append(result, importer)
				}
			}
			rows.Close()
		}
		frontier = nextFrontier
	}
	return result, nil
}

// FindOneHopNeighborFiles finds files that share an edge with nodes in the specified file,
// expanding one hop in any direction. This catches cross-file edges (e.g., CALLS from
// an unchanged file to a changed file) that BFS importers might miss.
func (r *Repository) FindOneHopNeighborFiles(file string) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT n2.file FROM edges e
		JOIN nodes n1 ON (e.source_id = n1.id OR e.target_id = n1.id)
		JOIN nodes n2 ON (e.source_id = n2.id OR e.target_id = n2.id)
		WHERE n1.file = ? AND n2.file != ?
	`, file, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []string
	seen := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			continue
		}
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	return files, nil
}

// FindFilesImportingFromPath finds files that import from the given module path
// (used for shadow-seed detection when a new file might "steal" an import resolution).
func (r *Repository) FindFilesImportingFromPath(modulePath string) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT n2.file FROM edges e
		JOIN nodes n1 ON e.target_id = n1.id
		JOIN nodes n2 ON e.source_id = n2.id
		WHERE e.kind = 'IMPORTS' AND n1.qualified_name LIKE ?
	`, modulePath+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			continue
		}
		files = append(files, f)
	}
	return files, nil
}

// FileMeta holds stored file metadata for fast change detection.
type FileMeta struct {
	Mtime int64
	Size  int64
	Hash  string
}

// GetFileHash gets the stored hash for a file.
func (r *Repository) GetFileHash(file string) (string, error) {
	var hash string
	err := r.db.QueryRow("SELECT hash FROM files WHERE path = ?", file).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

// GetFileMeta gets the stored metadata (mtime, size, hash) for a file.
// Returns nil if the file is not found in the database.
func (r *Repository) GetFileMeta(file string) (*FileMeta, error) {
	var meta FileMeta
	err := r.db.QueryRow("SELECT mtime, size, hash FROM files WHERE path = ?", file).Scan(&meta.Mtime, &meta.Size, &meta.Hash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

// GetAllFileMeta gets the stored metadata for all indexed files.
// Returns a map from file path to FileMeta.
func (r *Repository) GetAllFileMeta() (map[string]*FileMeta, error) {
	rows, err := r.db.Query("SELECT path, mtime, size, hash FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*FileMeta)
	for rows.Next() {
		var path string
		var meta FileMeta
		if err := rows.Scan(&path, &meta.Mtime, &meta.Size, &meta.Hash); err != nil {
			return nil, err
		}
		result[path] = &meta
	}
	return result, rows.Err()
}

// UpsertFileHash updates or inserts a file hash.
func (r *Repository) UpsertFileHash(file string, mtime, size int64, hash string) error {
	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO files (path, mtime, size, hash) VALUES (?, ?, ?, ?)
	`, file, mtime, size, hash)
	return err
}

// GetMetadata gets a metadata value.
func (r *Repository) GetMetadata(key string) (string, error) {
	var value string
	err := r.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetMetadata sets a metadata value.
func (r *Repository) SetMetadata(key, value string) error {
	_, err := r.db.Exec("INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)", key, value)
	return err
}

// CountNodes returns the total number of nodes.
func (r *Repository) CountNodes() (int64, error) {
	var count int64
	err := r.db.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count)
	return count, err
}

// CountEdges returns the total number of edges.
func (r *Repository) CountEdges() (int64, error) {
	var count int64
	err := r.db.QueryRow("SELECT COUNT(*) FROM edges").Scan(&count)
	return count, err
}

// ListFunctionNodes lists all function/method nodes.
func (r *Repository) ListFunctionNodes(limit, offset int) ([]*types.Node, error) {
	rows, err := r.db.Query(`
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes WHERE kind IN ('function', 'method')
		ORDER BY file, line
		LIMIT ? OFFSET ?
	`, limit, offset)
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

// CreateNode creates a new node and sets the ID on the node.
// Uses INSERT OR IGNORE to skip duplicates (based on unique constraint on name, kind, file, line).
// If the node already exists, it queries the existing ID.
func (r *Repository) CreateNode(node *types.Node) error {
	result, err := r.db.Exec(`
		INSERT OR IGNORE INTO nodes (name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, node.Name, node.Kind, node.File, node.Line, node.EndLine, node.ParentID, node.Exported, node.QualifiedName, node.Scope, node.Visibility, node.Role, node.FileHash)
	if err != nil {
		return err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		// New node was inserted
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		node.ID = id
		return nil
	}
	// Node already exists — look up its ID (only on conflict)
	existingNode, err := r.FindNodeByUniqueKey(node.Name, node.Kind, node.File, node.Line)
	if err != nil {
		return err
	}
	node.ID = existingNode.ID
	return nil
}

// FindNodeByUniqueKey finds a node by its unique key (name, kind, file, line).
func (r *Repository) FindNodeByUniqueKey(name string, kind types.SymbolKind, file string, line int) (*types.Node, error) {
	node := &types.Node{}
	err := r.db.QueryRow(`
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes WHERE name = ? AND kind = ? AND file = ? AND line = ?
	`, name, kind, file, line).Scan(&node.ID, &node.Name, &node.Kind, &node.File, &node.Line, &node.EndLine, &node.ParentID, &node.Exported, &node.QualifiedName, &node.Scope, &node.Visibility, &node.Role, &node.FileHash)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// CreateEdge creates a new edge.
// If an edge with the same (source_id, target_id, kind) already exists, it does nothing.
// Returns true if the edge was actually inserted, false if it was ignored (already existed).
func (r *Repository) CreateEdge(edge *types.Edge) (bool, error) {
	result, err := r.db.Exec(`
		INSERT OR IGNORE INTO edges (source_id, target_id, kind, confidence, dynamic)
		VALUES (?, ?, ?, ?, ?)
	`, edge.SourceID, edge.TargetID, edge.Kind, edge.Confidence, edge.Dynamic)
	if err != nil {
		return false, err
	}
	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// GetAllFiles returns all indexed files.
func (r *Repository) GetAllFiles() ([]string, error) {
	rows, err := r.db.Query("SELECT DISTINCT file FROM nodes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []string
	for rows.Next() {
		var file string
		if err := rows.Scan(&file); err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}
	
	// FindNodesByKind finds nodes by symbol kind.
	func (r *Repository) FindNodesByKind(kind types.SymbolKind, limit, offset int) ([]*types.Node, error) {
		query := `
			SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
			FROM nodes WHERE kind = ?
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
	
	// FindNodesByRole finds nodes by role.
	func (r *Repository) FindNodesByRole(role types.Role, limit, offset int) ([]*types.Node, error) {
		query := `
			SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
			FROM nodes WHERE role = ?
			ORDER BY file, line
		`
		args := []interface{}{role}
		if limit > 0 {
			query += " LIMIT ? OFFSET ?"
			args = append(args, limit, offset)
		}
	
		rows, err := r.db.Query(query, args...)
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
	
	// FindNodesByQualifiedName finds a node by its qualified name.
	func (r *Repository) FindNodesByQualifiedName(qualifiedName string) ([]*types.Node, error) {
		rows, err := r.db.Query(`
			SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
			FROM nodes WHERE qualified_name = ?
		`, qualifiedName)
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
	
	// FindEdgesByKind finds edges by kind.
	func (r *Repository) FindEdgesByKind(kind types.EdgeKind, limit, offset int) ([]*types.Edge, error) {
		query := `
			SELECT id, source_id, target_id, kind, confidence, dynamic
			FROM edges WHERE kind = ?
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
	
	// FindEdgesBySource finds edges by source node ID.
	func (r *Repository) FindEdgesBySource(sourceID int64) ([]*types.Edge, error) {
		rows, err := r.db.Query(`
			SELECT id, source_id, target_id, kind, confidence, dynamic
			FROM edges WHERE source_id = ?
		`, sourceID)
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
	
	// FindEdgesByTarget finds edges by target node ID.
	func (r *Repository) FindEdgesByTarget(targetID int64) ([]*types.Edge, error) {
		rows, err := r.db.Query(`
			SELECT id, source_id, target_id, kind, confidence, dynamic
			FROM edges WHERE target_id = ?
		`, targetID)
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
	
	// FindNodesByFileAndKind finds nodes by file and kind.
	func (r *Repository) FindNodesByFileAndKind(file string, kind types.SymbolKind) ([]*types.Node, error) {
		rows, err := r.db.Query(`
			SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
			FROM nodes WHERE file = ? AND kind = ?
			ORDER BY line
		`, file, kind)
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
	
	// FindExportedNodes finds all exported nodes.
	func (r *Repository) FindExportedNodes(limit, offset int) ([]*types.Node, error) {
		query := `
			SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
			FROM nodes WHERE exported = 1
			ORDER BY file, line
		`
		args := []interface{}{}
		if limit > 0 {
			query += " LIMIT ? OFFSET ?"
			args = append(args, limit, offset)
		}
	
		rows, err := r.db.Query(query, args...)
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
	
	// FindNodeWithSignature finds a node matching name, file, and line for signature verification.
	func (r *Repository) FindNodeWithSignature(name, file string, line int) (*types.Node, error) {
		node := &types.Node{}
		err := r.db.QueryRow(`
			SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
			FROM nodes WHERE name = ? AND file = ? AND line = ?
		`, name, file, line).Scan(&node.ID, &node.Name, &node.Kind, &node.File, &node.Line, &node.EndLine, &node.ParentID, &node.Exported, &node.QualifiedName, &node.Scope, &node.Visibility, &node.Role, &node.FileHash)
		if err != nil {
			return nil, err
		}
		return node, nil
	}
	
	// CountNodesByKind counts nodes by symbol kind.
	func (r *Repository) CountNodesByKind(kind types.SymbolKind) (int64, error) {
		var count int64
		err := r.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE kind = ?", kind).Scan(&count)
		return count, err
	}
	
	// CountNodesByRole counts nodes by role.
	func (r *Repository) CountNodesByRole(role types.Role) (int64, error) {
		var count int64
		err := r.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE role = ?", role).Scan(&count)
		return count, err
	}
	
	// CountEdgesByKind counts edges by kind.
	func (r *Repository) CountEdgesByKind(kind types.EdgeKind) (int64, error) {
		var count int64
		err := r.db.QueryRow("SELECT COUNT(*) FROM edges WHERE kind = ?", kind).Scan(&count)
		return count, err
	}
	
	// SearchNodes performs a full-text search on node names.
	func (r *Repository) SearchNodes(query string, limit int) ([]*types.Node, error) {
		rows, err := r.db.Query(`
			SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
			FROM nodes WHERE name LIKE ? OR qualified_name LIKE ?
			ORDER BY
				CASE WHEN name = ? THEN 0
				     WHEN name LIKE ? THEN 1
				     ELSE 2 END,
				name
			LIMIT ?
		`, "%"+query+"%", "%"+query+"%", query, query+"%", limit)
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
	
	// GetStats returns database statistics.
	func (r *Repository) GetStats() (*Stats, error) {
		stats := &Stats{}
	
		// Node counts by kind
		rows, err := r.db.Query("SELECT kind, COUNT(*) FROM nodes GROUP BY kind")
		if err != nil {
			return nil, err
		}
		stats.NodesByKind = make(map[types.SymbolKind]int64)
		for rows.Next() {
			var kind types.SymbolKind
			var count int64
			if err := rows.Scan(&kind, &count); err != nil {
				rows.Close()
				return nil, err
			}
			stats.NodesByKind[kind] = count
		}
		rows.Close()
	
		// Edge counts by kind
		rows, err = r.db.Query("SELECT kind, COUNT(*) FROM edges GROUP BY kind")
		if err != nil {
			return nil, err
		}
		stats.EdgesByKind = make(map[types.EdgeKind]int64)
		for rows.Next() {
			var kind types.EdgeKind
			var count int64
			if err := rows.Scan(&kind, &count); err != nil {
				rows.Close()
				return nil, err
			}
			stats.EdgesByKind[kind] = count
		}
		rows.Close()
	
		// Role counts
		rows, err = r.db.Query("SELECT role, COUNT(*) FROM nodes WHERE role != '' GROUP BY role")
		if err != nil {
			return nil, err
		}
		stats.NodesByRole = make(map[types.Role]int64)
		for rows.Next() {
			var role types.Role
			var count int64
			if err := rows.Scan(&role, &count); err != nil {
				rows.Close()
				return nil, err
			}
			stats.NodesByRole[role] = count
		}
		rows.Close()
	
		// Total counts
		stats.TotalNodes, err = r.CountNodes()
		if err != nil {
			return nil, err
		}
		stats.TotalEdges, err = r.CountEdges()
		if err != nil {
			return nil, err
		}
	
		// File count
		stats.TotalFiles, err = r.CountFiles()
		if err != nil {
			return nil, err
		}
	
		return stats, nil
	}
	
	// CountFiles returns the total number of indexed files.
	func (r *Repository) CountFiles() (int64, error) {
		var count int64
		err := r.db.QueryRow("SELECT COUNT(DISTINCT file) FROM nodes").Scan(&count)
		return count, err
	}
	
	// Stats represents database statistics.
	type Stats struct {
		TotalNodes   int64                      `json:"total_nodes"`
		TotalEdges   int64                      `json:"total_edges"`
		TotalFiles   int64                      `json:"total_files"`
		NodesByKind  map[types.SymbolKind]int64 `json:"nodes_by_kind"`
		EdgesByKind  map[types.EdgeKind]int64   `json:"edges_by_kind"`
		NodesByRole  map[types.Role]int64       `json:"nodes_by_role"`
	}
	
	// FindDescendants finds all descendant nodes of a given node.
	func (r *Repository) FindDescendants(nodeID int64) ([]*types.Node, error) {
		// Use recursive CTE to find all descendants
		rows, err := r.db.Query(`
			WITH RECURSIVE descendants AS (
				SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
				FROM nodes WHERE parent_id = ?
				UNION ALL
				SELECT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.parent_id, n.exported, n.qualified_name, n.scope, n.visibility, n.role, n.file_hash
				FROM nodes n
				INNER JOIN descendants d ON n.parent_id = d.id
			)
			SELECT * FROM descendants
		`, nodeID)
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
	
	// FindAncestors finds all ancestor nodes of a given node.
	func (r *Repository) FindAncestors(nodeID int64) ([]*types.Node, error) {
		// Use recursive CTE to find all ancestors
		rows, err := r.db.Query(`
			WITH RECURSIVE ancestors AS (
				SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
				FROM nodes WHERE id = (SELECT parent_id FROM nodes WHERE id = ?)
				UNION ALL
				SELECT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.parent_id, n.exported, n.qualified_name, n.scope, n.visibility, n.role, n.file_hash
				FROM nodes n
				INNER JOIN ancestors a ON n.id = a.parent_id
			)
			SELECT * FROM ancestors
		`, nodeID)
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
	
	// FindInheritance finds inheritance relationships for a class/interface.
	func (r *Repository) FindInheritance(nodeID int64) ([]*types.Node, error) {
		rows, err := r.db.Query(`
			SELECT DISTINCT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.parent_id, n.exported, n.qualified_name, n.scope, n.visibility, n.role, n.file_hash
			FROM edges e
			JOIN nodes n ON e.target_id = n.id
			WHERE e.source_id = ? AND e.kind IN ('extends', 'implements')
		`, nodeID)
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
	
	// FindImplementations finds implementations of an interface.
	func (r *Repository) FindImplementations(nodeID int64) ([]*types.Node, error) {
		rows, err := r.db.Query(`
			SELECT DISTINCT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.parent_id, n.exported, n.qualified_name, n.scope, n.visibility, n.role, n.file_hash
			FROM edges e
			JOIN nodes n ON e.source_id = n.id
			WHERE e.target_id = ? AND e.kind = 'implements'
		`, nodeID)
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
	
	// BatchInsertNodes inserts multiple nodes in a transaction.
	// Uses INSERT OR IGNORE to skip nodes that already exist (based on unique constraint).
	func (r *Repository) BatchInsertNodes(nodes []*types.Node) error {
		tx, err := r.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
	
		stmt, err := tx.Prepare(`
			INSERT OR IGNORE INTO nodes (name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
	
		for _, node := range nodes {
			_, err := stmt.Exec(node.Name, node.Kind, node.File, node.Line, node.EndLine, node.ParentID, node.Exported, node.QualifiedName, node.Scope, node.Visibility, node.Role, node.FileHash)
			if err != nil {
				return err
			}
		}
	
		return tx.Commit()
	}
	
	// BatchInsertNodesWithIDs inserts multiple nodes in a transaction and backfills their IDs.
	// Uses INSERT OR IGNORE to skip duplicates. After the transaction commits, it queries
	// each node's ID from the database using the unique key (name, kind, file, line).
	// This is the preferred method for graph building where node IDs are needed for edge creation.
	func (r *Repository) BatchInsertNodesWithIDs(nodes []*types.Node) (inserted int, err error) {
		if len(nodes) == 0 {
			return 0, nil
		}
	
		tx, err := r.db.Begin()
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()
	
		stmt, err := tx.Prepare(`
			INSERT OR IGNORE INTO nodes (name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return 0, err
		}
		defer stmt.Close()
	
		for _, node := range nodes {
			result, err := stmt.Exec(node.Name, node.Kind, node.File, node.Line, node.EndLine, node.ParentID, node.Exported, node.QualifiedName, node.Scope, node.Visibility, node.Role, node.FileHash)
			if err != nil {
				return 0, err
			}
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected > 0 {
				id, err := result.LastInsertId()
				if err != nil {
					return 0, err
				}
				node.ID = id
				inserted++
			}
		}
	
		if err := tx.Commit(); err != nil {
			return 0, err
		}
	
		// Backfill IDs for nodes that were ignored (duplicates) — query only those with ID == 0
		for _, node := range nodes {
			if node.ID == 0 {
				err := r.findNodeIDByUniqueKey(node)
				if err != nil {
					// Non-fatal: log but continue, the node exists but we couldn't get its ID
					continue
				}
			}
		}
	
		return inserted, nil
	}
	
	// findNodeIDByUniqueKey sets node.ID by querying the database for the node's unique key.
	func (r *Repository) findNodeIDByUniqueKey(node *types.Node) error {
		var id int64
		err := r.db.QueryRow(`
			SELECT id FROM nodes WHERE name = ? AND kind = ? AND file = ? AND line = ?
		`, node.Name, node.Kind, node.File, node.Line).Scan(&id)
		if err != nil {
			return err
		}
		node.ID = id
		return nil
	}
	
	// BatchInsertEdges inserts multiple edges in a transaction.
	// Uses INSERT OR IGNORE to skip duplicates (based on unique index on source_id, target_id, kind).
	// Returns the number of edges actually inserted.
	func (r *Repository) BatchInsertEdges(edges []*types.Edge) (int, error) {
		if len(edges) == 0 {
			return 0, nil
		}
	
		tx, err := r.db.Begin()
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()
	
		stmt, err := tx.Prepare(`
			INSERT OR IGNORE INTO edges (source_id, target_id, kind, confidence, dynamic)
			VALUES (?, ?, ?, ?, ?)
		`)
		if err != nil {
			return 0, err
		}
		defer stmt.Close()
	
		inserted := 0
		for _, edge := range edges {
			result, err := stmt.Exec(edge.SourceID, edge.TargetID, edge.Kind, edge.Confidence, edge.Dynamic)
			if err != nil {
				return 0, err
			}
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected > 0 {
				inserted++
			}
		}
	
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return inserted, nil
	}
	
	// ClearAll clears all nodes and edges from the database.
	func (r *Repository) ClearAll() error {
		_, err := r.db.Exec("DELETE FROM edges")
		if err != nil {
			return err
		}
		_, err = r.db.Exec("DELETE FROM nodes")
		if err != nil {
			return err
		}
		_, err = r.db.Exec("DELETE FROM files")
		if err != nil {
			return err
		}
		return nil
	}
	
	// UpdateNodeRole updates the role of a node.
	func (r *Repository) UpdateNodeRole(nodeID int64, role types.Role) error {
		_, err := r.db.Exec("UPDATE nodes SET role = ? WHERE id = ?", role, nodeID)
		return err
	}
	
	// BatchUpdateNodeRoles updates roles for multiple nodes.
	func (r *Repository) BatchUpdateNodeRoles(updates map[int64]types.Role) error {
		tx, err := r.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
	
		stmt, err := tx.Prepare("UPDATE nodes SET role = ? WHERE id = ?")
		if err != nil {
			return err
		}
		defer stmt.Close()
	
		for nodeID, role := range updates {
			_, err := stmt.Exec(role, nodeID)
			if err != nil {
				return err
			}
		}

		return tx.Commit()
	}

	// SaveComplexityMetrics saves complexity metrics for a node.
	func (r *Repository) SaveComplexityMetrics(metrics *ComplexityMetrics) error {
		_, err := r.db.Exec(`
			INSERT OR REPLACE INTO function_complexity
			(node_id, cyclomatic, cognitive, nesting, lines_of_code,
			 halstead_volume, halstead_difficulty, halstead_effort, halstead_time, halstead_bugs)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, metrics.NodeID, metrics.Cyclomatic, metrics.Cognitive, metrics.Nesting,
			metrics.LinesOfCode, metrics.HalsteadVolume, metrics.HalsteadDifficulty,
			metrics.HalsteadEffort, metrics.HalsteadTime, metrics.HalsteadBugs)
		return err
	}

	// GetComplexityMetrics gets complexity metrics for a node.
	func (r *Repository) GetComplexityMetrics(nodeID int64) (*ComplexityMetrics, error) {
		metrics := &ComplexityMetrics{NodeID: nodeID}
		err := r.db.QueryRow(`
			SELECT node_id, cyclomatic, cognitive, nesting, lines_of_code,
				   halstead_volume, halstead_difficulty, halstead_effort, halstead_time, halstead_bugs
			FROM function_complexity WHERE node_id = ?
		`, nodeID).Scan(&metrics.NodeID, &metrics.Cyclomatic, &metrics.Cognitive,
			&metrics.Nesting, &metrics.LinesOfCode, &metrics.HalsteadVolume,
			&metrics.HalsteadDifficulty, &metrics.HalsteadEffort,
			&metrics.HalsteadTime, &metrics.HalsteadBugs)
		if err != nil {
			return nil, err
		}
		return metrics, nil
	}

	// GetComplexityByFile gets complexity metrics for all functions in a file.
	func (r *Repository) GetComplexityByFile(filePath string) ([]*ComplexityMetrics, error) {
		rows, err := r.db.Query(`
			SELECT fc.node_id, fc.cyclomatic, fc.cognitive, fc.nesting, fc.lines_of_code,
				   fc.halstead_volume, fc.halstead_difficulty, fc.halstead_effort, fc.halstead_time, fc.halstead_bugs
			FROM function_complexity fc
			JOIN nodes n ON fc.node_id = n.id
			WHERE n.file = ?
		`, filePath)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var metrics []*ComplexityMetrics
		for rows.Next() {
			m := &ComplexityMetrics{}
			if err := rows.Scan(&m.NodeID, &m.Cyclomatic, &m.Cognitive, &m.Nesting,
				&m.LinesOfCode, &m.HalsteadVolume, &m.HalsteadDifficulty,
				&m.HalsteadEffort, &m.HalsteadTime, &m.HalsteadBugs); err != nil {
				return nil, err
			}
			metrics = append(metrics, m)
		}
		return metrics, nil
	}

	// GetHighComplexityFunctions gets functions with high cyclomatic complexity.
	func (r *Repository) GetHighComplexityFunctions(threshold int, limit int) ([]*ComplexityMetrics, error) {
		rows, err := r.db.Query(`
			SELECT node_id, cyclomatic, cognitive, nesting, lines_of_code,
				   halstead_volume, halstead_difficulty, halstead_effort, halstead_time, halstead_bugs
			FROM function_complexity
			WHERE cyclomatic >= ?
			ORDER BY cyclomatic DESC
			LIMIT ?
		`, threshold, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var metrics []*ComplexityMetrics
		for rows.Next() {
			m := &ComplexityMetrics{}
			if err := rows.Scan(&m.NodeID, &m.Cyclomatic, &m.Cognitive, &m.Nesting,
				&m.LinesOfCode, &m.HalsteadVolume, &m.HalsteadDifficulty,
				&m.HalsteadEffort, &m.HalsteadTime, &m.HalsteadBugs); err != nil {
				return nil, err
			}
			metrics = append(metrics, m)
		}
		return metrics, nil
	}

	// BatchSaveComplexityMetrics saves multiple complexity metrics in a transaction.
	func (r *Repository) BatchSaveComplexityMetrics(metrics []*ComplexityMetrics) error {
		tx, err := r.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		stmt, err := tx.Prepare(`
			INSERT OR REPLACE INTO function_complexity
			(node_id, cyclomatic, cognitive, nesting, lines_of_code,
			 halstead_volume, halstead_difficulty, halstead_effort, halstead_time, halstead_bugs)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, m := range metrics {
			_, err := stmt.Exec(m.NodeID, m.Cyclomatic, m.Cognitive, m.Nesting,
				m.LinesOfCode, m.HalsteadVolume, m.HalsteadDifficulty,
				m.HalsteadEffort, m.HalsteadTime, m.HalsteadBugs)
			if err != nil {
				return err
			}
		}

		return tx.Commit()
	}

	// GetComplexityStats gets overall complexity statistics.
	func (r *Repository) GetComplexityStats() (map[string]interface{}, error) {
		stats := make(map[string]interface{})

		// Average cyclomatic complexity
		var avgCyclomatic float64
		err := r.db.QueryRow("SELECT AVG(cyclomatic) FROM function_complexity").Scan(&avgCyclomatic)
		if err == nil {
			stats["avg_cyclomatic"] = avgCyclomatic
		}

		// Max cyclomatic complexity
		var maxCyclomatic int
		err = r.db.QueryRow("SELECT MAX(cyclomatic) FROM function_complexity").Scan(&maxCyclomatic)
		if err == nil {
			stats["max_cyclomatic"] = maxCyclomatic
		}

		// Average cognitive complexity
		var avgCognitive float64
		err = r.db.QueryRow("SELECT AVG(cognitive) FROM function_complexity").Scan(&avgCognitive)
		if err == nil {
			stats["avg_cognitive"] = avgCognitive
		}

		// Functions with high complexity (> 10)
		var highComplexityCount int
		err = r.db.QueryRow("SELECT COUNT(*) FROM function_complexity WHERE cyclomatic > 10").Scan(&highComplexityCount)
		if err == nil {
			stats["high_complexity_count"] = highComplexityCount
		}

		// Total lines of code
		var totalLOC int
		err = r.db.QueryRow("SELECT SUM(lines_of_code) FROM function_complexity").Scan(&totalLOC)
		if err == nil {
			stats["total_loc"] = totalLOC
		}

		return stats, nil
	}

	// SaveCFGBlock saves a CFG block.
	func (r *Repository) SaveCFGBlock(nodeID int64, blockType string, startLine, endLine int) (int64, error) {
		result, err := r.db.Exec(`
			INSERT INTO cfg_blocks (node_id, block_type, start_line, end_line)
			VALUES (?, ?, ?, ?)
		`, nodeID, blockType, startLine, endLine)
		if err != nil {
			return 0, err
		}
		return result.LastInsertId()
	}

	// SaveCFGEdge saves a CFG edge.
	func (r *Repository) SaveCFGEdge(sourceBlockID, targetBlockID int64, edgeType string) error {
		_, err := r.db.Exec(`
			INSERT INTO cfg_edges (source_block_id, target_block_id, edge_type)
			VALUES (?, ?, ?)
		`, sourceBlockID, targetBlockID, edgeType)
		return err
	}

	// GetCFGForNode gets CFG blocks and edges for a node.
	func (r *Repository) GetCFGForNode(nodeID int64) (blocks []map[string]interface{}, edges []map[string]interface{}, err error) {
		// Get blocks
		rows, err := r.db.Query(`
			SELECT id, block_type, start_line, end_line
			FROM cfg_blocks WHERE node_id = ?
			ORDER BY start_line
		`, nodeID)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()

		blockIDMap := make(map[int64]int)
		for rows.Next() {
			var id int64
			var blockType string
			var startLine, endLine int
			if err := rows.Scan(&id, &blockType, &startLine, &endLine); err != nil {
				return nil, nil, err
			}
			blocks = append(blocks, map[string]interface{}{
				"id":         id,
				"block_type": blockType,
				"start_line": startLine,
				"end_line":   endLine,
			})
			blockIDMap[id] = len(blocks) - 1
		}
		rows.Close()

		// Get edges
		rows, err = r.db.Query(`
			SELECT e.source_block_id, e.target_block_id, e.edge_type
			FROM cfg_edges e
			JOIN cfg_blocks b1 ON e.source_block_id = b1.id
			JOIN cfg_blocks b2 ON e.target_block_id = b2.id
			WHERE b1.node_id = ? AND b2.node_id = ?
		`, nodeID, nodeID)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var sourceID, targetID int64
			var edgeType string
			if err := rows.Scan(&sourceID, &targetID, &edgeType); err != nil {
				return nil, nil, err
			}
			edges = append(edges, map[string]interface{}{
				"source_id": sourceID,
				"target_id": targetID,
				"edge_type": edgeType,
			})
		}

		return blocks, edges, nil
	}

	// ClearCFGForNode clears CFG data for a node.
	func (r *Repository) ClearCFGForNode(nodeID int64) error {
		// Delete edges first (due to foreign key constraints)
		_, err := r.db.Exec(`
			DELETE FROM cfg_edges WHERE source_block_id IN
			(SELECT id FROM cfg_blocks WHERE node_id = ?)
		`, nodeID)
		if err != nil {
			return err
		}
		_, err = r.db.Exec("DELETE FROM cfg_blocks WHERE node_id = ?", nodeID)
		return err
	}

// FindNodesByNameExact finds nodes with exact name match (for incremental edge resolution).
func (r *Repository) FindNodesByNameExact(name string) ([]*types.Node, error) {
	rows, err := r.db.Query(`
		SELECT id, name, kind, file, line, end_line, parent_id, exported, qualified_name, scope, visibility, role, file_hash
		FROM nodes WHERE name = ?
	`, name)
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
